package juicewrld

import (
	"encoding/json"
	"strings"
)

type Era struct {
	ID          int    `json:"id,omitempty"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	TimeFrame   string `json:"time_frame,omitempty"`
	PlayCount   int    `json:"play_count,omitempty"`
}

// UnmarshalJSON parses an Era from either a JSON object or a string.
func (e *Era) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		return nil
	}

	type eraAlias Era
	var obj eraAlias
	if err := json.Unmarshal(data, &obj); err == nil && (obj.Name != "" || obj.ID != 0) {
		*e = Era(obj)
		return nil
	}

	var s string
	if err := json.Unmarshal(data, &s); err == nil && s != "" {
		e.Name = s
		return nil
	}
	return nil
}

type FlexStringSlice []string

// UnmarshalJSON parses a slice from either a JSON array or a newline-separated string.
func (f *FlexStringSlice) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		return nil
	}

	var arr []string
	if err := json.Unmarshal(data, &arr); err == nil {
		var cleaned []string
		for _, s := range arr {
			if t := strings.TrimSpace(s); t != "" {
				cleaned = append(cleaned, t)
			}
		}
		*f = cleaned
		return nil
	}

	var s string
	if err := json.Unmarshal(data, &s); err == nil && strings.TrimSpace(s) != "" {
		for _, line := range strings.Split(s, "\n") {
			if t := strings.TrimSpace(line); t != "" {
				*f = append(*f, t)
			}
		}
		return nil
	}
	return nil
}

type Song struct {
	ID                    int             `json:"id"`
	PublicID              int             `json:"public_id"`
	Name                  string          `json:"name"`
	Title                 string          `json:"title"`
	OriginalKey           string          `json:"original_key"`
	Category              string          `json:"category"`
	Era                   *Era            `json:"era,omitempty"`
	Path                  string          `json:"path"`
	TrackTitles           FlexStringSlice `json:"track_titles"`
	CreditedArtists       string          `json:"credited_artists"`
	Producers             string          `json:"producers"`
	Engineers             string          `json:"engineers"`
	RecordingLocations    string          `json:"recording_locations"`
	RecordDates           string          `json:"record_dates"`
	Length                string          `json:"length"`
	Bitrate               string          `json:"bitrate"`
	FileNames             string          `json:"file_names"`
	Instrumentals         string          `json:"instrumentals"`
	InstrumentalNames     string          `json:"instrumental_names"`
	SessionTitles         string          `json:"session_titles"`
	SessionTracking       string          `json:"session_tracking"`
	PreviewDate           string          `json:"preview_date"`
	ReleaseDate           string          `json:"release_date"`
	Dates                 string          `json:"dates"`
	Notes                 string          `json:"notes"`
	Lyrics                string          `json:"lyrics"`
	Snippets              json.RawMessage `json:"snippets"`
	DateLeaked            string          `json:"date_leaked"`
	LeakType              string          `json:"leak_type"`
	ImageURL              string          `json:"image_url"`
	AltNames              FlexStringSlice `json:"alt_names"`
	AdditionalInformation string          `json:"additional_information"`
}

func (s *Song) GetName() string {
	if n := strings.TrimSpace(s.Name); n != "" {
		return n
	}
	return strings.TrimSpace(s.Title)
}

func (s *Song) SnippetCount() int {
	if len(s.Snippets) == 0 {
		return 0
	}
	var arr []json.RawMessage
	if err := json.Unmarshal(s.Snippets, &arr); err != nil {
		return 0
	}
	return len(arr)
}

type SongsResponse struct {
	Count    int    `json:"count"`
	Next     string `json:"next"`
	Previous string `json:"previous"`
	Results  []Song `json:"results"`
}

type BrowseResponse struct {
	Items []BrowseItem `json:"items"`
}

type BrowseItem struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Type string `json:"type"`
}

type RadioSongResponse struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Path     string `json:"path"`
	Size     int64  `json:"size"`
	Modified string `json:"modified"`
	Hash     string `json:"hash"`
	Song     *Song  `json:"song,omitempty"`
}

type AvailableFiles struct {
	HasOriginalFiles bool
	HasLRWav         bool
	HasWav           bool
	HasMp3           bool
}

type SessionEditItem struct {
	Name string
	Path string
	URL  string
	Ext  string
}
