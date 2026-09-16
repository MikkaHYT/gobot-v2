package logger

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"
)

var (
	loggerUAMu sync.RWMutex
	loggerUA   = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/134.0.0.0 Safari/537.36 Edg/134.0.0.0"
)

func SetUserAgent(ua string) {
	loggerUAMu.Lock()
	defer loggerUAMu.Unlock()
	if ua != "" {
		loggerUA = ua
	}
}

func getUserAgent() string {
	loggerUAMu.RLock()
	defer loggerUAMu.RUnlock()
	return loggerUA
}

type LogLevel int

const (
	LevelDebug LogLevel = iota
	LevelInfo
	LevelWarn
	LevelError
	LevelFatal
)

func (l LogLevel) String() string {
	switch l {
	case LevelDebug:
		return "DEBUG"
	case LevelInfo:
		return "INFO"
	case LevelWarn:
		return "WARN"
	case LevelError:
		return "ERROR"
	case LevelFatal:
		return "FATAL"
	default:
		return "INFO"
	}
}

func ParseLogLevel(str string) LogLevel {
	switch strings.ToLower(strings.TrimSpace(str)) {
	case "debug", "trace", "verbose":
		return LevelDebug
	case "info":
		return LevelInfo
	case "warn", "warning":
		return LevelWarn
	case "error", "err":
		return LevelError
	case "fatal", "panic":
		return LevelFatal
	default:
		return LevelInfo
	}
}

const (
	maxLogSizeBytes       = 10 * 1024 * 1024
	webhookCoalesceWindow = 750 * time.Millisecond

	colorDefault = 0x2B2D31
	colorWarn    = 0xFEE75C
	colorError   = 0xe6161a
	colorFatal   = 0x992D22
)

var (
	logFilePath    = "logs.txt"
	oldLogFilePath = "logs.old.txt"
)

type webhookItem struct {
	level     LogLevel
	title     string
	msg       string
	targetURL string
	color     int
	timestamp time.Time
}

type Logger struct {
	mu         sync.RWMutex
	minLevel   LogLevel
	webhookURL string
	httpClient *http.Client
	fileMu     sync.Mutex

	batchMu   sync.Mutex
	batchCond *sync.Cond
	batch     []webhookItem
	stopped   bool

	closeOnce sync.Once
	wg        sync.WaitGroup
}

var (
	tokenRegex      = regexp.MustCompile(`(?i)(?:mfa\.[a-z0-9_-]{20,}|[a-z0-9_-]{24,28}\.[a-z0-9_-]{6}\.[a-z0-9_-]{27,38})`)
	credentialRegex = regexp.MustCompile(`(?i)(api_key|apikey|token|secret|password|key)\s*([:=])\s*([^\s,"'&]+)`)
	defaultLogger   = NewLogger(LevelDebug, "")
)

func SanitizeLogString(input string) string {
	if input == "" {
		return ""
	}
	sanitized := tokenRegex.ReplaceAllString(input, "[REDACTED_TOKEN]")
	sanitized = credentialRegex.ReplaceAllString(sanitized, "$1$2[REDACTED]")
	return sanitized
}

var newlineReplacer = strings.NewReplacer("\r\n", " ", "\r", " ", "\n", " ")

func flattenNewlines(input string) string {
	return newlineReplacer.Replace(input)
}

var defaultWebhookClient = &http.Client{
	Timeout: 5 * time.Second,
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

func NewLogger(level LogLevel, webhookURL string) *Logger {
	l := &Logger{
		minLevel:   level,
		webhookURL: webhookURL,
		httpClient: defaultWebhookClient,
	}
	l.batchCond = sync.NewCond(&l.batchMu)
	l.wg.Add(1)
	go l.runWebhookWorker()
	return l
}

func InitLogger(levelStr, webhookURL string) {
	defaultLogger.SetLevel(ParseLogLevel(levelStr))
	defaultLogger.SetWebhookURL(webhookURL)
}

func (l *Logger) SetLevel(level LogLevel) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.minLevel = level
}

func (l *Logger) SetWebhookURL(url string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.webhookURL = url
}

func (l *Logger) Close() {
	l.closeOnce.Do(func() {
		l.batchMu.Lock()
		l.stopped = true
		l.batchCond.Broadcast()
		l.batchMu.Unlock()
		l.wg.Wait()
	})
}

func Close() {
	defaultLogger.Close()
}

func (l *Logger) log(level LogLevel, msg string) {
	l.mu.RLock()
	if level < l.minLevel {
		l.mu.RUnlock()
		return
	}
	webhookURL := l.webhookURL
	l.mu.RUnlock()

	now := time.Now()
	timestamp := now.Format("2006/01/02 15:04:05")
	cleanMsg := flattenNewlines(msg)
	cleanMsg = SanitizeLogString(cleanMsg)
	formattedLine := fmt.Sprintf("[%s] [%s] %s\n", timestamp, level.String(), cleanMsg)

	if level >= LevelError {
		_, _ = fmt.Fprint(os.Stderr, formattedLine)
	} else {
		_, _ = fmt.Fprint(os.Stdout, formattedLine)
	}

	l.appendToFile(timestamp, level.String(), cleanMsg)

	if level >= LevelWarn && webhookURL != "" {
		if level == LevelFatal {
			l.flushAndSendFatal(webhookURL, cleanMsg)
		} else {
			l.enqueueWebhook(webhookItem{
				level:     level,
				title:     fmt.Sprintf("%s Alert", level.String()),
				msg:       cleanMsg,
				timestamp: now,
			})
		}
	}
}

func (l *Logger) enqueueWebhook(item webhookItem) {
	l.batchMu.Lock()
	if l.stopped {
		l.batchMu.Unlock()
		_, _ = fmt.Fprintf(os.Stderr, "[LOGGER] Logger closed, dropping alert: %s\n", item.msg)
		return
	}
	l.batch = append(l.batch, item)
	l.batchCond.Signal()
	l.batchMu.Unlock()
}

func (l *Logger) runWebhookWorker() {
	defer l.wg.Done()
	for {
		l.batchMu.Lock()
		for len(l.batch) == 0 && !l.stopped {
			l.batchCond.Wait()
		}
		if len(l.batch) == 0 {
			l.batchMu.Unlock()
			return
		}
		pending := l.batch
		l.batch = nil
		l.batchMu.Unlock()

		time.Sleep(webhookCoalesceWindow)
		l.batchMu.Lock()
		if len(l.batch) > 0 {
			pending = append(pending, l.batch...)
			l.batch = nil
		}
		l.batchMu.Unlock()

		l.sendBatch(pending)
	}
}

func (l *Logger) flushAndSendFatal(webhookURL string, fatalMsg string) {
	l.batchMu.Lock()
	pending := l.batch
	l.batch = nil
	l.stopped = true
	l.batchMu.Unlock()

	pending = append(pending, webhookItem{
		level:     LevelFatal,
		title:     "FATAL",
		msg:       fatalMsg,
		timestamp: time.Now(),
	})

	l.sendBatchWithURL(webhookURL, pending)
}

func (l *Logger) sendBatch(batch []webhookItem) {
	if len(batch) == 0 {
		return
	}

	urlBatches := make(map[string][]webhookItem)

	l.mu.RLock()
	defaultURL := l.webhookURL
	l.mu.RUnlock()

	for _, item := range batch {
		target := item.targetURL
		if target == "" {
			target = defaultURL
		}
		if target != "" {
			urlBatches[target] = append(urlBatches[target], item)
		}
	}

	for targetURL, items := range urlBatches {
		l.sendBatchWithURL(targetURL, items)
	}
}

func truncateUTF16Safe(s string, maxUnits int) string {
	if maxUnits <= 0 {
		return ""
	}
	suffix := "\n... (truncated)"
	targetUnits := maxUnits - len(suffix)
	if targetUnits <= 0 {
		targetUnits = maxUnits
	}

	var units int
	for idx, r := range s {
		rUnits := 1
		if r > 0xFFFF {
			rUnits = 2
		}
		if units+rUnits > targetUnits {
			return s[:idx] + suffix
		}
		units += rUnits
	}
	return s
}

func (l *Logger) sendBatchWithURL(webhookURL string, batch []webhookItem) {
	if len(batch) == 0 || webhookURL == "" {
		return
	}

	l.mu.RLock()
	client := l.httpClient
	l.mu.RUnlock()

	if client == nil {
		client = defaultWebhookClient
	}

	maxLevel := LevelInfo
	var customColor int
	var sb strings.Builder

	for _, item := range batch {
		if item.level > maxLevel {
			maxLevel = item.level
		}
		if item.color != 0 {
			customColor = item.color
		}
		ts := item.timestamp.UTC().Format("15:04:05")
		cleanMsg := SanitizeLogString(item.msg)
		cleanTitle := SanitizeLogString(item.title)
		if cleanTitle != "" {
			_, _ = fmt.Fprintf(&sb, "`[%s]` **[%s]** `%s`: %s\n", ts, item.level.String(), cleanTitle, cleanMsg)
		} else {
			_, _ = fmt.Fprintf(&sb, "`[%s]` **[%s]** %s\n", ts, item.level.String(), cleanMsg)
		}
	}

	desc := truncateUTF16Safe(sb.String(), 3800)

	title := maxLevel.String()
	if len(batch) > 1 {
		title = fmt.Sprintf("%s (%d events)", maxLevel.String(), len(batch))
	}

	embedColor := colorForLevel(maxLevel)
	if customColor != 0 {
		embedColor = customColor
	}

	payload := map[string]any{
		"embeds": []map[string]any{
			{
				"title":       title,
				"description": desc,
				"color":       embedColor,
				"timestamp":   time.Now().UTC().Format(time.RFC3339),
			},
		},
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL, bytes.NewBuffer(bodyBytes))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", getUserAgent())
	resp, err := client.Do(req)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "[LOGGER] Failed to send webhook alert: %v\n", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<10))
		_, _ = fmt.Fprintf(os.Stderr, "[LOGGER] Webhook endpoint returned status %d: %s\n", resp.StatusCode, string(respBody))
		return
	}
	_, _ = io.Copy(io.Discard, resp.Body)
}

func colorForLevel(level LogLevel) int {
	switch level {
	case LevelWarn:
		return colorWarn
	case LevelError:
		return colorError
	case LevelFatal:
		return colorFatal
	default:
		return colorDefault
	}
}

func (l *Logger) appendToFile(timestamp, levelStr, msg string) {
	l.fileMu.Lock()
	defer l.fileMu.Unlock()

	if info, err := os.Stat(logFilePath); err == nil && info.Size() >= maxLogSizeBytes {
		_ = os.Remove(oldLogFilePath)
		_ = os.Rename(logFilePath, oldLogFilePath)
	}

	f, err := os.OpenFile(logFilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return
	}
	defer f.Close()

	_, _ = fmt.Fprintf(f, "[%s] [%s] %s\n", timestamp, levelStr, msg)
}

func (l *Logger) Debugf(format string, args ...any) { l.log(LevelDebug, fmt.Sprintf(format, args...)) }
func (l *Logger) Infof(format string, args ...any)  { l.log(LevelInfo, fmt.Sprintf(format, args...)) }
func (l *Logger) Warnf(format string, args ...any)  { l.log(LevelWarn, fmt.Sprintf(format, args...)) }
func (l *Logger) Errorf(format string, args ...any) { l.log(LevelError, fmt.Sprintf(format, args...)) }
func (l *Logger) Fatalf(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	cleanMsg := SanitizeLogString(msg)
	l.log(LevelFatal, cleanMsg)
	panic(fmt.Sprintf("FATAL: %s", cleanMsg))
}

func Debugf(format string, args ...any) { defaultLogger.Debugf(format, args...) }
func Infof(format string, args ...any)  { defaultLogger.Infof(format, args...) }
func Warnf(format string, args ...any)  { defaultLogger.Warnf(format, args...) }
func Errorf(format string, args ...any) { defaultLogger.Errorf(format, args...) }
func Fatalf(format string, args ...any) { defaultLogger.Fatalf(format, args...) }

func SendConsoleWebhook(webhookURL, title, description string, color int) {
	cleanTitle := flattenNewlines(SanitizeLogString(title))
	cleanDesc := flattenNewlines(SanitizeLogString(description))
	defaultLogger.appendToFile(time.Now().Format("2006/01/02 15:04:05"), "WEBHOOK", fmt.Sprintf("[%s] %s", cleanTitle, cleanDesc))
	if webhookURL != "" {
		defaultLogger.enqueueWebhook(webhookItem{
			level:     LevelInfo,
			title:     cleanTitle,
			msg:       cleanDesc,
			targetURL: webhookURL,
			color:     color,
			timestamp: time.Now(),
		})
	}
}
