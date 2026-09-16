package helpers

import (
	"net/url"
	"path"
	"path/filepath"
	"strings"
)

var SupportedAudioExtensions = AudioExtensions

func IsAudioExtension(filenameOrPath string) bool {
	clean := strings.ToLower(strings.TrimSpace(filenameOrPath))
	ext := filepath.Ext(clean)
	if ext == "" {
		ext = path.Ext(clean)
	}
	return AudioExtensions[ext]
}

func IsDirectAudioURL(rawURL string) bool {
	if rawURL == "" {
		return false
	}
	u, err := url.Parse(rawURL)
	if err != nil || u == nil {
		return false
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	if host == "juicewrldapi.com" || strings.HasSuffix(host, ".juicewrldapi.com") {
		path := strings.ToLower(u.Path)
		if strings.HasPrefix(path, "/song/") {
			return false
		}
		if strings.Contains(path, "/download") || strings.Contains(path, "/files/download") ||
			strings.Contains(path, "/track/") || strings.Contains(strings.ToLower(u.RawQuery), "path=") ||
			IsAudioURL(path) {
			return true
		}
		return false
	}
	return IsAudioURL(u.Path)
}
