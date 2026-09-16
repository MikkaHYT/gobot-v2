package radio

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"gobot/internal/bot"
	"gobot/internal/helpers"
	coordinator "gobot/internal/radio"

	"github.com/bwmarrin/discordgo"
)

type TrackMetadata struct {
	Title     string
	Duration  int
	CoverData []byte
}

func IsSupportedAudioAttachment(att *discordgo.MessageAttachment) bool {
	if att == nil {
		return false
	}
	if att.ContentType != "" && strings.HasPrefix(strings.ToLower(att.ContentType), "audio/") {
		return true
	}
	return helpers.IsAudioExtension(att.Filename)
}
func IsSupportedPlaylistAttachment(att *discordgo.MessageAttachment) bool {
	if att == nil {
		return false
	}
	ext := strings.ToLower(filepath.Ext(att.Filename))
	return ext == ".txt" || (att.ContentType != "" && strings.HasPrefix(strings.ToLower(att.ContentType), "text/plain"))
}

func supportedAudioExtensionList() string {
	exts := make([]string, 0, len(helpers.SupportedAudioExtensions))
	for ext := range helpers.SupportedAudioExtensions {
		exts = append(exts, ext)
	}
	sort.Strings(exts)
	return strings.Join(exts, ", ")
}
func ResolveAudioAttachment(attachment *discordgo.MessageAttachment, optCtx ...context.Context) (*Track, error) {
	if attachment == nil {
		return nil, fmt.Errorf("no attachment provided")
	}

	if !IsSupportedAudioAttachment(attachment) {
		return nil, fmt.Errorf("invalid audio file `%s`. Supported audio formats: %s", attachment.Filename, supportedAudioExtensionList())
	}

	title := strings.TrimSuffix(attachment.Filename, filepath.Ext(attachment.Filename))

	t := &Track{
		Title:      title,
		URL:        attachment.URL,
		WebpageURL: attachment.URL,
		Uploader:   "Uploaded File",
		Category:   "uploaded",
		Duration:   0,
	}

	probeCtx := context.Background()
	if len(optCtx) > 0 && optCtx[0] != nil {
		probeCtx = optCtx[0]
	}
	meta := ProbeTrackMetadata(probeCtx, t)
	if meta.Duration > 0 {
		t.Duration = meta.Duration
	}
	if meta.Title != "" {
		t.Title = meta.Title
	}
	if len(meta.CoverData) > 0 {
		t.CoverData = meta.CoverData
		t.CoverFilename = fmt.Sprintf("cover_%d.jpg", time.Now().UnixNano())
		t.Thumbnail = ""
	} else {
		t.Thumbnail = helpers.DefaultRadioCoverURL
	}

	if t.Duration <= 0 {
		return nil, fmt.Errorf("unable to verify audio duration for `%s`. Please ensure the file is a valid audio track", attachment.Filename)
	}

	if t.Duration > coordinator.MaxTrackDurationSec {
		return nil, fmt.Errorf("audio file duration (%s) exceeds maximum allowed duration of %s", helpers.FormatDuration(time.Duration(t.Duration)*time.Second), helpers.FormatDuration(time.Duration(coordinator.MaxTrackDurationSec)*time.Second))
	}
	return t, nil
}
func ResolvePlaylistAttachment(ctx *bot.Context, att *discordgo.MessageAttachment) ([]*Track, string, error) {
	if att == nil {
		return nil, "", fmt.Errorf("no attachment provided")
	}

	if !IsSupportedPlaylistAttachment(att) {
		return nil, "", fmt.Errorf("invalid playlist file `%s`. Playlist attachments must be a `.txt` text file", att.Filename)
	}

	req, err := http.NewRequestWithContext(ctx.Context(), http.MethodGet, att.URL, nil)
	if err != nil {
		return nil, "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("User-Agent", helpers.GetUserAgent())

	client := helpers.NewSafeHTTPClient(helpers.DurationTimeoutHTTP)
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("failed to download playlist file: %w", err)
	}
	defer resp.Body.Close()

	buf, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if err != nil {
		return nil, "", fmt.Errorf("failed to read playlist file: %w", err)
	}

	lines := strings.Split(string(buf), "\n")
	var validLines []string
	const maxPlaylistTracks = 100

	for _, rawLine := range lines {
		if len(validLines) >= maxPlaylistTracks {
			break
		}
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") {
			continue
		}
		validLines = append(validLines, line)
	}

	if len(validLines) == 0 {
		return nil, "", fmt.Errorf("no valid tracks or URLs found in playlist file")
	}

	var tracks []*Track
	upfrontLimit := 5
	if len(validLines) < upfrontLimit {
		upfrontLimit = len(validLines)
	}

	for i := 0; i < upfrontLimit; i++ {
		line := validLines[i]
		resolved, err := resolveQuery(ctx, line)
		if err == nil && len(resolved) > 0 {
			tracks = append(tracks, resolved...)
		} else {
			tracks = append(tracks, &Track{
				Title:      line,
				WebpageURL: line,
				URL:        "",
				Uploader:   "Playlist",
				Category:   "playlist_unresolved",
			})
		}
	}

	for i := upfrontLimit; i < len(validLines); i++ {
		line := validLines[i]
		tracks = append(tracks, &Track{
			Title:      line,
			WebpageURL: line,
			URL:        "",
			Uploader:   "Playlist",
			Category:   "playlist_unresolved",
		})
	}

	if len(tracks) == 0 {
		return nil, "", fmt.Errorf("no valid tracks could be resolved from playlist file")
	}

	return tracks, att.Filename, nil
}

func ProbeTrackMetadata(ctx context.Context, t *Track) TrackMetadata {
	if t == nil || t.URL == "" {
		return TrackMetadata{}
	}

	meta := TrackMetadata{
		Duration:  t.Duration,
		Title:     t.Title,
		CoverData: t.CoverData,
	}

	if ctx == nil {
		ctx = context.Background()
	}

	userAgent := helpers.GetUserAgent()

	probeCtx, cancelProbe := context.WithTimeout(ctx, 10*time.Second)
	defer cancelProbe()

	var stdout bytes.Buffer
	probedPath := ""

	lowerURL := strings.ToLower(t.URL)
	if strings.HasPrefix(lowerURL, "http://") || strings.HasPrefix(lowerURL, "https://") {
		safeClient := helpers.NewSafeStreamingHTTPClient()
		req, errReq := http.NewRequestWithContext(probeCtx, http.MethodGet, t.URL, nil)
		if errReq == nil {
			req.Header.Set("User-Agent", userAgent)
			resp, errResp := safeClient.Do(req)
			if errResp == nil && resp != nil {
				defer resp.Body.Close()
				if resp.StatusCode >= 200 && resp.StatusCode < 300 {
					tmpFile, errTmp := os.CreateTemp(coordinator.RadioBufferDir(), "probe_*.mp3")
					if errTmp == nil {
						defer func() {
							name := tmpFile.Name()
							tmpFile.Close()
							os.Remove(name)
						}()
						_, _ = io.Copy(tmpFile, io.LimitReader(resp.Body, 10*1024*1024))
						tmpFile.Close()
						probedPath = tmpFile.Name()
						cmd := exec.CommandContext(probeCtx, helpers.GetFFprobePath(),
							"-v", "quiet",
							"-print_format", "json",
							"-show_format",
							"-show_streams",
							"--",
							tmpFile.Name(),
						)
						cmd.Stdout = &stdout
						_ = cmd.Run()
					}
				}
			}
		}
	} else if t.URL != "" && !strings.HasPrefix(t.URL, "-") {
		probedPath = t.URL
		cmd := exec.CommandContext(probeCtx, helpers.GetFFprobePath(),
			"-v", "quiet",
			"-print_format", "json",
			"-show_format",
			"-show_streams",
			"--",
			t.URL,
		)
		cmd.Stdout = &stdout
		_ = cmd.Run()
	}

	if stdout.Len() > 0 {
		var probe struct {
			Format struct {
				Duration string            `json:"duration"`
				Tags     map[string]string `json:"tags"`
			} `json:"format"`
			Streams []struct {
				CodecType string            `json:"codec_type"`
				Tags      map[string]string `json:"tags"`
			} `json:"streams"`
		}
		if json.Unmarshal(stdout.Bytes(), &probe) == nil {
			if d, err := strconv.ParseFloat(probe.Format.Duration, 64); err == nil && d > 0 {
				meta.Duration = int(d)
			}

			allTags := make(map[string]string)
			for k, v := range probe.Format.Tags {
				allTags[strings.ToLower(strings.TrimSpace(k))] = strings.TrimSpace(v)
			}
			for _, stream := range probe.Streams {
				for k, v := range stream.Tags {
					lowerK := strings.ToLower(strings.TrimSpace(k))
					if _, exists := allTags[lowerK]; !exists {
						allTags[lowerK] = strings.TrimSpace(v)
					}
				}
			}

			if id3Title, ok := allTags["title"]; ok && id3Title != "" {
				meta.Title = id3Title
			}

			for _, stream := range probe.Streams {
				if stream.CodecType == "video" {
					meta.CoverData = extractEmbeddedCoverArt(probeCtx, probedPath)
					break
				}
			}
		}
	}

	return meta
}

func extractEmbeddedCoverArt(ctx context.Context, path string) []byte {
	if ctx == nil || path == "" {
		return nil
	}

	outFile, err := os.CreateTemp(coordinator.RadioBufferDir(), "cover_*.jpg")
	if err != nil {
		return nil
	}
	outPath := outFile.Name()
	outFile.Close()
	defer func() { _ = os.Remove(outPath) }()

	cmd := exec.CommandContext(ctx, helpers.GetFFmpegPath(),
		"-v", "error",
		"-y",
		"-i", path,
		"-map", "0:v:0",
		"-frames:v", "1",
		"-f", "image2",
		"-update", "1",
		outPath,
	)
	if err := cmd.Run(); err != nil {
		bot.Warnf("[RADIO] Embedded cover art export failed for %s: %v", filepath.Base(path), err)
		return nil
	}

	data, err := os.ReadFile(outPath)
	if err != nil || len(data) < 4 {
		return nil
	}
	return data
}
