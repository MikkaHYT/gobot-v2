package radio

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gobot/internal/helpers"
)

type QueryFetcher func(ctx context.Context, query string) (*Track, error)

const (
	DefaultUserAgent    = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"
	MaxTrackDurationSec = 3 * 60 * 60
)

type SearchFailureClass string

const (
	SearchFailureExecutableUnavailable SearchFailureClass = "executable_unavailable"
	SearchFailureExecutablePermission  SearchFailureClass = "executable_permission_denied"
	SearchFailureDeadline              SearchFailureClass = "process_deadline"
	SearchFailureProcessExit           SearchFailureClass = "process_exit_failure"
	SearchFailureMalformedJSON         SearchFailureClass = "malformed_json"
	SearchFailureEmpty                 SearchFailureClass = "empty_result"
)

type SearchError struct {
	Source     string
	Class      SearchFailureClass
	Executable string
	ExitCode   int
	Diagnostic string
	cause      error
}

func (e *SearchError) Error() string {
	if e == nil {
		return "radio search failed"
	}
	if e.Source == "" {
		return fmt.Sprintf("radio search failed (%s)", e.Class)
	}
	return fmt.Sprintf("%s search failed (%s)", e.Source, e.Class)
}

func (e *SearchError) Unwrap() error { return e.cause }

var (
	searchCommandPathPattern = regexp.MustCompile(`(?i)\b[A-Za-z]:[\\/][^\s]+|(?:^|[\s=(])/(?:[^\s,)]+)`)
	searchURLPattern         = regexp.MustCompile(`(?i)\b(?:https?|ftp)://[^\s]+`)
	searchSecretPattern      = regexp.MustCompile(`(?i)(?:(?:--?(?:cookies?|proxy|password|token|authorization))|(?:cookies?|proxy|password|token|authorization))(?:=|:|\s+)\s*[^\s]+`)
)

func newSearchError(source string, class SearchFailureClass, executable string, exitCode int, diagnostic string, cause error) *SearchError {
	return &SearchError{
		Source:     source,
		Class:      class,
		Executable: executable,
		ExitCode:   exitCode,
		Diagnostic: sanitizeSearchDiagnostic(diagnostic),
		cause:      cause,
	}
}

func sanitizeSearchDiagnostic(raw string) string {
	diagnostic := strings.Join(strings.Fields(raw), " ")
	diagnostic = searchSecretPattern.ReplaceAllString(diagnostic, "$1[redacted]")
	diagnostic = searchURLPattern.ReplaceAllString(diagnostic, "[url]")
	diagnostic = searchCommandPathPattern.ReplaceAllString(diagnostic, "[path]")
	if len(diagnostic) > 240 {
		diagnostic = diagnostic[:240] + "..."
	}
	return diagnostic
}

func sanitizeStderr(raw string, maxLen int) string {
	if maxLen <= 0 {
		maxLen = 1500
	}
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}

	var errorLines []string
	for _, l := range strings.Split(trimmed, "\n") {
		lineTrimmed := strings.TrimSpace(l)
		lower := strings.ToLower(lineTrimmed)
		if strings.HasPrefix(lower, "error:") || strings.Contains(lower, "[error]") || strings.Contains(lower, "error: ") {
			errorLines = append(errorLines, lineTrimmed)
		}
	}

	target := trimmed
	if len(errorLines) > 0 {
		target = strings.Join(errorLines, " | ")
	}

	diagnostic := searchSecretPattern.ReplaceAllString(target, "$1[redacted]")
	diagnostic = searchCommandPathPattern.ReplaceAllString(diagnostic, "[path]")
	diagnostic = strings.Join(strings.Fields(diagnostic), " ")

	if len(diagnostic) > maxLen {
		diagnostic = diagnostic[:maxLen] + "..."
	}
	return diagnostic
}

func logSearchFailure(err *SearchError) {
	if err == nil || err.Class == SearchFailureEmpty {
		return
	}
	helpers.LogWarn("[RADIO][SEARCH] source=%s class=%s executable=%s exit_code=%d diagnostic=%q", err.Source, err.Class, sanitizeSearchExecutable(err.Executable), err.ExitCode, err.Diagnostic)
}

func sanitizeSearchExecutable(path string) string {
	name := filepath.Base(strings.TrimSpace(path))
	if name == "." || name == string(filepath.Separator) || name == "" {
		return "[unknown]"
	}
	return sanitizeSearchDiagnostic(name)
}

func searchSourceName(source string) string {
	if strings.EqualFold(source, "youtube") || strings.EqualFold(source, "yt") {
		return "YouTube"
	}
	return "SoundCloud"
}

type ytdlpCommandRunner func(ctx context.Context, executable string, args ...string) (stdout, stderr []byte, err error)

var (
	getYTDLPPath                             = helpers.GetYTDLPPath
	runYTDLPSearchCommand ytdlpCommandRunner = runYTDLPCommand
)

func runYTDLPCommand(ctx context.Context, executable string, args ...string) ([]byte, []byte, error) {
	cmd := exec.CommandContext(ctx, executable, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.Bytes(), stderr.Bytes(), err
}

func ParseMediaTimestamp(s string) int {
	clean := strings.TrimSpace(s)
	if clean == "" || strings.EqualFold(clean, "n/a") {
		return 0
	}
	parts := strings.Split(clean, ":")
	if len(parts) == 2 {
		mins, errM := strconv.Atoi(strings.TrimSpace(parts[0]))
		secs, errS := strconv.Atoi(strings.TrimSpace(parts[1]))
		if errM == nil && errS == nil && mins >= 0 && secs >= 0 {
			return mins*60 + secs
		}
	} else if len(parts) == 3 {
		hours, errH := strconv.Atoi(strings.TrimSpace(parts[0]))
		mins, errM := strconv.Atoi(strings.TrimSpace(parts[1]))
		secs, errS := strconv.Atoi(strings.TrimSpace(parts[2]))
		if errH == nil && errM == nil && errS == nil && hours >= 0 && mins >= 0 && secs >= 0 {
			return hours*3600 + mins*60 + secs
		}
	} else if secVal, err := strconv.Atoi(clean); err == nil && secVal > 0 {
		return secVal
	}
	return 0
}

func ProbeFileDuration(filePath string) int {
	if filePath == "" {
		return 0
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, helpers.GetFFprobePath(),
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1",
		filePath,
	)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err == nil {
		outStr := strings.TrimSpace(stdout.String())
		if d, errParse := strconv.ParseFloat(outStr, 64); errParse == nil && d > 0 {
			return int(d)
		}
	}
	return 0
}

func ValidateTrackDuration(t *Track) error {
	if t == nil {
		return fmt.Errorf("invalid track")
	}
	if t.Duration > MaxTrackDurationSec {
		min := t.Duration / 60
		sec := t.Duration % 60
		maxMin := MaxTrackDurationSec / 60
		return fmt.Errorf("track is too long (**%02d:%02d**); the maximum allowed track length is **%d minutes**", min, sec, maxMin)
	}
	return nil
}

type resolveCall struct {
	done   chan struct{}
	tracks []*Track
	err    error
}

func ResolveQuery(ctx context.Context, query string) ([]*Track, error) {
	return ResolveQueryWithFetcher(ctx, nil, query)
}

func ResolveQueryWithFetcher(ctx context.Context, fetcher QueryFetcher, query string) ([]*Track, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	clean := strings.TrimSpace(query)
	if clean == "" {
		return nil, fmt.Errorf("empty query")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	return resolveQueryInternal(ctx, fetcher, clean)
}

func resolveQueryInternal(ctx context.Context, fetcher QueryFetcher, query string) ([]*Track, error) {
	if clean, isURL := helpers.CleanMediaURL(query); isURL {
		query = clean
	}

	if strings.Contains(query, "://") {
		if !helpers.IsURLSafe(query) {
			return nil, fmt.Errorf("insecure or unsupported URL target")
		}
		if !helpers.IsDomainAllowed(query) {
			return nil, fmt.Errorf("domain not allowed")
		}
	}

	if !strings.HasPrefix(query, "http://") && !strings.HasPrefix(query, "https://") {
		if fetcher != nil {
			if jwTrack, err := fetcher(ctx, query); err == nil && jwTrack != nil {
				if errDur := ValidateTrackDuration(jwTrack); errDur != nil {
					return nil, errDur
				}
				return []*Track{jwTrack}, nil
			}
		}
	}

	targetQuery := query
	if !strings.HasPrefix(query, "http://") && !strings.HasPrefix(query, "https://") {
		targetQuery = fmt.Sprintf("scsearch1:%s", query)
	}

	track, err := fetchSingleTrack(ctx, targetQuery)
	if err != nil && strings.HasPrefix(targetQuery, "scsearch1:") {
		ytQuery := fmt.Sprintf("ytsearch1:%s", query)
		track, err = fetchSingleTrack(ctx, ytQuery)
	}

	if err != nil {
		return nil, err
	}

	if errDur := ValidateTrackDuration(track); errDur != nil {
		return nil, errDur
	}

	return []*Track{track}, nil
}

func SearchMultipleTracks(ctx context.Context, source, query string, count int) ([]*Track, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("empty search query")
	}
	if clean, isURL := helpers.CleanMediaURL(query); isURL {
		track, err := fetchSingleTrack(ctx, clean)
		if err != nil {
			return nil, err
		}
		if track != nil {
			return []*Track{track}, nil
		}
	}
	if count <= 0 {
		count = 5
	}
	if count > 10 {
		count = 10
	}

	userAgent := helpers.GetUserAgent()

	prefix := "scsearch"
	if strings.EqualFold(source, "youtube") || strings.EqualFold(source, "yt") {
		prefix = "ytsearch"
	}

	searchSpec := fmt.Sprintf("%s%d:%s", prefix, count, query)

	args := []string{
		"--ignore-config",
		"--no-call-home",
		"--socket-timeout", "15",
		"--dump-single-json",
		"--flat-playlist",
		"--format", "bestaudio/best",
		"--user-agent", userAgent,
	}

	radioPathMu.RLock()
	cookiesPath := configuredCookies
	radioPathMu.RUnlock()
	if cookiesPath != "" {
		if _, err := os.Stat(cookiesPath); err == nil {
			args = append(args, "--cookies", cookiesPath)
		}
	}

	args = append(args, "--", searchSpec)

	ytdlpBin := getYTDLPPath()
	if err := helpers.ValidateExecutable(ytdlpBin); err != nil {
		class := SearchFailureExecutableUnavailable
		if errors.Is(err, helpers.ErrBinaryPermissionDenied) {
			class = SearchFailureExecutablePermission
		}
		searchErr := newSearchError(searchSourceName(source), class, ytdlpBin, 0, err.Error(), err)
		logSearchFailure(searchErr)
		return nil, searchErr
	}

	ctxSearch, cancelSearch := context.WithTimeout(ctx, 25*time.Second)
	defer cancelSearch()
	stdout, stderr, err := runYTDLPSearchCommand(ctxSearch, ytdlpBin, args...)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctxSearch.Err(), context.DeadlineExceeded) {
			searchErr := newSearchError(searchSourceName(source), SearchFailureDeadline, ytdlpBin, 0, context.DeadlineExceeded.Error(), err)
			logSearchFailure(searchErr)
			return nil, searchErr
		}
		if ctxErr := ctxSearch.Err(); ctxErr != nil {
			if !errors.Is(ctxErr, context.DeadlineExceeded) {
				return nil, ctxErr
			}
			searchErr := newSearchError(searchSourceName(source), SearchFailureDeadline, ytdlpBin, 0, ctxErr.Error(), ctxErr)
			logSearchFailure(searchErr)
			return nil, searchErr
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			if !errors.Is(ctxErr, context.DeadlineExceeded) {
				return nil, ctxErr
			}
			searchErr := newSearchError(searchSourceName(source), SearchFailureDeadline, ytdlpBin, 0, ctxErr.Error(), ctxErr)
			logSearchFailure(searchErr)
			return nil, searchErr
		}
		if errors.Is(err, os.ErrPermission) {
			searchErr := newSearchError(searchSourceName(source), SearchFailureExecutablePermission, ytdlpBin, 0, err.Error(), err)
			logSearchFailure(searchErr)
			return nil, searchErr
		}

		exitCode := 0
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		}
		searchErr := newSearchError(searchSourceName(source), SearchFailureProcessExit, ytdlpBin, exitCode, string(stderr), err)
		logSearchFailure(searchErr)
		return nil, searchErr
	}

	var raw map[string]any
	if err := json.Unmarshal(stdout, &raw); err != nil {
		searchErr := newSearchError(searchSourceName(source), SearchFailureMalformedJSON, ytdlpBin, 0, err.Error(), err)
		logSearchFailure(searchErr)
		return nil, searchErr
	}

	entries, ok := raw["entries"].([]any)
	if !ok || len(entries) == 0 {
		return nil, newSearchError(searchSourceName(source), SearchFailureEmpty, ytdlpBin, 0, "no entries", nil)
	}

	var tracks []*Track
	for _, item := range entries {
		entryMap, isMap := item.(map[string]any)
		if !isMap {
			continue
		}
		t := mapToTrack(entryMap, "")
		if t.WebpageURL == "" && t.URL != "" {
			if strings.Contains(t.URL, "youtube.com") || strings.Contains(t.URL, "youtu.be") || strings.Contains(t.URL, "soundcloud.com") {
				t.WebpageURL = t.URL
				t.URL = ""
			}
		}
		tracks = append(tracks, t)
		if len(tracks) >= count {
			break
		}
	}

	if len(tracks) == 0 {
		return nil, newSearchError(searchSourceName(source), SearchFailureEmpty, ytdlpBin, 0, "no valid entries", nil)
	}

	return tracks, nil
}

func fetchSingleTrack(parentCtx context.Context, query string) (*Track, error) {
	userAgent := helpers.GetUserAgent()

	args := []string{
		"--ignore-config",
		"--no-call-home",
		"--socket-timeout", "15",
		"--dump-single-json",
		"--no-playlist",
		"--format", "bestaudio/best",
		"--user-agent", userAgent,
	}

	radioPathMu.RLock()
	cookiesPath := configuredCookies
	radioPathMu.RUnlock()
	if cookiesPath != "" {
		if _, err := os.Stat(cookiesPath); err == nil {
			args = append(args, "--cookies", cookiesPath)
		}
	}

	args = append(args, "--", query)

	ytdlpBin := helpers.GetYTDLPPath()
	if parentCtx == nil {
		parentCtx = context.Background()
	}
	ctxFetch, cancelFetch := context.WithTimeout(parentCtx, 25*time.Second)
	defer cancelFetch()

	cmd := exec.CommandContext(ctxFetch, ytdlpBin, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		if ctxErr := parentCtx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		if errStr := sanitizeStderr(stderr.String(), 1500); errStr != "" {
			return nil, fmt.Errorf("yt-dlp search failed: %w (%s)", err, errStr)
		}
		return nil, fmt.Errorf("yt-dlp search failed: %w", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &raw); err != nil {
		return nil, fmt.Errorf("failed to parse track JSON: %w", err)
	}

	if entries, ok := raw["entries"].([]any); ok && len(entries) > 0 {
		if first, ok := entries[0].(map[string]any); ok {
			raw = first
		}
	}

	return mapToTrack(raw, query), nil
}

func getJSONString(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	val, ok := m[key]
	if !ok || val == nil {
		return ""
	}
	if s, ok := val.(string); ok {
		return strings.TrimSpace(s)
	}
	return ""
}

func getJSONFloat64(m map[string]any, key string) float64 {
	if m == nil {
		return 0
	}
	val, ok := m[key]
	if !ok || val == nil {
		return 0
	}
	switch v := val.(type) {
	case float64:
		return v
	case float32:
		return float64(v)
	case int:
		return float64(v)
	case int64:
		return float64(v)
	case json.Number:
		if f, err := v.Float64(); err == nil {
			return f
		}
	case string:
		if f, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
			return f
		}
	}
	return 0
}

func sanitizeTrackString(s string, maxLen int) string {
	if s == "" {
		return ""
	}
	cleaned := strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return -1
		}
		return r
	}, s)
	cleaned = strings.TrimSpace(cleaned)
	if len(cleaned) > maxLen {
		cleaned = cleaned[:maxLen]
	}
	return cleaned
}

func isValidTrackURL(raw string) bool {
	return helpers.IsValidHTTPURL(raw)
}

func mapToTrack(raw map[string]any, fallbackQuery string) *Track {
	var initialWebURL string
	if isValidTrackURL(fallbackQuery) {
		initialWebURL = fallbackQuery
	}

	t := &Track{
		Title:      "Unknown Track",
		Uploader:   "Independent Artist",
		Thumbnail:  "",
		Category:   "stream",
		WebpageURL: initialWebURL,
	}

	if title := sanitizeTrackString(getJSONString(raw, "title"), 256); title != "" {
		t.Title = title
	}
	if streamURL := getJSONString(raw, "url"); isValidTrackURL(streamURL) {
		t.URL = streamURL
	}
	if uploader := sanitizeTrackString(getJSONString(raw, "uploader"), 128); uploader != "" {
		t.Uploader = uploader
	}
	if thumb := getJSONString(raw, "thumbnail"); isValidTrackURL(thumb) {
		t.Thumbnail = thumb
	}
	dur := getJSONFloat64(raw, "duration")
	if dur > 0 && dur <= 24*3600 {
		t.Duration = int(dur)
	}

	webpageURL := getJSONString(raw, "webpage_url")
	if webpageURL == "" {
		webpageURL = getJSONString(raw, "original_url")
	}
	if isValidTrackURL(webpageURL) {
		t.WebpageURL = webpageURL
	}

	if t.URL == "" && isValidTrackURL(fallbackQuery) {
		t.URL = fallbackQuery
	}

	return t
}
