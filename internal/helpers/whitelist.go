package helpers

import (
	"net/url"
	"os"
	"strings"
)

var defaultAllowedDomains = []string{
	"youtube.com",
	"youtu.be",
	"tiktok.com",
	"twitter.com",
	"x.com",
	"vxtwitter.com",
	"fxtwitter.com",
	"fixupx.com",
	"instagram.com",
	"ddinstagram.com",
	"reddit.com",
	"redd.it",
	"twitch.tv",
	"streamable.com",
	"vimeo.com",
	"dailymotion.com",
	"bilibili.com",
	"tumblr.com",
	"facebook.com",
	"fb.watch",
	"threads.net",
	"pinterest.com",
	"pin.it",
	"soundcloud.com",
	"spotify.com",
	"bandcamp.com",
	"apple.com",
	"discordapp.com",
	"discord.com",
	"discordapp.net",
	"imgur.com",
	"giphy.com",
	"tenor.com",
	"juicewrldapi.com",
}

func IsDomainAllowed(rawURL string) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Host == "" {
		return false
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return false
	}

	host := strings.ToLower(parsed.Hostname())
	if host == "localhost" || host == "127.0.0.1" || host == "::1" || host == "0.0.0.0" {
		return false
	}

	for _, domain := range defaultAllowedDomains {
		if host == domain || strings.HasSuffix(host, "."+domain) {
			return true
		}
	}

	if envExtra := os.Getenv("RIPPER_ALLOWED_DOMAINS"); envExtra != "" {
		for _, extra := range strings.Split(envExtra, ",") {
			trimmed := strings.ToLower(strings.TrimSpace(extra))
			if trimmed != "" && (host == trimmed || strings.HasSuffix(host, "."+trimmed)) {
				return true
			}
		}
	}
	return false
}
