package ripper

import (
	"context"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"gobot/internal/helpers"
	"gobot/internal/logger"
)

type CompositeDownloader struct {
	primary  Downloader
	fallback Downloader
}

func NewCompositeDownloader(primary Downloader, fallback Downloader) *CompositeDownloader {
	return &CompositeDownloader{
		primary:  primary,
		fallback: fallback,
	}
}

func (c *CompositeDownloader) Supports(targetURL *url.URL) bool {
	if c.primary != nil && c.primary.Supports(targetURL) {
		return true
	}
	if c.fallback != nil && c.fallback.Supports(targetURL) {
		return true
	}
	return false
}

func prefersFallbackFirst(targetURL *url.URL) bool {
	if targetURL == nil {
		return false
	}
	host := strings.ToLower(targetURL.Hostname())
	path := strings.ToLower(targetURL.Path)

	if (host == "tiktok.com" || strings.HasSuffix(host, ".tiktok.com")) && strings.Contains(path, "/photo/") {
		return true
	}

	if helpers.IsDirectImageURL(targetURL) {
		return true
	}
	if host == "imgur.com" || host == "i.imgur.com" {
		return true
	}

	return false
}

func (c *CompositeDownloader) Download(ctx context.Context, req DownloadRequest, progress chan<- Progress) (DownloadResult, error) {
	if c.primary == nil {
		return DownloadResult{}, errors.New("no primary downloader configured")
	}

	u, parseErr := url.Parse(req.URL)

	if parseErr == nil && c.fallback != nil && c.fallback.Supports(u) && prefersFallbackFirst(u) {
		fallbackResult, fallbackErr := c.fallback.Download(ctx, req, progress)
		if fallbackErr == nil && len(fallbackResult.Files) > 0 {
			return fallbackResult, nil
		}
		if errors.Is(fallbackErr, ErrMediaTooLarge) || ctx.Err() != nil {
			return fallbackResult, fallbackErr
		}
		logger.Warnf("[RIPPER] Fast-path fallback failed for %s, trying primary: %v", req.URL, fallbackErr)
		cleanDirectoryFiles(req.OutputDir)
	}

	result, err := c.primary.Download(ctx, req, progress)
	if err == nil && len(result.Files) > 0 {
		return result, nil
	}

	if errors.Is(err, ErrMediaTooLarge) || ctx.Err() != nil {
		return result, err
	}

	if parseErr != nil || c.fallback == nil || !c.fallback.Supports(u) {
		return result, err
	}

	logger.Infof("[RIPPER] Primary download failed for %s, trying direct API fallback: %v", req.URL, err)
	cleanDirectoryFiles(req.OutputDir)

	fallbackResult, fallbackErr := c.fallback.Download(ctx, req, progress)
	if fallbackErr == nil && len(fallbackResult.Files) > 0 {
		return fallbackResult, nil
	}

	if errors.Is(fallbackErr, ErrMediaTooLarge) {
		return fallbackResult, fallbackErr
	}

	return result, fmtFallbackError(err, fallbackErr)
}

func cleanDirectoryFiles(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		_ = os.Remove(filepath.Join(dir, entry.Name()))
	}
}

func fmtFallbackError(primaryErr, fallbackErr error) error {
	if fallbackErr != nil {
		return errors.Join(primaryErr, fallbackErr)
	}
	return primaryErr
}
