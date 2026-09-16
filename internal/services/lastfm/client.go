package lastfm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"gobot/internal/helpers"
)

const (
	BaseURL                      = "https://ws.audioscrobbler.com/2.0/"
	LastFmDefaultPlaceholderHash = "2a96cbd8b46e442fc41c2b86b821562f"
	maxBodyBytes                 = 2 * 1024 * 1024
)

var (
	ErrUserNotFound      = errors.New("user not found")
	ErrRateLimited       = errors.New("rate limit exceeded")
	ErrAuthFailed        = errors.New("authentication failed")
	ErrServiceDown       = errors.New("last.fm service unavailable")
	ErrInvalidParameters = errors.New("invalid request parameters")
)

type ErrorKind string

const (
	ErrKindNotFound      ErrorKind = "NOT_FOUND"
	ErrKindRateLimited   ErrorKind = "RATE_LIMITED"
	ErrKindAuthFailed    ErrorKind = "AUTH_FAILED"
	ErrKindServiceDown   ErrorKind = "SERVICE_UNAVAILABLE"
	ErrKindInvalidParams ErrorKind = "INVALID_REQUEST"
	ErrKindUnknown       ErrorKind = "UNKNOWN"
)

type Logger interface {
	Warnf(format string, args ...any)
	Errorf(format string, args ...any)
}

type noopLogger struct{}

func (n noopLogger) Warnf(string, ...any)  {}
func (n noopLogger) Errorf(string, ...any) {}

type Client struct {
	APIKey      string
	HTTPClient  *http.Client
	logger      Logger
	limiterMu   sync.Mutex
	nextReq     time.Time
	minInterval time.Duration
}

type ClientOption func(*Client)

func NewClient(apiKey string, opts ...ClientOption) *Client {
	c := &Client{
		APIKey:      apiKey,
		HTTPClient:  helpers.NewSafeHTTPClient(helpers.DurationTimeoutHTTP),
		logger:      noopLogger{},
		minInterval: 200 * time.Millisecond,
	}

	for _, opt := range opts {
		opt(c)
	}

	if c.APIKey == "" {
		c.logger.Warnf("LASTFM_API_KEY is empty")
	}

	return c
}

func (c *Client) waitRateLimit(ctx context.Context) error {
	c.limiterMu.Lock()
	now := time.Now()
	if c.nextReq.Before(now) {
		c.nextReq = now
	}
	wait := c.nextReq.Sub(now)
	c.nextReq = c.nextReq.Add(c.minInterval)
	c.limiterMu.Unlock()

	if wait <= 0 {
		return nil
	}

	timer := time.NewTimer(wait)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (c *Client) Fetch(ctx context.Context, params map[string]string, target any) error {
	if c.APIKey == "" {
		return &APIError{
			Code:      -1,
			Kind:      ErrKindAuthFailed,
			UserLabel: "Last.fm API key is not configured.",
		}
	}

	if err := c.waitRateLimit(ctx); err != nil {
		return fmt.Errorf("rate limiter canceled: %w", err)
	}

	v := url.Values{}
	v.Set("api_key", c.APIKey)
	v.Set("format", "json")
	for k, val := range params {
		v.Set(k, val)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, BaseURL+"?"+v.Encode(), nil)
	if err != nil {
		return fmt.Errorf("failed to create http request: %w", sanitizeURLError(err))
	}

	req.Header.Set("User-Agent", helpers.GetUserAgent())
	req.Header.Set("Accept", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		return &APIError{
			Code:       -2,
			Kind:       ErrKindServiceDown,
			RawMessage: sanitizeURLError(err).Error(),
			UserLabel:  "Cannot connect to Last.fm servers.",
		}
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}()

	limitedBody := io.LimitReader(resp.Body, maxBodyBytes)

	if resp.StatusCode != http.StatusOK {
		var rawErr struct {
			Error   int    `json:"error"`
			Message string `json:"message"`
		}
		if decodeErr := json.NewDecoder(limitedBody).Decode(&rawErr); decodeErr == nil && rawErr.Error != 0 {
			return InterpretAPIError(rawErr.Error, rawErr.Message)
		}
		return &APIError{
			Code:      resp.StatusCode,
			Kind:      ErrKindServiceDown,
			UserLabel: fmt.Sprintf("Last.fm returned HTTP status %d.", resp.StatusCode),
		}
	}

	if err := json.NewDecoder(limitedBody).Decode(target); err != nil {
		return fmt.Errorf("failed to decode json response: %w", err)
	}

	return nil
}

func (c *Client) FetchiTunesAlbumCover(ctx context.Context, artist, album string) (string, error) {
	term := strings.TrimSpace(artist + " " + album)
	if term == "" {
		return "", nil
	}

	u, err := url.Parse("https://itunes.apple.com/search")
	if err != nil {
		return "", err
	}
	q := u.Query()
	q.Set("entity", "album")
	q.Set("term", term)
	q.Set("limit", "1")
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", helpers.GetUserAgent())

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("itunes api returned status %d", resp.StatusCode)
	}

	var result struct {
		Results []struct {
			ArtworkURL100 string `json:"artworkUrl100"`
		} `json:"results"`
	}

	limitedBody := io.LimitReader(resp.Body, 512*1024)
	if err := json.NewDecoder(limitedBody).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to decode itunes response: %w", err)
	}

	if len(result.Results) > 0 {
		return strings.ReplaceAll(result.Results[0].ArtworkURL100, "100x100bb", "600x600bb"), nil
	}

	return "", nil
}

func (c *Client) GetUserInfo(ctx context.Context, username string) (*UserInfo, error) {
	params := map[string]string{
		"method": "user.getinfo",
		"user":   username,
	}
	var res UserGetInfoResponse
	if err := c.Fetch(ctx, params, &res); err != nil {
		return nil, err
	}
	if res.User.Name == "" {
		return nil, ErrUserNotFound
	}
	res.User.init()
	return &res.User, nil
}

func (c *Client) GetRecentTracks(ctx context.Context, username string, limit int) (*RecentTracksResponse, error) {
	if limit <= 0 {
		limit = 10
	}
	params := map[string]string{
		"method": "user.getrecenttracks",
		"user":   username,
		"limit":  strconv.Itoa(limit),
	}
	var res RecentTracksResponse
	if err := c.Fetch(ctx, params, &res); err != nil {
		return nil, err
	}
	for i := range res.RecentTracks.Track {
		res.RecentTracks.Track[i].init()
	}
	return &res, nil
}

func (c *Client) GetTopAlbums(ctx context.Context, username, period string, limit int) (*TopAlbumsResponse, error) {
	if period == "" {
		period = "overall"
	}
	if limit <= 0 {
		limit = 10
	}
	params := map[string]string{
		"method": "user.gettopalbums",
		"user":   username,
		"period": period,
		"limit":  strconv.Itoa(limit),
	}
	var res TopAlbumsResponse
	if err := c.Fetch(ctx, params, &res); err != nil {
		return nil, err
	}
	for i := range res.TopAlbums.Album {
		res.TopAlbums.Album[i].init()
	}
	return &res, nil
}

func (c *Client) GetTopArtists(ctx context.Context, username, period string, limit int) (*TopArtistsResponse, error) {
	if period == "" {
		period = "overall"
	}
	if limit <= 0 {
		limit = 10
	}
	params := map[string]string{
		"method": "user.gettopartists",
		"user":   username,
		"period": period,
		"limit":  strconv.Itoa(limit),
	}
	var res TopArtistsResponse
	if err := c.Fetch(ctx, params, &res); err != nil {
		return nil, err
	}
	for i := range res.TopArtists.Artist {
		res.TopArtists.Artist[i].init()
	}
	return &res, nil
}

func (c *Client) GetTopTracks(ctx context.Context, username, period string, limit int) (*TopTracksResponse, error) {
	if period == "" {
		period = "overall"
	}
	if limit <= 0 {
		limit = 10
	}
	params := map[string]string{
		"method": "user.gettoptracks",
		"user":   username,
		"period": period,
		"limit":  strconv.Itoa(limit),
	}
	var res TopTracksResponse
	if err := c.Fetch(ctx, params, &res); err != nil {
		return nil, err
	}
	for i := range res.TopTracks.Track {
		res.TopTracks.Track[i].init()
	}
	return &res, nil
}

func (c *Client) GetArtistInfo(ctx context.Context, artist, username string) (*ArtistInfo, error) {
	params := map[string]string{
		"method": "artist.getinfo",
		"artist": artist,
	}
	if username != "" {
		params["username"] = username
	}
	var res ArtistInfoResponse
	if err := c.Fetch(ctx, params, &res); err != nil {
		return nil, err
	}
	res.Artist.init()
	return &res.Artist, nil
}

type APIError struct {
	Code       int       `json:"error"`
	RawMessage string    `json:"message"`
	Kind       ErrorKind `json:"-"`
	UserLabel  string    `json:"-"`
}

func (e *APIError) Error() string {
	if e.RawMessage != "" {
		return fmt.Sprintf("last.fm api error (%d: %s): %s", e.Code, e.Kind, e.RawMessage)
	}
	return fmt.Sprintf("last.fm api error (%d: %s): %s", e.Code, e.Kind, e.UserLabel)
}

func (e *APIError) Is(target error) bool {
	switch target {
	case ErrUserNotFound:
		return e.Kind == ErrKindNotFound || e.Code == 6
	case ErrRateLimited:
		return e.Kind == ErrKindRateLimited || e.Code == 29
	case ErrAuthFailed:
		return e.Kind == ErrKindAuthFailed || e.Code == 4 || e.Code == 9 || e.Code == 10 || e.Code == 26
	case ErrServiceDown:
		return e.Kind == ErrKindServiceDown || e.Code == 8 || e.Code == 11 || e.Code == 16
	case ErrInvalidParameters:
		return e.Kind == ErrKindInvalidParams
	default:
		return false
	}
}

func InterpretAPIError(code int, rawMsg string) *APIError {
	var kind ErrorKind
	var label string

	switch code {
	case 6:
		kind = ErrKindNotFound
		label = "The requested Last.fm user, artist, or track was not found."
	case 4, 9, 14:
		kind = ErrKindAuthFailed
		label = "Last.fm authentication failed. Check your account connection."
	case 10, 26:
		kind = ErrKindAuthFailed
		label = "Last.fm API key is invalid or suspended."
	case 29:
		kind = ErrKindRateLimited
		label = "Last.fm is busy. Try again in a few moments."
	case 8, 11, 16:
		kind = ErrKindServiceDown
		label = "Last.fm servers are temporarily offline."
	case 2, 3, 7, 13:
		kind = ErrKindInvalidParams
		label = "Invalid request format sent to Last.fm."
	default:
		kind = ErrKindUnknown
		label = "An unexpected Last.fm error occurred."
	}

	return &APIError{
		Code:       code,
		RawMessage: rawMsg,
		Kind:       kind,
		UserLabel:  label,
	}
}

func sanitizeURLError(err error) error {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		redactedURL := *urlErr
		if parsed, parseErr := url.Parse(redactedURL.URL); parseErr == nil {
			q := parsed.Query()
			if q.Has("api_key") {
				q.Set("api_key", "[REDACTED]")
				parsed.RawQuery = q.Encode()
			}
			redactedURL.URL = parsed.String()
		}
		return fmt.Errorf("%s %s: %w", redactedURL.Op, redactedURL.URL, redactedURL.Err)
	}
	return err
}

var mdLinkReplacer = strings.NewReplacer("[", "\\[", "]", "\\]")

func EscapeMDLink(text string) string {
	return mdLinkReplacer.Replace(text)
}

func MakeLink(text, linkURL string) string {
	label := EscapeMDLink(text)
	if label == "" {
		label = "Unknown"
	}
	if linkURL == "" {
		return label
	}
	sanitizedURL := strings.ReplaceAll(linkURL, ")", "%29")
	return fmt.Sprintf("[%s](%s)", label, sanitizedURL)
}

func ArtistURL(artist string) string {
	if artist == "" || artist == "Unknown Artist" {
		return ""
	}
	return fmt.Sprintf("https://www.last.fm/music/%s", url.PathEscape(artist))
}

// FlexibleString unmarshals JSON numbers and strings into a string.
type FlexibleString string

func (f *FlexibleString) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		*f = ""
		return nil
	}
	if data[0] == '"' {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		*f = FlexibleString(s)
		return nil
	}
	*f = FlexibleString(string(data))
	return nil
}

func (f FlexibleString) String() string {
	return string(f)
}

// FlexibleSlice unmarshals JSON arrays, single objects, and empty objects into a slice.
type FlexibleSlice[T any] []T

func (f *FlexibleSlice[T]) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) || bytes.Equal(data, []byte(`""`)) || bytes.Equal(data, []byte("{}")) {
		*f = nil
		return nil
	}
	if data[0] == '[' {
		var slice []T
		if err := json.Unmarshal(data, &slice); err != nil {
			return err
		}
		*f = slice
		return nil
	}
	var single T
	if err := json.Unmarshal(data, &single); err != nil {
		return err
	}
	*f = []T{single}
	return nil
}

type TrackImage struct {
	Text string `json:"#text"`
	Size string `json:"size"`
}

func extractBestImage(images []TrackImage) string {
	for i := len(images) - 1; i >= 0; i-- {
		img := strings.TrimSpace(images[i].Text)
		if img != "" && !strings.Contains(img, LastFmDefaultPlaceholderHash) {
			return img
		}
	}
	return ""
}

type UserInfo struct {
	Name        string         `json:"name"`
	Playcount   FlexibleString `json:"playcount"`
	ArtistCount FlexibleString `json:"artist_count"`
	TrackCount  FlexibleString `json:"track_count"`
	AlbumCount  FlexibleString `json:"album_count"`
	URL         string         `json:"url"`
	Registered  struct {
		Unixtime FlexibleString `json:"unixtime"`
		Text     FlexibleString `json:"#text"`
	} `json:"registered"`
	Image []TrackImage `json:"image"`

	DisplayName      string `json:"-"`
	AvatarURL        string `json:"-"`
	DisplayPlaycount string `json:"-"`
	RegisteredDate   string `json:"-"`
}

func (u *UserInfo) init() {
	if u.Name == "" {
		u.DisplayName = "Unknown User"
	} else {
		u.DisplayName = u.Name
	}
	u.AvatarURL = extractBestImage(u.Image)
	if u.Playcount == "" {
		u.DisplayPlaycount = "0 scrobbles"
	} else {
		u.DisplayPlaycount = fmt.Sprintf("%s scrobbles", u.Playcount)
	}
	if sec, err := strconv.ParseInt(string(u.Registered.Unixtime), 10, 64); err == nil && sec > 0 {
		u.RegisteredDate = time.Unix(sec, 0).UTC().Format("Jan 02, 2006")
	} else {
		u.RegisteredDate = string(u.Registered.Text)
	}
}

type UserGetInfoResponse struct {
	User    UserInfo `json:"user"`
	Error   int      `json:"error"`
	Message string   `json:"message"`
}

type RecentTrack struct {
	Name   string `json:"name"`
	URL    string `json:"url"`
	Artist struct {
		Text string `json:"#text"`
	} `json:"artist"`
	Album struct {
		Text string `json:"#text"`
	} `json:"album"`
	Image []TrackImage `json:"image"`
	Attr  struct {
		NowPlaying string `json:"nowplaying"`
	} `json:"@attr"`

	DisplayTitle  string `json:"-"`
	DisplayArtist string `json:"-"`
	DisplayAlbum  string `json:"-"`
	StatusLabel   string `json:"-"`
	CoverURL      string `json:"-"`
	ArtistLink    string `json:"-"`
	TrackLink     string `json:"-"`
}

func (t *RecentTrack) init() {
	if t.Name == "" {
		t.DisplayTitle = "Unknown Track"
	} else {
		t.DisplayTitle = t.Name
	}
	if t.Artist.Text == "" {
		t.DisplayArtist = "Unknown Artist"
	} else {
		t.DisplayArtist = t.Artist.Text
	}
	if t.Album.Text == "" {
		t.DisplayAlbum = "Unknown Album"
	} else {
		t.DisplayAlbum = t.Album.Text
	}
	if t.Attr.NowPlaying == "true" {
		t.StatusLabel = "Listening Now"
	} else {
		t.StatusLabel = "Scrobbled"
	}
	t.CoverURL = extractBestImage(t.Image)
	t.ArtistLink = MakeLink(t.DisplayArtist, ArtistURL(t.DisplayArtist))
	t.TrackLink = MakeLink(t.DisplayTitle, t.URL)
}

type RecentTracksResponse struct {
	RecentTracks struct {
		Track FlexibleSlice[RecentTrack] `json:"track"`
		Attr  struct {
			User  string `json:"user"`
			Total string `json:"total"`
		} `json:"@attr"`
	} `json:"recenttracks"`
	Error   int    `json:"error"`
	Message string `json:"message"`
}

type TopAlbum struct {
	Name      string `json:"name"`
	Playcount string `json:"playcount"`
	URL       string `json:"url"`
	Artist    struct {
		Name string `json:"name"`
		URL  string `json:"url"`
	} `json:"artist"`
	Image []TrackImage `json:"image"`

	DisplayTitle   string `json:"-"`
	DisplayArtist  string `json:"-"`
	CoverURL       string `json:"-"`
	FormattedPlays string `json:"-"`
}

func (a *TopAlbum) init() {
	if a.Name == "" {
		a.DisplayTitle = "Unknown Album"
	} else {
		a.DisplayTitle = a.Name
	}
	if a.Artist.Name == "" {
		a.DisplayArtist = "Unknown Artist"
	} else {
		a.DisplayArtist = a.Artist.Name
	}
	a.CoverURL = extractBestImage(a.Image)
	if a.Playcount != "" {
		a.FormattedPlays = fmt.Sprintf("%s plays", a.Playcount)
	}
}

type TopAlbumsResponse struct {
	TopAlbums struct {
		Album FlexibleSlice[TopAlbum] `json:"album"`
	} `json:"topalbums"`
	Error   int    `json:"error"`
	Message string `json:"message"`
}

type TopArtist struct {
	Name      string `json:"name"`
	Playcount string `json:"playcount"`
	URL       string `json:"url"`

	DisplayName    string `json:"-"`
	FormattedPlays string `json:"-"`
	ArtistLink     string `json:"-"`
}

func (a *TopArtist) init() {
	if a.Name == "" {
		a.DisplayName = "Unknown Artist"
	} else {
		a.DisplayName = a.Name
	}
	if a.Playcount != "" {
		a.FormattedPlays = fmt.Sprintf("%s plays", a.Playcount)
	}
	a.ArtistLink = MakeLink(a.DisplayName, a.URL)
}

type TopArtistsResponse struct {
	TopArtists struct {
		Artist FlexibleSlice[TopArtist] `json:"artist"`
	} `json:"topartists"`
	Error   int    `json:"error"`
	Message string `json:"message"`
}

type TopTrack struct {
	Name      string `json:"name"`
	Playcount string `json:"playcount"`
	URL       string `json:"url"`
	Artist    struct {
		Name string `json:"name"`
		URL  string `json:"url"`
	} `json:"artist"`

	DisplayTitle   string `json:"-"`
	DisplayArtist  string `json:"-"`
	FormattedPlays string `json:"-"`
	TrackLink      string `json:"-"`
}

func (t *TopTrack) init() {
	if t.Name == "" {
		t.DisplayTitle = "Unknown Track"
	} else {
		t.DisplayTitle = t.Name
	}
	if t.Artist.Name == "" {
		t.DisplayArtist = "Unknown Artist"
	} else {
		t.DisplayArtist = t.Artist.Name
	}
	if t.Playcount != "" {
		t.FormattedPlays = fmt.Sprintf("%s plays", t.Playcount)
	}
	t.TrackLink = MakeLink(t.DisplayTitle, t.URL)
}

type TopTracksResponse struct {
	TopTracks struct {
		Track FlexibleSlice[TopTrack] `json:"track"`
	} `json:"toptracks"`
	Error   int    `json:"error"`
	Message string `json:"message"`
}

type ArtistInfo struct {
	Name  string `json:"name"`
	URL   string `json:"url"`
	Stats struct {
		Listeners     string `json:"listeners"`
		Playcount     string `json:"playcount"`
		UserPlayCount string `json:"userplaycount"`
	} `json:"stats"`
	Tags struct {
		Tag FlexibleSlice[struct {
			Name string `json:"name"`
		}] `json:"tag"`
	} `json:"tags"`
	Bio struct {
		Summary string `json:"summary"`
	} `json:"bio"`

	DisplayName string   `json:"-"`
	CleanTags   []string `json:"-"`
	CleanBio    string   `json:"-"`
}

func (a *ArtistInfo) init() {
	if a.Name == "" {
		a.DisplayName = "Unknown Artist"
	} else {
		a.DisplayName = a.Name
	}
	var tags []string
	for _, t := range a.Tags.Tag {
		if strings.TrimSpace(t.Name) != "" {
			tags = append(tags, strings.ToLower(t.Name))
		}
	}
	a.CleanTags = tags
	bio := a.Bio.Summary
	if idx := strings.Index(bio, "<a href="); idx != -1 {
		bio = strings.TrimSpace(bio[:idx])
	}
	a.CleanBio = bio
}

type ArtistInfoResponse struct {
	Artist  ArtistInfo `json:"artist"`
	Error   int        `json:"error"`
	Message string     `json:"message"`
}
