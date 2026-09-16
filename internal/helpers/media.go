package helpers

import (
	"net/url"
	"path"
	"path/filepath"
	"strings"
)

var (
	ImageExtensions = map[string]bool{
		".jpg":  true,
		".jpeg": true,
		".png":  true,
		".gif":  true,
		".webp": true,
		".bmp":  true,
	}

	VideoExtensions = map[string]bool{
		".mp4":  true,
		".webm": true,
		".mov":  true,
		".mkv":  true,
	}

	AudioExtensions = map[string]bool{
		".mp3":  true,
		".m4a":  true,
		".wav":  true,
		".ogg":  true,
		".flac": true,
		".aac":  true,
		".opus": true,
		".wma":  true,
		".alac": true,
		".aiff": true,
		".aif":  true,
		".mid":  true,
		".midi": true,
		".weba": true,
		".webm": true,
	}
)

func cleanURLPath(rawURL string) string {
	clean := strings.TrimSpace(rawURL)
	if idx := strings.IndexAny(clean, "?#"); idx != -1 {
		clean = clean[:idx]
	}
	return strings.ToLower(clean)
}

func IsImageURL(rawURL string) bool {
	ext := path.Ext(cleanURLPath(rawURL))
	return ImageExtensions[ext]
}

func IsAudioURL(rawURL string) bool {
	ext := path.Ext(cleanURLPath(rawURL))
	return AudioExtensions[ext]
}

func IsMediaURL(rawURL string) bool {
	ext := path.Ext(cleanURLPath(rawURL))
	return ImageExtensions[ext] || VideoExtensions[ext] || AudioExtensions[ext]
}

func IsDirectMediaURL(u *url.URL) bool {
	if u == nil {
		return false
	}
	ext := strings.ToLower(filepath.Ext(u.Path))
	return ImageExtensions[ext] || VideoExtensions[ext] || AudioExtensions[ext]
}

func IsDirectImageURL(u *url.URL) bool {
	if u == nil {
		return false
	}
	ext := strings.ToLower(filepath.Ext(u.Path))
	return ImageExtensions[ext]
}
