package radio

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"gobot/internal/helpers"
)

var newlineSpaceReplacer = strings.NewReplacer("\n", " ", "\r", " ")

type CacheEntry struct {
	Key        string    `json:"key"`
	Path       string    `json:"path"`
	Size       int64     `json:"size"`
	LastAccess time.Time `json:"last_access"`
	AccessSeq  uint64    `json:"access_seq"`
	Track      *Track    `json:"track"`
}

type CacheOverage struct {
	OverSongs int
	OverBytes int64
	Leased    int
}

type CacheDiag func(CacheOverage)

type countingReader struct {
	r       io.Reader
	n       atomic.Int64
	limit   int64
	onLimit func()
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	if n > 0 {
		total := c.n.Add(int64(n))
		if c.limit > 0 && total > c.limit && c.onLimit != nil {
			c.onLimit()
		}
	}
	return n, err
}

type LRUSongCache struct {
	mu                sync.Mutex
	dir               string
	maxBytes          int64
	maxSongs          int
	maxDownloadSizeMB int64
	totalBytes        int64
	seqCounter        uint64
	entries           map[string]*CacheEntry
	leaseChecker      func(path string) bool
	diagHook          CacheDiag
	downloads         sync.Map
}

const (
	DefaultMaxCachedSongs = 50
	DefaultMaxCacheBytes  = 500 * 1024 * 1024
	MaxDownloadSizeBytes  = 100 * 1024 * 1024
)

var (
	radioPathMu       sync.RWMutex
	configuredSongDir string
	configuredTempDir string
	configuredCookies string
)

func ConfigureRadioStorage(songCacheDir, tempDir, cookiesPath string) {
	radioPathMu.Lock()
	defer radioPathMu.Unlock()
	configuredSongDir = songCacheDir
	configuredTempDir = tempDir
	configuredCookies = cookiesPath
}

func dataCacheDir() string {
	radioPathMu.RLock()
	defer radioPathMu.RUnlock()
	if configuredSongDir != "" {
		return filepath.Clean(configuredSongDir)
	}
	return filepath.Join("data", "cache", "songs")
}
func NewLRUSongCache(dir string, maxBytes int64, maxSongs int) *LRUSongCache {
	if maxBytes <= 0 {
		maxBytes = DefaultMaxCacheBytes
	}
	if maxSongs <= 0 {
		maxSongs = DefaultMaxCachedSongs
	}
	if dir == "" {
		dir = dataCacheDir()
	}
	_ = os.MkdirAll(dir, 0755)

	cache := &LRUSongCache{
		dir:               filepath.Clean(dir),
		maxBytes:          maxBytes,
		maxSongs:          maxSongs,
		maxDownloadSizeMB: 100,
		entries:           make(map[string]*CacheEntry),
	}
	cache.ScanExistingFiles()
	return cache
}

func (c *LRUSongCache) SetMaxDownloadSizeMB(mb int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if mb <= 0 {
		mb = 100
	}
	c.maxDownloadSizeMB = mb
}

func (c *LRUSongCache) GetMaxDownloadSizeMB() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.maxDownloadSizeMB <= 0 {
		return 100
	}
	return c.maxDownloadSizeMB
}

func (c *LRUSongCache) SetLeaseChecker(checker func(path string) bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.leaseChecker = checker
}

func (c *LRUSongCache) SetDiagHook(hook CacheDiag) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.diagHook = hook
}

func (c *LRUSongCache) OwnsPath(path string) bool {
	if c == nil || path == "" {
		return false
	}
	c.mu.Lock()
	dir := c.dir
	c.mu.Unlock()
	return pathWithinRoot(dir, path)
}

func (c *LRUSongCache) ScanExistingFiles() {
	dir := c.CacheDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}

	scanned := make(map[string]*CacheEntry)
	var totalBytes int64
	var seq uint64

	for _, f := range entries {
		if f.IsDir() || !strings.HasSuffix(f.Name(), ".opus") {
			continue
		}
		info, errI := f.Info()
		if errI != nil {
			continue
		}

		key := strings.TrimSuffix(f.Name(), ".opus")
		key = strings.TrimPrefix(key, "song_")
		fullPath := filepath.Join(dir, f.Name())
		metaPath := filepath.Join(dir, fmt.Sprintf("song_%s.meta.json", key))

		var track *Track
		if metaBytes, errMeta := os.ReadFile(metaPath); errMeta == nil {
			var loadedTrack Track
			if errU := json.Unmarshal(metaBytes, &loadedTrack); errU == nil && loadedTrack.Title != "" {
				loadedTrack.Title = helpers.CleanTrackTitle(loadedTrack.Title)
				track = &loadedTrack
			}
		}

		if track == nil {
			continue
		}

		seq++
		scanned[key] = &CacheEntry{
			Key:        key,
			Path:       fullPath,
			Size:       info.Size(),
			LastAccess: info.ModTime(),
			AccessSeq:  seq,
			Track:      track,
		}
		totalBytes += info.Size()
	}

	c.mu.Lock()
	c.entries = scanned
	c.totalBytes = totalBytes
	c.seqCounter = seq
	removePaths := c.evictIfNecessaryLocked()
	c.mu.Unlock()
	removeCachePaths(removePaths)
}

func (c *LRUSongCache) CacheDir() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.dir
}

func (c *LRUSongCache) RandomTrack(excludeTitles []string, mode string) (*Track, string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.entries) == 0 {
		return nil, ""
	}

	excludeMap := make(map[string]bool, len(excludeTitles))
	for _, t := range excludeTitles {
		clean := strings.ToLower(strings.TrimSpace(t))
		if clean != "" {
			excludeMap[clean] = true
		}
	}

	var eligibleKeys []string
	for k, e := range c.entries {
		if e == nil || e.Track == nil {
			continue
		}
		if !IsRadioEligible(e.Track) {
			continue
		}
		titleClean := strings.ToLower(strings.TrimSpace(e.Track.Title))
		if excludeMap[titleClean] {
			continue
		}
		if !MatchesMode(e.Track, mode) {
			continue
		}
		eligibleKeys = append(eligibleKeys, k)
	}

	if len(eligibleKeys) == 0 {
		return nil, ""
	}

	var b [8]byte
	_, _ = rand.Read(b[:])
	idx := int(binary.LittleEndian.Uint64(b[:]) % uint64(len(eligibleKeys)))

	pickKey := eligibleKeys[idx]
	entry := c.entries[pickKey]
	entry.LastAccess = time.Now()
	c.seqCounter++
	entry.AccessSeq = c.seqCounter

	return cloneTrack(entry.Track), entry.Path
}

func (c *LRUSongCache) CacheKey(t *Track) string {
	if t == nil {
		return ""
	}
	raw := t.URL
	if raw == "" {
		raw = t.WebpageURL
	}
	if raw != "" {
		raw = strings.TrimSpace(raw)
		if u, err := url.Parse(raw); err == nil && u.Scheme != "" && u.Host != "" {
			u.Scheme = strings.ToLower(u.Scheme)
			u.Host = strings.ToLower(u.Host)
			raw = u.String()
		}
	} else if t.Title != "" {
		raw = strings.ToLower(strings.TrimSpace(t.Title))
	}
	if raw == "" {
		return ""
	}
	hash := sha256.Sum256([]byte(raw))
	return fmt.Sprintf("%x", hash)[:32]
}

func (c *LRUSongCache) Get(t *Track) (string, bool) {
	if t == nil {
		return "", false
	}
	key := c.CacheKey(t)

	c.mu.Lock()
	defer c.mu.Unlock()

	if key != "" {
		if entry, ok := c.entries[key]; ok && entry != nil {
			if _, err := os.Stat(entry.Path); err == nil {
				entry.LastAccess = time.Now()
				c.seqCounter++
				entry.AccessSeq = c.seqCounter
				return entry.Path, true
			}
			c.totalBytes -= entry.Size
			delete(c.entries, key)
		}
	}

	if t.Title != "" {
		cleanTitle := strings.ToLower(strings.TrimSpace(t.Title))
		for k, e := range c.entries {
			if e != nil && e.Track != nil && strings.ToLower(strings.TrimSpace(e.Track.Title)) == cleanTitle {
				if _, err := os.Stat(e.Path); err == nil {
					e.LastAccess = time.Now()
					c.seqCounter++
					e.AccessSeq = c.seqCounter
					return e.Path, true
				}
				c.totalBytes -= e.Size
				delete(c.entries, k)
			}
		}
	}

	return "", false
}

func (c *LRUSongCache) Put(t *Track, tempFilePath string) (string, error) {
	if t == nil || tempFilePath == "" {
		return "", fmt.Errorf("invalid arguments to Put")
	}

	key := c.CacheKey(t)
	if key == "" {
		return "", fmt.Errorf("cannot derive cache key for track")
	}

	cacheDir := c.CacheDir()
	destPath := filepath.Join(cacheDir, fmt.Sprintf("song_%s.opus", key))
	metaPath := filepath.Join(cacheDir, fmt.Sprintf("song_%s.meta.json", key))

	if filepath.Clean(tempFilePath) != filepath.Clean(destPath) {
		if err := os.Rename(tempFilePath, destPath); err != nil {
			if errCopy := copyFile(tempFilePath, destPath); errCopy != nil {
				return "", fmt.Errorf("failed to copy into cache: %w", errCopy)
			}
			_ = os.Remove(tempFilePath)
		}
	}

	info, err := os.Stat(destPath)
	if err != nil {
		return "", fmt.Errorf("failed to stat cached file: %w", err)
	}

	trackClone := cloneTrack(t)
	metaBytes, errM := json.Marshal(trackClone)
	if errM != nil {
		_ = os.Remove(destPath)
		return "", fmt.Errorf("failed to marshal cache metadata: %w", errM)
	}
	if errW := atomicWriteFile(metaPath, metaBytes, 0644); errW != nil {
		_ = os.Remove(destPath)
		return "", fmt.Errorf("failed to write cache metadata: %w", errW)
	}

	c.mu.Lock()

	if prev, exists := c.entries[key]; exists && prev != nil {
		c.totalBytes -= prev.Size
	}

	c.seqCounter++
	c.entries[key] = &CacheEntry{
		Key:        key,
		Path:       destPath,
		Size:       info.Size(),
		LastAccess: time.Now(),
		AccessSeq:  c.seqCounter,
		Track:      trackClone,
	}
	c.totalBytes += info.Size()

	removePaths := c.evictIfNecessaryLocked()
	c.mu.Unlock()
	removeCachePaths(removePaths)
	return destPath, nil
}

func (c *LRUSongCache) evictIfNecessaryLocked() []string {
	if len(c.entries) <= c.maxSongs && c.totalBytes <= c.maxBytes {
		return nil
	}

	list := make([]*CacheEntry, 0, len(c.entries))
	for _, e := range c.entries {
		list = append(list, e)
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].LastAccess.Equal(list[j].LastAccess) {
			return list[i].AccessSeq < list[j].AccessSeq
		}
		return list[i].LastAccess.Before(list[j].LastAccess)
	})

	var toRemovePaths []string
	for _, e := range list {
		if len(c.entries) <= c.maxSongs && c.totalBytes <= c.maxBytes {
			break
		}

		if c.leaseChecker != nil && c.leaseChecker(e.Path) {
			continue
		}

		metaPath := filepath.Join(c.dir, fmt.Sprintf("song_%s.meta.json", e.Key))
		toRemovePaths = append(toRemovePaths, e.Path, metaPath)

		c.totalBytes -= e.Size
		delete(c.entries, e.Key)
	}

	if (len(c.entries) > c.maxSongs || c.totalBytes > c.maxBytes) && len(toRemovePaths) == 0 && c.diagHook != nil {
		overSongs := len(c.entries) - c.maxSongs
		if overSongs < 0 {
			overSongs = 0
		}
		overBytes := c.totalBytes - c.maxBytes
		if overBytes < 0 {
			overBytes = 0
		}
		leased := 0
		for _, e := range list {
			if c.leaseChecker != nil && c.leaseChecker(e.Path) {
				leased++
			}
		}
		hook := c.diagHook
		hook(CacheOverage{OverSongs: overSongs, OverBytes: overBytes, Leased: leased})
	}

	return toRemovePaths
}

func removeCachePaths(paths []string) {
	for _, path := range paths {
		_ = os.Remove(path)
	}
}

func atomicWriteFile(filename string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(filename)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	tmpFile, err := os.CreateTemp(dir, "meta_*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmpFile.Name()

	if _, err := tmpFile.Write(data); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmpFile.Sync(); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		_ = os.Remove(tmpName)
		return err
	}

	if err := os.Rename(tmpName, filename); err != nil {
		_ = os.Remove(filename)
		if err2 := os.Rename(tmpName, filename); err2 != nil {
			_ = os.Remove(tmpName)
			return err2
		}
	}
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}

	if _, err = io.Copy(out, in); err != nil {
		_ = out.Close()
		_ = os.Remove(dst)
		return err
	}

	if err = out.Close(); err != nil {
		_ = os.Remove(dst)
		return err
	}

	return nil
}

type downloadCall struct {
	done  chan struct{}
	val   string
	owned bool
	err   error
}

func EnsureTrackCachedWithCache(ctx context.Context, cache *LRUSongCache, t *Track) (savedPath string, owned bool, err error) {
	if t == nil {
		return "", false, fmt.Errorf("nil track")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if cache == nil {
		cache = NewLRUSongCache("", DefaultMaxCacheBytes, DefaultMaxCachedSongs)
	}

	if cachedPath, ok := cache.Get(t); ok {
		if t != nil {
			t.Title = helpers.CleanTrackTitle(t.Title)
		}
		if t != nil && (t.Duration <= 0 || t.Duration == 180) {
			if dur := ProbeFileDuration(cachedPath); dur > 0 {
				t.Duration = dur
			}
		}
		return cachedPath, false, nil
	}

	targetURL := t.WebpageURL
	if targetURL == "" {
		targetURL = t.URL
	}
	if targetURL == "" {
		return "", false, fmt.Errorf("no URL for track `%s`", t.Title)
	}

	key := cache.CacheKey(t)
	if key == "" {
		return "", false, fmt.Errorf("cannot derive cache key for track")
	}

	call := &downloadCall{done: make(chan struct{})}
	actual, loaded := cache.downloads.LoadOrStore(key, call)
	if loaded {
		inFlightCall, ok := actual.(*downloadCall)
		if !ok {
			return "", false, fmt.Errorf("unexpected download call type in cache")
		}
		select {
		case <-ctx.Done():
			return "", false, ctx.Err()
		case <-inFlightCall.done:
			return inFlightCall.val, inFlightCall.owned, inFlightCall.err
		}
	}

	defer func() {
		call.val = savedPath
		call.owned = owned
		call.err = err
		close(call.done)
		cache.downloads.Delete(key)
	}()

	jobDir, errDir := os.MkdirTemp(RadioBufferDir(), "download_*")
	if errDir != nil {
		return "", false, fmt.Errorf("failed to create download temp dir: %w", errDir)
	}
	defer func() { _ = os.RemoveAll(jobDir) }()

	userAgent := helpers.GetUserAgent()
	directURL := resolveDirectTrackURL(t)
	if directURL != "" {
		return downloadDirectStream(ctx, cache, t, directURL, jobDir, key, userAgent)
	}

	return downloadRemoteWithYtdlp(ctx, cache, t, targetURL, jobDir, key, userAgent)
}

func resolveDirectTrackURL(t *Track) string {
	if helpers.IsDirectAudioURL(t.URL) && t.URL != "" && helpers.IsURLSafe(t.URL) {
		return t.URL
	}
	if helpers.IsDirectAudioURL(t.WebpageURL) && t.WebpageURL != "" && helpers.IsURLSafe(t.WebpageURL) {
		return t.WebpageURL
	}
	return ""
}

func commitTrackToCache(cache *LRUSongCache, t *Track, opusPath, key string) (string, bool, error) {
	trackCopy := cloneTrack(t)
	if trackCopy != nil {
		trackCopy.Title = helpers.CleanTrackTitle(trackCopy.Title)
	}
	if t != nil {
		t.Title = helpers.CleanTrackTitle(t.Title)
	}
	if dur := ProbeFileDuration(opusPath); dur > 0 {
		if trackCopy != nil {
			trackCopy.Duration = dur
		}
		if t != nil {
			t.Duration = dur
		}
	}
	savedPath, errPut := cache.Put(trackCopy, opusPath)
	if errPut != nil {
		scratchPath := filepath.Join(RadioBufferDir(), fmt.Sprintf("tmp_%s_%d.opus", key, time.Now().UnixNano()))
		if errCopy := copyFile(opusPath, scratchPath); errCopy == nil {
			return scratchPath, true, nil
		}
		return "", false, fmt.Errorf("failed to commit cached track: %w", errPut)
	}
	return savedPath, false, nil
}

func downloadDirectStream(ctx context.Context, cache *LRUSongCache, t *Track, directURL, jobDir, key, userAgent string) (string, bool, error) {
	safeClient := helpers.NewSafeStreamingHTTPClient()
	reqCtx, cancelReq := context.WithTimeout(ctx, 3*time.Minute)
	defer cancelReq()

	var lastHtmlErr error
	var saved string
	const directAttempts = 4

	for attempt := 1; attempt <= directAttempts && saved == ""; attempt++ {
		if ctx.Err() != nil {
			return "", false, ctx.Err()
		}
		req, errReq := http.NewRequestWithContext(reqCtx, http.MethodGet, directURL, nil)
		if errReq != nil {
			return "", false, fmt.Errorf("failed to build download request for `%s`: %w", directURL, errReq)
		}
		req.Header.Set("User-Agent", userAgent)

		resp, errResp := safeClient.Do(req)
		if errResp != nil {
			lastHtmlErr = fmt.Errorf("network error fetching audio stream from `%s`: %w", directURL, errResp)
		} else if resp != nil {
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				contentType := strings.ToLower(resp.Header.Get("Content-Type"))
				if strings.Contains(contentType, "text/html") {
					peek, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
					preview := strings.TrimSpace(string(peek))
					if len(preview) > 300 {
						preview = preview[:300] + "..."
					}
					preview = newlineSpaceReplacer.Replace(preview)
					lastHtmlErr = fmt.Errorf("expected audio stream but received HTML webpage from `%s` (content-type=%s, attempt %d/%d, preview=%q)", directURL, resp.Header.Get("Content-Type"), attempt, directAttempts, preview)
					_ = resp.Body.Close()
					resp = nil
				} else {
					maxBytes := int64(cache.GetMaxDownloadSizeMB()) * 1024 * 1024
					ffmpegCtx, ffmpegCancel := context.WithCancel(reqCtx)
					defer ffmpegCancel()

					var overflowHit atomic.Bool
					onOverflow := func() {
						if overflowHit.CompareAndSwap(false, true) {
							ffmpegCancel()
							_ = resp.Body.Close()
						}
					}

					cr := &countingReader{
						r:       io.LimitReader(resp.Body, maxBytes+1),
						limit:   maxBytes,
						onLimit: onOverflow,
					}
					tmpOpus := filepath.Join(jobDir, "direct_stream.opus")

					args := []string{
						"-v", "error",
						"-y",
						"-i", "pipe:0",
						"-vn",
						"-map", "a:0",
						"-c:a", "libopus",
						"-b:a", "196k",
						"-ar", "48000",
						"-ac", "2",
						"-application", "audio",
						"-f", "opus",
						"-vbr", "on",
						tmpOpus,
					}

					watchDone := make(chan struct{})
					go func() {
						ticker := time.NewTicker(200 * time.Millisecond)
						defer ticker.Stop()
						for {
							select {
							case <-watchDone:
								return
							case <-ffmpegCtx.Done():
								return
							case <-ticker.C:
								if fi, err := os.Stat(tmpOpus); err == nil && fi.Size() > maxBytes {
									onOverflow()
									return
								}
							}
						}
					}()

					cmd := exec.CommandContext(ffmpegCtx, helpers.GetFFmpegPath(), args...)
					cmd.Stdin = cr
					var stderr bytes.Buffer
					cmd.Stderr = &stderr
					errRun := cmd.Run()
					close(watchDone)
					_ = resp.Body.Close()

					if cr.n.Load() > maxBytes || overflowHit.Load() {
						_ = os.Remove(tmpOpus)
						return "", false, fmt.Errorf("direct stream from `%s` exceeded maximum allowed download size of %dMB", directURL, cache.GetMaxDownloadSizeMB())
					}
					if errRun != nil {
						_ = os.Remove(tmpOpus)
						lastHtmlErr = fmt.Errorf("failed to decode stream from `%s`: %w (stderr: %s)", directURL, errRun, sanitizeStderr(stderr.String(), 1500))
					} else {
						if errCtx := ctx.Err(); errCtx != nil {
							_ = os.Remove(tmpOpus)
							return "", false, errCtx
						}
						stat, errStat := os.Stat(tmpOpus)
						if errStat != nil || stat.Size() > maxBytes {
							_ = os.Remove(tmpOpus)
							if errStat != nil {
								return "", false, errStat
							}
							return "", false, fmt.Errorf("decoded direct stream from `%s` exceeded maximum size of %dMB", directURL, cache.GetMaxDownloadSizeMB())
						}
						savedPath, owned, errCommit := commitTrackToCache(cache, t, tmpOpus, key)
						if errCommit != nil {
							_ = os.Remove(tmpOpus)
							return "", false, errCommit
						}
						if owned {
							return savedPath, true, nil
						}
						saved = savedPath
					}
				}
			} else {
				lastHtmlErr = fmt.Errorf("HTTP error %d fetching audio stream from `%s`", resp.StatusCode, directURL)
				_ = resp.Body.Close()
			}
		}

		if saved == "" && attempt < directAttempts {
			timer := time.NewTimer(time.Duration(attempt) * 1500 * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				return "", false, ctx.Err()
			case <-timer.C:
			}
		}
	}

	if saved != "" {
		return saved, false, nil
	}
	return "", false, lastHtmlErr
}

func downloadRemoteWithYtdlp(ctx context.Context, cache *LRUSongCache, t *Track, targetURL, jobDir, key, userAgent string) (string, bool, error) {
	if strings.Contains(strings.ToLower(targetURL), "juicewrldapi.com") {
		return "", false, fmt.Errorf("cannot download juicewrldapi track without direct stream URL (got: `%s`)", targetURL)
	}

	if !helpers.IsURLSafe(targetURL) {
		return "", false, fmt.Errorf("target URL `%s` resolved to an unsafe or private destination", targetURL)
	}

	if ctx.Err() != nil {
		return "", false, ctx.Err()
	}

	ytdlpBin := helpers.GetYTDLPPath()
	tmpPattern := filepath.Join(jobDir, "%(id)s.%(ext)s")
	maxSizeMB := cache.GetMaxDownloadSizeMB()
	args := []string{
		"--ignore-config",
		"--no-call-home",
		"--socket-timeout", "15",
		"--abort-on-unavailable-fragment",
		"--no-playlist",
		"--format", "bestaudio/best",
		"-x",
		"--audio-format", "opus",
		"--audio-quality", "0",
		"--user-agent", userAgent,
		"--max-filesize", fmt.Sprintf("%dM", maxSizeMB),
		"--retries", "5",
	}

	if strings.Contains(strings.ToLower(t.URL), "youtube.com") || strings.Contains(strings.ToLower(t.URL), "youtu.be") {
		args = append(args, "--extractor-args", "youtube:player_client=android,web")
	}
	radioPathMu.RLock()
	cookiesPath := configuredCookies
	radioPathMu.RUnlock()
	if cookiesPath != "" {
		if _, errStat := os.Stat(cookiesPath); errStat == nil {
			args = append(args, "--cookies", cookiesPath)
		}
	}

	args = append(args, "-o", tmpPattern, "--", targetURL)

	ytdlpCtx, cancelYtdlp := context.WithTimeout(ctx, 4*time.Minute)
	defer cancelYtdlp()

	cmd := exec.CommandContext(ytdlpCtx, ytdlpBin, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if errRun := cmd.Run(); errRun != nil {
		return "", false, fmt.Errorf("download failed for `%s`: %w (details: %s)", t.Title, errRun, sanitizeStderr(stderr.String(), 1500))
	}

	if errCtx := ctx.Err(); errCtx != nil {
		return "", false, errCtx
	}

	globPattern := filepath.Join(jobDir, "*.opus")
	matches, _ := filepath.Glob(globPattern)
	if len(matches) == 0 {
		return "", false, fmt.Errorf("no opus file found after download")
	}

	downloadedFile := matches[0]
	return commitTrackToCache(cache, t, downloadedFile, key)
}

func CleanupStaleRadioBuffers() {
	bufDir := RadioBufferDir()
	clean := filepath.Clean(bufDir)
	if clean == "" || clean == "." || clean == "/" || clean == filepath.Clean(os.TempDir()) {
		return
	}
	if filepath.Base(clean) != "radio_buffer" {
		return
	}
	_ = os.RemoveAll(bufDir)
	_ = os.MkdirAll(bufDir, 0755)
}

func RadioBufferDir() string {
	radioPathMu.RLock()
	tempDir := configuredTempDir
	radioPathMu.RUnlock()
	if tempDir == "" {
		tempDir = filepath.Join("data", "temp")
	}
	bufDir := filepath.Join(tempDir, "radio_buffer")
	_ = os.MkdirAll(bufDir, 0755)
	return filepath.Clean(bufDir)
}

func normalizeMode(mode string) string {
	if canonical, ok := NormalizeRadioMode(mode); ok {
		clean := strings.ToLower(canonical)
		clean = strings.ReplaceAll(clean, "&", "")
		clean = strings.ReplaceAll(clean, "-", "")
		return clean
	}
	clean := strings.ToLower(strings.TrimSpace(mode))
	clean = strings.ReplaceAll(clean, "&", "")
	clean = strings.ReplaceAll(clean, "-", "")
	clean = strings.ReplaceAll(clean, " ", "")
	clean = strings.ReplaceAll(clean, "_", "")
	return clean
}

func IsRadioEligible(t *Track) bool {
	if t == nil {
		return false
	}
	cat := strings.ToLower(strings.TrimSpace(t.Category))
	if cat == "stream" || cat == "uploaded" || cat == "upload" || cat == "custom" {
		return false
	}
	uploader := strings.ToLower(strings.TrimSpace(t.Uploader))
	if uploader == "uploaded file" || uploader == "independent artist" {
		return false
	}
	if strings.Contains(uploader, "juice wrld") {
		return true
	}
	switch cat {
	case "unreleased", "released", "recording_session", "session", "sessions", "studio_acapella", "stem":
		return true
	}
	if strings.Contains(strings.ToLower(t.URL), "juicewrld") || strings.Contains(strings.ToLower(t.WebpageURL), "juicewrld") {
		return true
	}
	if strings.TrimSpace(t.Era) != "" {
		return true
	}
	return false
}

func MatchesMode(t *Track, mode string) bool {
	if t == nil || !IsRadioEligible(t) {
		return false
	}
	modeClean := strings.ToLower(strings.TrimSpace(mode))
	if modeClean == "" || modeClean == "all" || modeClean == "any" || modeClean == "auto" {
		return true
	}
	if strings.EqualFold(t.Era, modeClean) || strings.EqualFold(t.Category, modeClean) {
		return true
	}
	normMode := normalizeMode(modeClean)
	if t.Era != "" && normalizeMode(t.Era) == normMode {
		return true
	}
	if t.Category != "" && normalizeMode(t.Category) == normMode {
		return true
	}
	return false
}

func (c *LRUSongCache) PrepareTrack(ctx context.Context, track *Track) (string, bool, error) {
	return EnsureTrackCachedWithCache(ctx, c, track)
}
