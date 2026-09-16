package juicewrld

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"gobot/config"
	"gobot/internal/helpers"
)

var (
	cfgMu           sync.RWMutex
	defaultCfg      = config.DefaultConfig()
	juiceAPITimeout = defaultCfg.JuiceWRLDAPITimeout
	JuiceAPIBase    = defaultCfg.JuiceWRLDAPIURL
	JuiceEndpoint   = strings.TrimSuffix(defaultCfg.JuiceWRLDAPIURL, "/") + "/juicewrld"
	httpClient      = helpers.NewSafeHTTPClient(defaultCfg.JuiceWRLDAPITimeout)
)

func ConfigureAPI(baseURL string, timeout time.Duration) {
	cfgMu.Lock()
	defer cfgMu.Unlock()
	if baseURL != "" {
		JuiceAPIBase = baseURL
		JuiceEndpoint = strings.TrimSuffix(JuiceAPIBase, "/") + "/juicewrld"
	}
	if timeout > 0 {
		juiceAPITimeout = timeout
		httpClient = helpers.NewSafeHTTPClient(timeout)
	}
	searchCacheMu.Lock()
	searchCache = make(map[string]cacheEntry[[]Song])
	searchCacheMu.Unlock()
	browseCacheMu.Lock()
	browseCache = make(map[string]cacheEntry[[]BrowseItem])
	browseCacheMu.Unlock()
}

func GetJuiceAPITimeout() time.Duration {
	cfgMu.RLock()
	defer cfgMu.RUnlock()
	return juiceAPITimeout
}

func getJuiceEndpoint() string {
	cfgMu.RLock()
	defer cfgMu.RUnlock()
	return JuiceEndpoint
}

func getJuiceAPIBase() string {
	cfgMu.RLock()
	defer cfgMu.RUnlock()
	return JuiceAPIBase
}

func getHTTPClient() *http.Client {
	cfgMu.RLock()
	defer cfgMu.RUnlock()
	return httpClient
}

const (
	maxConcurrentRequests = 5
	maxResponseBodySize   = 10 << 20
	apiCacheTTL           = 1 * time.Hour
	maxCacheEntries       = 2000
)

type cacheEntry[T any] struct {
	value     T
	expiresAt time.Time
}

var (
	searchCacheMu sync.RWMutex
	searchCache   = make(map[string]cacheEntry[[]Song])

	browseCacheMu sync.RWMutex
	browseCache   = make(map[string]cacheEntry[[]BrowseItem])
)

func setCacheEntry[T any](cache map[string]cacheEntry[T], key string, val T) {
	if len(cache) >= maxCacheEntries {
		now := time.Now()
		for k, v := range cache {
			if now.After(v.expiresAt) {
				delete(cache, k)
			}
		}
		if len(cache) >= maxCacheEntries {
			for k := range cache {
				delete(cache, k)
			}
		}
	}
	cache[key] = cacheEntry[T]{value: val, expiresAt: time.Now().Add(apiCacheTTL)}
}

func fetchAndDecode[T any](ctx context.Context, requestURL string) (*T, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", requestURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", helpers.GetUserAgent())
	req.Header.Set("Accept", "application/json")

	resp, err := getHTTPClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned HTTP %d for %s", resp.StatusCode, requestURL)
	}

	var result T
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBodySize)).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return &result, nil
}

func SearchJuiceWRLDSongs(ctx context.Context, query string, category string) ([]Song, error) {
	qClean := strings.TrimSpace(query)
	cacheKey := fmt.Sprintf("%s|%s", category, strings.ToLower(qClean))

	searchCacheMu.RLock()
	if entry, ok := searchCache[cacheKey]; ok && time.Now().Before(entry.expiresAt) {
		res := make([]Song, len(entry.value))
		copy(res, entry.value)
		searchCacheMu.RUnlock()
		return res, nil
	}
	searchCacheMu.RUnlock()

	endpoint := getJuiceEndpoint()
	doSearch := func(param, q string) ([]Song, error) {
		reqURL := fmt.Sprintf("%s/songs/?%s=%s&page_size=100", endpoint, param, url.QueryEscape(q))
		if category != "" {
			reqURL += "&category=" + url.QueryEscape(category)
		}
		resp, err := fetchAndDecode[SongsResponse](ctx, reqURL)
		if err != nil {
			return nil, err
		}
		return resp.Results, nil
	}

	type searchAttempt struct {
		param string
		q     string
	}
	var attempts []searchAttempt
	attempts = append(attempts, searchAttempt{"search", qClean})
	if ap := addContractionApostrophes(qClean); ap != "" && ap != qClean {
		attempts = append(attempts, searchAttempt{"search", ap})
	}
	attempts = append(attempts, searchAttempt{"searchall", qClean})
	if ap := addContractionApostrophes(qClean); ap != "" && ap != qClean {
		attempts = append(attempts, searchAttempt{"searchall", ap})
	}

	var lastErr error
	for _, attempt := range attempts {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		songs, err := doSearch(attempt.param, attempt.q)
		if err != nil {
			lastErr = err
			continue
		}
		lastErr = nil
		if len(songs) > 0 {
			res := make([]Song, len(songs))
			copy(res, songs)
			searchCacheMu.Lock()
			setCacheEntry(searchCache, cacheKey, res)
			searchCacheMu.Unlock()
			return songs, nil
		}
	}

	if lastErr != nil {
		return nil, lastErr
	}
	return nil, nil
}

func BrowseJuiceWRLDFiles(ctx context.Context, query string, dirPath string) ([]BrowseItem, error) {
	qClean := strings.TrimSpace(query)
	cacheKey := fmt.Sprintf("%s|%s", dirPath, strings.ToLower(qClean))

	browseCacheMu.RLock()
	if entry, ok := browseCache[cacheKey]; ok && time.Now().Before(entry.expiresAt) {
		res := make([]BrowseItem, len(entry.value))
		copy(res, entry.value)
		browseCacheMu.RUnlock()
		return res, nil
	}
	browseCacheMu.RUnlock()

	qEncoded := url.QueryEscape(qClean)
	reqURL := fmt.Sprintf("%s/files/browse/?search=%s", getJuiceEndpoint(), qEncoded)
	if dirPath != "" {
		reqURL += "&path=" + url.QueryEscape(dirPath)
	}

	resp, err := fetchAndDecode[BrowseResponse](ctx, reqURL)
	if err != nil {
		return nil, err
	}

	if len(resp.Items) > 0 {
		res := make([]BrowseItem, len(resp.Items))
		copy(res, resp.Items)
		browseCacheMu.Lock()
		setCacheEntry(browseCache, cacheKey, res)
		browseCacheMu.Unlock()
	}

	return resp.Items, nil
}

func GetRandomRadioSong(ctx context.Context) (*RadioSongResponse, error) {
	reqURL := fmt.Sprintf("%s/radio/random/", getJuiceEndpoint())
	return fetchAndDecode[RadioSongResponse](ctx, reqURL)
}

func GetDownloadURL(path string) string {
	return fmt.Sprintf("%s/files/download/?path=%s", getJuiceEndpoint(), url.QueryEscape(path))
}

func BuildFullImageURL(imgURL string) string {
	if imgURL == "" {
		return ""
	}
	if strings.HasPrefix(imgURL, "http://") || strings.HasPrefix(imgURL, "https://") {
		return imgURL
	}
	if !strings.HasPrefix(imgURL, "/") {
		imgURL = "/" + imgURL
	}
	return getJuiceAPIBase() + imgURL
}

func extractPathFromURL(urlOrPath string) string {
	u, err := url.Parse(urlOrPath)
	if err != nil {
		return urlOrPath
	}
	if p := u.Query().Get("path"); p != "" {
		return p
	}
	return urlOrPath
}
