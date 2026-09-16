package radio

import (
	"encoding/json"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	DefaultRecentPlayCap = 50
	DefaultRecentMaxAge  = 48 * time.Hour
)

type recentSnapshot struct {
	SavedAt time.Time           `json:"saved_at"`
	Guilds  map[string][]string `json:"guilds"`
}

type RecentPlaysTracker struct {
	mu         sync.Mutex
	persistMu  sync.Mutex
	recent     map[string][]string
	recentPath string
	maxCap     int
	maxAge     time.Duration
	now        func() time.Time
}

func (t *RecentPlaysTracker) currentTime() time.Time {
	if t.now != nil {
		return t.now()
	}
	return time.Now()
}

func NewRecentPlaysTracker(maxCap int, maxAge time.Duration) *RecentPlaysTracker {
	return NewRecentPlaysTrackerWithClock(maxCap, maxAge, time.Now)
}

func NewRecentPlaysTrackerWithClock(maxCap int, maxAge time.Duration, now func() time.Time) *RecentPlaysTracker {
	if maxCap <= 0 {
		maxCap = DefaultRecentPlayCap
	}
	if maxAge <= 0 {
		maxAge = DefaultRecentMaxAge
	}
	if now == nil {
		now = time.Now
	}
	return &RecentPlaysTracker{
		recent: make(map[string][]string),
		maxCap: maxCap,
		maxAge: maxAge,
		now:    now,
	}
}

func (t *RecentPlaysTracker) Add(guildID string, title string) {
	cleanTitle := strings.TrimSpace(title)
	if guildID == "" || cleanTitle == "" {
		return
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	list := t.recent[guildID]
	deduplicated := make([]string, 0, len(list)+1)
	for _, existing := range list {
		if !strings.EqualFold(existing, cleanTitle) {
			deduplicated = append(deduplicated, existing)
		}
	}
	list = append(deduplicated, cleanTitle)
	if len(list) > t.maxCap {
		list = list[len(list)-t.maxCap:]
	}
	t.recent[guildID] = list
}

func (t *RecentPlaysTracker) Recent(guildID string) []string {
	t.mu.Lock()
	defer t.mu.Unlock()

	list := t.recent[guildID]
	if len(list) == 0 {
		return nil
	}
	out := make([]string, len(list))
	copy(out, list)
	return out
}

func (t *RecentPlaysTracker) LoadFromFile(path string) error {
	if path == "" {
		return nil
	}

	t.persistMu.Lock()
	defer t.persistMu.Unlock()

	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	var snap recentSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return err
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	t.recentPath = path
	loaded := make(map[string][]string)
	if snap.SavedAt.IsZero() || t.currentTime().Sub(snap.SavedAt) <= t.maxAge {
		for gid, titles := range snap.Guilds {
			if len(titles) > t.maxCap {
				titles = titles[len(titles)-t.maxCap:]
			}
			loaded[gid] = append([]string(nil), titles...)
		}
	}
	t.recent = loaded

	return nil
}

func (t *RecentPlaysTracker) SaveToFile(path string) error {
	t.persistMu.Lock()
	defer t.persistMu.Unlock()
	if path == "" {
		t.mu.Lock()
		path = t.recentPath
		t.mu.Unlock()
	}
	if path == "" {
		return nil
	}

	t.mu.Lock()
	t.recentPath = path
	snap := recentSnapshot{
		SavedAt: t.currentTime().UTC(),
		Guilds:  make(map[string][]string, len(t.recent)),
	}
	for gid, titles := range t.recent {
		snap.Guilds[gid] = append([]string(nil), titles...)
	}
	t.mu.Unlock()

	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return err
	}

	return atomicWriteFile(path, data, 0644)
}
