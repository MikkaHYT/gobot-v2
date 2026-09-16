package helpers

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	DefaultUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"
)

var (
	customUserAgentMu sync.RWMutex
	customUserAgent   string
)

func SetUserAgent(ua string) {
	customUserAgentMu.Lock()
	defer customUserAgentMu.Unlock()
	customUserAgent = ua
}

func GetUserAgent() string {
	customUserAgentMu.RLock()
	defer customUserAgentMu.RUnlock()
	if customUserAgent != "" {
		return customUserAgent
	}
	return DefaultUserAgent
}

func IsValidHTTPURL(raw string) bool {
	if raw == "" {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil || u == nil {
		return false
	}
	return (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

func CleanMediaURL(raw string) (string, bool) {
	s := strings.TrimSpace(raw)
	s = strings.TrimPrefix(s, "<")
	s = strings.TrimSuffix(s, ">")
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") {
		return s, true
	}
	lower := strings.ToLower(s)
	if strings.HasPrefix(lower, "youtube.com/") ||
		strings.HasPrefix(lower, "www.youtube.com/") ||
		strings.HasPrefix(lower, "youtu.be/") ||
		strings.HasPrefix(lower, "soundcloud.com/") ||
		strings.HasPrefix(lower, "m.soundcloud.com/") {
		return "https://" + s, true
	}
	return s, false
}

func IsURLSafe(input string) bool {
	u, err := url.Parse(input)
	if err != nil {
		return false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return false
	}

	hostname := strings.ToLower(u.Hostname())
	if hostname == "" {
		return false
	}

	if isKnownPublicMediaDomain(hostname) {
		return true
	}

	ips, err := net.LookupIP(hostname)
	if err != nil || len(ips) == 0 {
		return false
	}

	for _, ip := range ips {
		if isPrivateOrReservedIP(ip) {
			return false
		}
	}
	return true
}

func isKnownPublicMediaDomain(host string) bool {
	if host == "juicewrldapi.com" || strings.HasSuffix(host, ".juicewrldapi.com") {
		return true
	}
	if host == "youtube.com" || strings.HasSuffix(host, ".youtube.com") || host == "youtu.be" {
		return true
	}
	if host == "soundcloud.com" || strings.HasSuffix(host, ".soundcloud.com") || strings.HasSuffix(host, ".sndcdn.com") {
		return true
	}
	if host == "spotify.com" || strings.HasSuffix(host, ".spotify.com") || strings.HasSuffix(host, ".scdn.co") {
		return true
	}
	if host == "discord.com" || host == "cdn.discordapp.com" || host == "media.discordapp.net" {
		return true
	}
	return false
}

var (
	ipv4ReservedNets = parseCIDRList([]string{
		"0.0.0.0/8",
		"10.0.0.0/8",
		"100.64.0.0/10",
		"127.0.0.0/8",
		"169.254.0.0/16",
		"172.16.0.0/12",
		"192.0.0.0/24",
		"192.0.2.0/24",
		"192.168.0.0/16",
		"198.18.0.0/15",
		"198.51.100.0/24",
		"203.0.113.0/24",
		"224.0.0.0/4",
		"240.0.0.0/4",
		"255.255.255.255/32",
	})

	ipv6ReservedNets = parseCIDRList([]string{
		"::/128",
		"::1/128",
		"::ffff:0:0/96",
		"100::/64",
		"2001:db8::/32",
		"fc00::/7",
		"fe80::/10",
		"ff00::/8",
	})
)

func parseCIDRList(cidrs []string) []*net.IPNet {
	nets := make([]*net.IPNet, 0, len(cidrs))
	for _, c := range cidrs {
		_, block, err := net.ParseCIDR(c)
		if err == nil {
			nets = append(nets, block)
		}
	}
	return nets
}

func isPrivateOrReservedIP(ip net.IP) bool {
	if ip == nil {
		return true
	}

	if ipv4 := ip.To4(); ipv4 != nil {
		for _, block := range ipv4ReservedNets {
			if block.Contains(ipv4) {
				return true
			}
		}
		return false
	}

	for _, block := range ipv6ReservedNets {
		if block.Contains(ip) {
			return true
		}
	}
	return false
}

var safeTransport = &http.Transport{
	DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}

		ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
		if err != nil {
			return nil, err
		}
		if len(ips) == 0 {
			return nil, fmt.Errorf("no IP addresses found for host: %s", host)
		}

		for _, ip := range ips {
			if isPrivateOrReservedIP(ip) {
				return nil, fmt.Errorf("connection to private/reserved IP blocked: %s", ip.String())
			}
		}

		dialer := &net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}
		return dialer.DialContext(ctx, network, net.JoinHostPort(ips[0].String(), port))
	},
	ResponseHeaderTimeout: 30 * time.Second,
	MaxIdleConns:          100,
	IdleConnTimeout:       90 * time.Second,
	TLSHandshakeTimeout:   10 * time.Second,
	ExpectContinueTimeout: 1 * time.Second,
}

func NewSafeHTTPClient(timeout time.Duration) *http.Client {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: safeTransport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("stopped after 5 redirects")
			}
			if req.URL != nil && !IsURLSafe(req.URL.String()) {
				return fmt.Errorf("redirect to unsafe destination blocked: %s", req.URL.String())
			}
			return nil
		},
	}
}

func NewSafeStreamingHTTPClient() *http.Client {
	return &http.Client{
		Timeout:   0,
		Transport: safeTransport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("stopped after 5 redirects")
			}
			if req.URL != nil && !IsURLSafe(req.URL.String()) {
				return fmt.Errorf("redirect to unsafe destination blocked: %s", req.URL.String())
			}
			return nil
		},
	}
}

var imageHTTPClient = NewSafeHTTPClient(DurationTimeoutHTTP)

func FetchImageData(imageURL string, optCtx ...context.Context) ([]byte, string, error) {
	if !IsURLSafe(imageURL) {
		return nil, "", fmt.Errorf("insecure or invalid image URL: %s", imageURL)
	}
	ctx := context.Background()
	if len(optCtx) > 0 && optCtx[0] != nil {
		ctx = optCtx[0]
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, imageURL, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("User-Agent", GetUserAgent())
	req.Header.Set("Accept", "image/*,*/*;q=0.8")

	resp, err := imageHTTPClient.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("failed to fetch image: HTTP status %d", resp.StatusCode)
	}

	const maxBytes = int64(10 << 20)
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return nil, "", fmt.Errorf("failed to read image stream: %w", err)
	}
	if int64(len(data)) > maxBytes {
		return nil, "", fmt.Errorf("image exceeds maximum allowed size of %d MB", maxBytes/(1024*1024))
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType == "" || contentType == "application/octet-stream" {
		contentType = http.DetectContentType(data)
	}
	if idx := strings.Index(contentType, ";"); idx != -1 {
		contentType = strings.TrimSpace(contentType[:idx])
	}

	detectedMIME := http.DetectContentType(data)
	if idx := strings.Index(detectedMIME, ";"); idx != -1 {
		detectedMIME = strings.TrimSpace(detectedMIME[:idx])
	}

	if strings.HasPrefix(detectedMIME, "image/") {
		contentType = detectedMIME
	} else if !strings.HasPrefix(contentType, "image/") {
		return nil, "", fmt.Errorf("file payload is not a valid image (detected: %s)", detectedMIME)
	}

	return data, contentType, nil
}

func SanitizeURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "[invalid-url]"
	}
	u.RawQuery = ""
	u.User = nil
	return u.String()
}
