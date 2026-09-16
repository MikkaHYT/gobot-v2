package ripper

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"gobot/internal/helpers"
	"gobot/internal/logger"
)

type YTDLPDownloader struct {
	binPath     string
	cookiesPath string
}

func NewYTDLPDownloader(cookiesPath ...string) *YTDLPDownloader {
	c := ""
	if len(cookiesPath) > 0 {
		c = cookiesPath[0]
	}
	return &YTDLPDownloader{
		binPath:     helpers.GetYTDLPPath(),
		cookiesPath: c,
	}
}

var ytdlpProgRx = regexp.MustCompile(`\[download\]\s+(\d+\.\d+)%\s+of\s+[~]?([^ ]+)\s+at\s+([^ ]+)\s+ETA\s+([^ ]+)`)

func (y *YTDLPDownloader) Supports(targetURL *url.URL) bool {
	return targetURL != nil && targetURL.Host != ""
}

func (y *YTDLPDownloader) Download(ctx context.Context, req DownloadRequest, progress chan<- Progress) (DownloadResult, error) {
	var result DownloadResult
	if !helpers.IsURLSafe(req.URL) {
		return result, fmt.Errorf("insecure or private download target")
	}

	binPath := y.binPath
	if binPath == "" {
		binPath = helpers.GetYTDLPPath()
	}
	maxSizeMB := int(req.MaxSizeBytes / (1024 * 1024))
	if maxSizeMB <= 0 {
		maxSizeMB = 50
	}

	outPattern := filepath.Join(req.OutputDir, fmt.Sprintf("rip_%s_%%(id)s.%%(ext)s", req.TaskID))
	formatString := "bestvideo[height<=1080][ext=mp4]+bestaudio[ext=m4a]/bestvideo[height<=1080]+bestaudio/best[height<=1080]/best"

	args := []string{
		"--ignore-config",
		"--no-call-home",
		"--socket-timeout", "15",
		"--no-playlist",
		"--write-info-json",
		"--max-filesize", fmt.Sprintf("%dM", maxSizeMB),
		"--concurrent-fragments", "5",
		"--retries", "10",
		"--user-agent", helpers.GetUserAgent(),
		"--newline",
		"--geo-bypass",
		"-o", outPattern,
	}

	if nodePath, err := exec.LookPath("node"); err == nil && nodePath != "" {
		args = append(args, "--js-runtimes", "node")
	}

	if ffmpegPath := helpers.GetFFmpegPath(); ffmpegPath != "" && ffmpegPath != "ffmpeg" {
		if filepath.IsAbs(ffmpegPath) || strings.ContainsRune(ffmpegPath, filepath.Separator) {
			args = append(args, "--ffmpeg-location", ffmpegPath)
		}
	}

	if y.cookiesPath != "" {
		if _, err := os.Stat(y.cookiesPath); err == nil {
			args = append(args, "--cookies", y.cookiesPath)
		}
	}

	if req.AudioOnly {
		args = append(args, "-f", "ba/b", "-x", "--audio-format", "mp3")
		if req.BestQuality {
			args = append(args, "--audio-quality", "320k")
		}
	} else {
		if req.BestQuality {
			formatString = fmt.Sprintf("bestvideo[filesize_approx<?%dM]+bestaudio[filesize_approx<?%dM]/bestvideo+bestaudio/best", maxSizeMB, maxSizeMB)
			args = append(args, "--merge-output-format", "mp4/mkv")
		}
		args = append(args, "-f", formatString)
	}

	args = append(args, "--", req.URL)
	cmd := exec.CommandContext(ctx, binPath, args...)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return result, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return result, err
	}

	if err := cmd.Start(); err != nil {
		_ = stdout.Close()
		_ = stderr.Close()
		return result, fmt.Errorf("failed to start yt-dlp: %w", err)
	}

	var maxFilesizeHit atomic.Bool
	recordMaxFilesize := func() {
		maxFilesizeHit.Store(true)
	}

	var stderrBuf strings.Builder
	var stderrWg sync.WaitGroup
	stderrWg.Add(1)
	helpers.Spawn(func() {
		defer stderrWg.Done()
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.Contains(line, "File is larger than max-filesize") {
				recordMaxFilesize()
			}
			stderrBuf.WriteString(line + "\n")
		}
	})

	var stdoutWg sync.WaitGroup
	if progress != nil {
		stdoutWg.Add(1)
		helpers.Spawn(func() {
			defer stdoutWg.Done()
			parseStdoutProgress(ctx, stdout, progress, recordMaxFilesize)
		})
	} else {
		stdoutWg.Add(1)
		helpers.Spawn(func() {
			defer stdoutWg.Done()
			scanner := bufio.NewScanner(stdout)
			for scanner.Scan() {
				if strings.Contains(scanner.Text(), "File is larger than max-filesize") {
					recordMaxFilesize()
				}
			}
			_ = stdout.Close()
		})
	}

	cmdErr := cmd.Wait()
	stderrWg.Wait()
	stdoutWg.Wait()
	if cmdErr != nil {
		errOutput := strings.TrimSpace(stderrBuf.String())
		if strings.Contains(errOutput, "File is larger than max-filesize") || maxFilesizeHit.Load() {
			return result, ErrMediaTooLarge
		}
		return result, fmt.Errorf("yt-dlp error: %w: %s", cmdErr, helpers.TruncateStringWithEllipsis(errOutput, 200))
	}

	if maxFilesizeHit.Load() {
		return result, ErrMediaTooLarge
	}

	infoPattern := filepath.Join(req.OutputDir, fmt.Sprintf("rip_%s_*.info.json", req.TaskID))
	if infoMatches, err := filepath.Glob(infoPattern); err == nil && len(infoMatches) > 0 {
		if jsonBytes, err := os.ReadFile(infoMatches[0]); err == nil {
			if unmarshalErr := json.Unmarshal(jsonBytes, &result.Meta); unmarshalErr != nil {
				logger.Warnf("[RIPPER] Failed to parse yt-dlp info json for task %s: %v", req.TaskID, unmarshalErr)
			}
		}
	}

	pattern := filepath.Join(req.OutputDir, fmt.Sprintf("rip_%s_*", req.TaskID))
	matches, err := filepath.Glob(pattern)
	if err != nil || len(matches) == 0 {
		return result, fmt.Errorf("downloaded media file not found")
	}

	var hasPartFile bool
	for _, m := range matches {
		if strings.HasSuffix(m, ".part") || strings.HasSuffix(m, ".ytdl") {
			hasPartFile = true
		} else if !strings.HasSuffix(m, ".info.json") {
			result.Files = append(result.Files, m)
		}
	}

	if maxFilesizeHit.Load() || (hasPartFile && len(result.Files) == 0) {
		return result, ErrMediaTooLarge
	}

	if len(result.Files) == 0 {
		return result, fmt.Errorf("no extracted media files found")
	}

	if !req.AudioOnly {
		hasVideo := false
		for _, f := range result.Files {
			if isVideoFile(f) {
				hasVideo = true
				break
			}
		}
		if !hasVideo {
			if hasPartFile || maxFilesizeHit.Load() {
				return result, ErrMediaTooLarge
			}
			return result, fmt.Errorf("video download incomplete, only audio was found")
		}
	}

	if req.AudioOnly && len(result.Files) == 1 && result.Meta.Thumbnail != "" {
		ext := strings.ToLower(filepath.Ext(result.Files[0]))
		if ext == ".mp3" || ext == ".m4a" {
			tagged := embedID3Artwork(ctx, result.Files[0], result.Meta.Thumbnail)
			if tagged != result.Files[0] {
				result.Files[0] = tagged
			}
		}
	}

	return result, nil
}

func isVideoFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".mp4", ".mkv", ".webm", ".mov", ".avi", ".flv", ".wmv", ".m4v":
		base := strings.ToLower(filepath.Base(path))
		if strings.Contains(base, ".f249.") || strings.Contains(base, ".f250.") || strings.Contains(base, ".f251.") {
			return false
		}
		return true
	default:
		return false
	}
}

func parseStdoutProgress(ctx context.Context, stdout io.ReadCloser, progressCh chan<- Progress, onMaxFilesize ...func()) {
	defer stdout.Close()
	scanner := bufio.NewScanner(stdout)

	done := make(chan struct{})
	helpers.Spawn(func() {
		select {
		case <-ctx.Done():
			_ = stdout.Close()
		case <-done:
		}
	})
	defer close(done)

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return
		default:
		}
		line := scanner.Text()
		if strings.Contains(line, "File is larger than max-filesize") {
			for _, fn := range onMaxFilesize {
				if fn != nil {
					fn()
				}
			}
		}
		matches := ytdlpProgRx.FindStringSubmatch(line)
		if len(matches) >= 5 {
			percent, _ := strconv.ParseFloat(matches[1], 64)
			p := Progress{
				Stage:   StageDownloading,
				Percent: percent,
				Speed:   matches[3],
				ETA:     matches[4],
			}
			select {
			case progressCh <- p:
			default:
			}
		}
	}
}
