package ripper

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"gobot/internal/helpers"
)

var (
	safeFilenameRegex    = regexp.MustCompile(`[^a-zA-Z0-9_-]`)
	twitterStatusIDRegex = regexp.MustCompile(`/(?:status|statuses)/(\d+)`)
)

type APIFallbackDownloader struct {
	client *http.Client
}

func NewAPIFallbackDownloader() *APIFallbackDownloader {
	return &APIFallbackDownloader{
		client: helpers.NewSafeHTTPClient(15 * time.Second),
	}
}

func (a *APIFallbackDownloader) Supports(targetURL *url.URL) bool {
	if targetURL == nil {
		return false
	}
	host := strings.ToLower(targetURL.Hostname())
	if host == "tiktok.com" || strings.HasSuffix(host, ".tiktok.com") {
		return true
	}
	if host == "twitter.com" || strings.HasSuffix(host, ".twitter.com") ||
		host == "x.com" || strings.HasSuffix(host, ".x.com") ||
		host == "fxtwitter.com" || strings.HasSuffix(host, ".fxtwitter.com") ||
		host == "vxtwitter.com" || strings.HasSuffix(host, ".vxtwitter.com") ||
		host == "fixupx.com" || strings.HasSuffix(host, ".fixupx.com") {
		return true
	}
	if helpers.IsDirectMediaURL(targetURL) {
		return true
	}
	if host == "imgur.com" || host == "i.imgur.com" {
		return true
	}
	return false
}

func (a *APIFallbackDownloader) Download(ctx context.Context, req DownloadRequest, progress chan<- Progress) (DownloadResult, error) {
	var result DownloadResult
	u, err := url.Parse(req.URL)
	if err != nil || !a.Supports(u) {
		return result, ErrUnsupportedDomain
	}

	host := strings.ToLower(u.Hostname())
	if host == "tiktok.com" || strings.HasSuffix(host, ".tiktok.com") {
		return a.downloadTikTok(ctx, req, progress)
	}
	if host == "twitter.com" || strings.HasSuffix(host, ".twitter.com") ||
		host == "x.com" || strings.HasSuffix(host, ".x.com") ||
		host == "fxtwitter.com" || strings.HasSuffix(host, ".fxtwitter.com") ||
		host == "vxtwitter.com" || strings.HasSuffix(host, ".vxtwitter.com") ||
		host == "fixupx.com" || strings.HasSuffix(host, ".fixupx.com") {
		return a.downloadTwitter(ctx, req, progress)
	}
	return a.downloadDirectMedia(ctx, req, progress)
}

type tikWMApiResponse struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data struct {
		ID     string   `json:"id"`
		Title  string   `json:"title"`
		Play   string   `json:"play"`
		WMPlay string   `json:"wmplay"`
		HDPlay string   `json:"hdplay"`
		Music  string   `json:"music"`
		Images []string `json:"images"`
		Author struct {
			Nickname string `json:"nickname"`
			UniqueID string `json:"unique_id"`
		} `json:"author"`
	} `json:"data"`
}

func (a *APIFallbackDownloader) downloadTikTok(ctx context.Context, req DownloadRequest, progress chan<- Progress) (DownloadResult, error) {
	var result DownloadResult
	apiURL := fmt.Sprintf("https://www.tikwm.com/api/?url=%s", url.QueryEscape(req.URL))

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return result, err
	}
	httpReq.Header.Set("User-Agent", helpers.GetUserAgent())

	resp, err := a.client.Do(httpReq)
	if err != nil {
		return result, fmt.Errorf("tikwm api error: %w", err)
	}
	defer resp.Body.Close()

	var apiRes tikWMApiResponse
	limitedBody := io.LimitReader(resp.Body, 10*1024*1024)
	if err := json.NewDecoder(limitedBody).Decode(&apiRes); err != nil {
		return result, fmt.Errorf("failed to decode tikwm response: %w", err)
	}

	if apiRes.Code != 0 || (apiRes.Data.Play == "" && apiRes.Data.WMPlay == "" && apiRes.Data.Music == "" && len(apiRes.Data.Images) == 0) {
		return result, fmt.Errorf("tikwm returned error: %s", apiRes.Msg)
	}

	result.Meta.Title = apiRes.Data.Title
	if apiRes.Data.Author.Nickname != "" {
		result.Meta.Uploader = apiRes.Data.Author.Nickname
	} else if apiRes.Data.Author.UniqueID != "" {
		result.Meta.Uploader = "@" + apiRes.Data.Author.UniqueID
	}
	result.Meta.WebpageURL = req.URL

	if req.AudioOnly {
		audioURL := apiRes.Data.Music
		if audioURL == "" {
			audioURL = apiRes.Data.Play
		}
		if audioURL == "" {
			audioURL = apiRes.Data.WMPlay
		}
		if audioURL == "" {
			return result, fmt.Errorf("no audio track in tikwm response")
		}
		outPath := filepath.Join(req.OutputDir, fmt.Sprintf("rip_%s_tiktok.mp3", req.TaskID))
		if err := a.downloadFile(ctx, audioURL, outPath, req.MaxSizeBytes, progress); err != nil {
			return result, err
		}
		result.Files = append(result.Files, outPath)
		return result, nil
	}

	if len(apiRes.Data.Images) > 0 {
		safeID := safeFilenameRegex.ReplaceAllString(apiRes.Data.ID, "_")
		if safeID == "" {
			safeID = "tiktok"
		}
		maxImages := 20
		if len(apiRes.Data.Images) < maxImages {
			maxImages = len(apiRes.Data.Images)
		}
		for i := range maxImages {
			imgURL := apiRes.Data.Images[i]
			ext := ".jpg"
			if imgParsed, parseErr := url.Parse(imgURL); parseErr == nil {
				if e := filepath.Ext(imgParsed.Path); e != "" {
					ext = e
				}
			}
			outPath := filepath.Join(req.OutputDir, fmt.Sprintf("rip_%s_%s_img_%d%s", req.TaskID, safeID, i+1, ext))
			if err := a.downloadFile(ctx, imgURL, outPath, req.MaxSizeBytes, progress); err != nil {
				if errors.Is(err, ErrMediaTooLarge) {
					if len(result.Files) > 0 {
						break
					}
					return result, err
				}
				if len(result.Files) > 0 {
					break
				}
				cleanupFiles(result.Files)
				return result, err
			}
			result.Files = append(result.Files, outPath)
		}
		if len(result.Files) > 0 {
			return result, nil
		}
	}

	videoURL := apiRes.Data.Play
	if req.BestQuality && apiRes.Data.HDPlay != "" {
		videoURL = apiRes.Data.HDPlay
	} else if videoURL == "" {
		videoURL = apiRes.Data.WMPlay
	}

	if videoURL != "" {
		outPath := filepath.Join(req.OutputDir, fmt.Sprintf("rip_%s_tiktok.mp4", req.TaskID))
		if err := a.downloadFile(ctx, videoURL, outPath, req.MaxSizeBytes, progress); err != nil {
			return result, err
		}
		result.Files = append(result.Files, outPath)
		return result, nil
	}

	return result, fmt.Errorf("no playable media url in tikwm response")
}

type fxTwitterResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Tweet   struct {
		ID     string `json:"id"`
		Text   string `json:"text"`
		Author struct {
			Name       string `json:"name"`
			ScreenName string `json:"screen_name"`
		} `json:"author"`
		Media struct {
			Photos []struct {
				URL string `json:"url"`
			} `json:"photos"`
			Videos []struct {
				URL string `json:"url"`
			} `json:"videos"`
		} `json:"media"`
	} `json:"tweet"`
}

func (a *APIFallbackDownloader) downloadTwitter(ctx context.Context, req DownloadRequest, progress chan<- Progress) (DownloadResult, error) {
	var result DownloadResult
	u, err := url.Parse(req.URL)
	if err != nil {
		return result, fmt.Errorf("invalid twitter url: %w", err)
	}

	matches := twitterStatusIDRegex.FindStringSubmatch(u.Path)
	if len(matches) < 2 {
		return result, fmt.Errorf("invalid tweet url format")
	}

	tweetID := matches[1]
	apiURL := fmt.Sprintf("https://api.fxtwitter.com/_/status/%s", tweetID)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return result, err
	}
	httpReq.Header.Set("User-Agent", helpers.GetUserAgent())

	resp, err := a.client.Do(httpReq)
	if err != nil {
		return result, fmt.Errorf("fxtwitter api request failed: %w", err)
	}
	defer resp.Body.Close()

	var apiRes fxTwitterResponse
	limitedBody := io.LimitReader(resp.Body, 10*1024*1024)
	if err := json.NewDecoder(limitedBody).Decode(&apiRes); err != nil {
		return result, fmt.Errorf("failed to decode fxtwitter response: %w", err)
	}

	t := apiRes.Tweet
	if t.ID == "" {
		return result, fmt.Errorf("tweet data empty from fxtwitter api")
	}

	safeTweetID := safeFilenameRegex.ReplaceAllString(t.ID, "_")
	result.Meta.Title = t.Text
	if t.Author.ScreenName != "" {
		result.Meta.Uploader = "@" + t.Author.ScreenName
	}
	result.Meta.WebpageURL = req.URL

	if req.AudioOnly {
		return result, fmt.Errorf("twitter audio fallback is unavailable")
	}

	for i, video := range t.Media.Videos {
		name := fmt.Sprintf("rip_%s_%s_vid_%d.mp4", req.TaskID, safeTweetID, i+1)
		if len(t.Media.Videos) == 1 && len(t.Media.Photos) == 0 {
			name = fmt.Sprintf("rip_%s_%s_vid.mp4", req.TaskID, safeTweetID)
		}
		outPath := filepath.Join(req.OutputDir, name)
		if err := a.downloadFile(ctx, video.URL, outPath, req.MaxSizeBytes, progress); err != nil {
			cleanupFiles(result.Files)
			return result, err
		}
		result.Files = append(result.Files, outPath)
	}

	for i, photo := range t.Media.Photos {
		ext := ".jpg"
		if photoURL, parseErr := url.Parse(photo.URL); parseErr == nil {
			if e := filepath.Ext(photoURL.Path); e != "" {
				ext = e
			}
		}
		outPath := filepath.Join(req.OutputDir, fmt.Sprintf("rip_%s_%s_img_%d%s", req.TaskID, safeTweetID, i+1, ext))
		if err := a.downloadFile(ctx, photo.URL, outPath, req.MaxSizeBytes, progress); err != nil {
			if errors.Is(err, ErrMediaTooLarge) {
				if len(result.Files) > 0 {
					break
				}
				return result, err
			}
			if len(result.Files) > 0 {
				break
			}
			cleanupFiles(result.Files)
			return result, err
		}
		result.Files = append(result.Files, outPath)
	}

	if len(result.Files) > 0 {
		return result, nil
	}

	return result, fmt.Errorf("tweet contains no downloadable media files")
}
func (a *APIFallbackDownloader) downloadDirectMedia(ctx context.Context, req DownloadRequest, progress chan<- Progress) (DownloadResult, error) {
	var result DownloadResult
	u, err := url.Parse(req.URL)
	if err != nil {
		return result, err
	}

	targetURL := req.URL
	host := strings.ToLower(u.Hostname())
	ext := strings.ToLower(filepath.Ext(u.Path))

	if (host == "imgur.com" || host == "i.imgur.com") && ext == "" {
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		if len(parts) > 0 {
			lastPart := parts[len(parts)-1]
			if lastPart != "" && !strings.Contains(lastPart, ".") {
				targetURL = fmt.Sprintf("https://i.imgur.com/%s.jpg", lastPart)
				ext = ".jpg"
			}
		}
	}

	if ext == "" {
		ext = ".jpg"
	}

	baseName := strings.TrimSuffix(filepath.Base(u.Path), filepath.Ext(u.Path))
	safeBaseName := safeFilenameRegex.ReplaceAllString(baseName, "_")
	if safeBaseName == "" || safeBaseName == "_" {
		safeBaseName = "media"
	}

	outPath := filepath.Join(req.OutputDir, fmt.Sprintf("rip_%s_%s%s", req.TaskID, safeBaseName, ext))
	if err := a.downloadFile(ctx, targetURL, outPath, req.MaxSizeBytes, progress); err != nil {
		return result, err
	}

	result.Meta.Title = safeBaseName
	result.Meta.WebpageURL = req.URL
	result.Files = append(result.Files, outPath)
	return result, nil
}

func (a *APIFallbackDownloader) downloadFile(ctx context.Context, fileURL string, targetPath string, maxBytes int64, progress chan<- Progress) error {
	if !helpers.IsURLSafe(fileURL) {
		return fmt.Errorf("insecure download target")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fileURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", helpers.GetUserAgent())

	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("media download request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("media server returned status %d", resp.StatusCode)
	}

	if maxBytes > 0 && resp.ContentLength > maxBytes {
		return ErrMediaTooLarge
	}

	out, err := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("failed to create destination file: %w", err)
	}
	defer func() { _ = out.Close() }()

	var reader io.Reader = resp.Body
	if maxBytes > 0 {
		reader = io.LimitReader(resp.Body, maxBytes+1)
	}

	total := resp.ContentLength
	var written int64
	buf := make([]byte, 32*1024)

	for {
		nr, rErr := reader.Read(buf)
		if nr > 0 {
			nw, wErr := out.Write(buf[:nr])
			if nw > 0 {
				written += int64(nw)
				if progress != nil {
					var pct float64
					if total > 0 {
						pct = (float64(written) / float64(total)) * 100
					}

					select {
					case progress <- Progress{
						Stage:   StageDownloading,
						Percent: pct,
					}:
					default:
					}
				}
			}
			if wErr != nil {
				_ = out.Close()
				_ = os.Remove(targetPath)
				return fmt.Errorf("failed to write media file: %w", wErr)
			}
			if nw != nr {
				_ = out.Close()
				_ = os.Remove(targetPath)
				return io.ErrShortWrite
			}
		}
		if rErr != nil {
			if rErr == io.EOF {
				break
			}
			_ = out.Close()
			_ = os.Remove(targetPath)
			return fmt.Errorf("failed to stream media: %w", rErr)
		}
	}

	_ = out.Close()

	if maxBytes > 0 && written > maxBytes {
		_ = os.Remove(targetPath)
		return ErrMediaTooLarge
	}

	return nil
}
func cleanupFiles(files []string) {
	for _, f := range files {
		_ = os.Remove(f)
	}
}
