package radio

import (
	"fmt"
	"math/rand"
	"net/url"
	"strings"
)

const (
	historyLimit    = 30
	MaxQueuedTracks = 100
)

type QueueLimitError struct {
	Limit     int
	Pending   int
	Requested int
}

func (e *QueueLimitError) Error() string {
	return fmt.Sprintf("queue limit exceeded: at most %d pending tracks are allowed", e.Limit)
}

type DuplicateMatch struct {
	Found         bool
	Current       bool
	QueuePosition int
}

func (m *Module) Duplicate(guildID string, candidate Track) DuplicateMatch {
	if m == nil {
		return DuplicateMatch{}
	}
	s := m.existingSession(guildID)
	if s == nil {
		return DuplicateMatch{}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.current != nil && duplicateTracks(candidate, *s.current) {
		return DuplicateMatch{Found: true, Current: true}
	}
	for i := range s.queue {
		if s.queue[i].IsAutoplay {
			continue
		}
		if duplicateTracks(candidate, s.queue[i]) {
			return DuplicateMatch{Found: true, QueuePosition: i + 1}
		}
	}
	return DuplicateMatch{}
}

func duplicateTracks(candidate, queued Track) bool {
	candidateURL := canonicalTrackURL(candidate)
	queuedURL := canonicalTrackURL(queued)
	if candidateURL != "" && queuedURL != "" {
		return candidateURL == queuedURL
	}
	return normalizeTrackMatch(candidate.Title) == normalizeTrackMatch(queued.Title) &&
		normalizeTrackMatch(candidate.Uploader) == normalizeTrackMatch(queued.Uploader)
}

func canonicalTrackURL(track Track) string {
	raw := strings.TrimSpace(track.WebpageURL)
	if raw == "" {
		raw = strings.TrimSpace(track.URL)
	}
	if raw == "" {
		return ""
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Host = strings.ToLower(parsed.Host)
	return parsed.String()
}

func normalizeTrackMatch(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(value), " "))
}

func (s *session) enqueueLocked(tracks []Track, placement EnqueuePlacement) {
	clones := cloneTracks(tracks)
	if len(clones) == 0 {
		return
	}

	filteredIncoming := make([]Track, 0, len(clones))
	for _, inc := range clones {
		promoted := false
		for i := range s.queue {
			if s.queue[i].IsAutoplay && duplicateTracks(inc, s.queue[i]) {
				s.queue[i].IsAutoplay = false
				promoted = true
				break
			}
		}
		if !promoted {
			filteredIncoming = append(filteredIncoming, inc)
		}
	}

	if len(filteredIncoming) > 0 {
		if placement == EnqueueNext {
			s.queue = append(filteredIncoming, s.queue...)
		} else {
			firstAutoIdx := -1
			for i, t := range s.queue {
				if t.IsAutoplay {
					firstAutoIdx = i
					break
				}
			}
			if firstAutoIdx >= 0 {
				prefix := append([]Track(nil), s.queue[:firstAutoIdx]...)
				suffix := s.queue[firstAutoIdx:]
				s.queue = append(prefix, append(filteredIncoming, suffix...)...)
			} else {
				s.queue = append(s.queue, filteredIncoming...)
			}
		}
	}
	s.nextQueueRevisionLocked()
}

func (s *session) clearQueueLocked() {
	s.queue = s.queue[:0]
	s.nextQueueRevisionLocked()
}

func (s *session) shuffleLocked() {
	var userTracks []Track
	var autoTracks []Track
	for _, t := range s.queue {
		if t.IsAutoplay {
			autoTracks = append(autoTracks, t)
		} else {
			userTracks = append(userTracks, t)
		}
	}
	if len(userTracks) > 1 {
		shuffleTracks(userTracks)
		s.queue = append(userTracks, autoTracks...)
	} else if len(autoTracks) == 0 {
		shuffleTracks(s.queue)
	}
	s.nextQueueRevisionLocked()
}

func shuffleTracks(tracks []Track) {
	rand.Shuffle(len(tracks), func(i, j int) { tracks[i], tracks[j] = tracks[j], tracks[i] })
}

func (s *session) addHistoryLocked(track Track) {
	s.history = append(s.history, track.Clone())
	if len(s.history) > historyLimit {
		copy(s.history, s.history[len(s.history)-historyLimit:])
		s.history = s.history[:historyLimit]
	}
}

func (s *session) selectNextQueuedLocked() bool {
	if len(s.queue) == 0 {
		return false
	}
	next := s.queue[0].Clone()
	s.queue = s.queue[1:]
	s.current = &next
	s.nextQueueRevisionLocked()
	return true
}

func (s *session) finishCurrentLocked(replay bool) {
	if s.current == nil {
		return
	}
	finished := s.current.Clone()
	s.current = nil
	if replay && s.loopMode == LoopTrack {
		s.queue = append([]Track{finished}, s.queue...)
		s.nextQueueRevisionLocked()
		return
	}
	s.addHistoryLocked(finished)
	if replay && s.loopMode == LoopQueue {
		s.queue = append(s.queue, finished)
		s.nextQueueRevisionLocked()
	}
}

func (s *session) skipCurrentLocked() {
	if s.current != nil {
		s.addHistoryLocked(*s.current)
		s.current = nil
	}
	if s.loopMode == LoopTrack {
		s.loopMode = LoopOff
	}
	s.nextQueueRevisionLocked()
	s.nextPlaybackGenerationLocked()
}

func (s *session) selectPreviousLocked() bool {
	if len(s.history) == 0 {
		return false
	}
	previous := s.history[len(s.history)-1].Clone()
	s.history = s.history[:len(s.history)-1]
	s.current = nil
	s.queue = append([]Track{previous}, s.queue...)
	if s.loopMode == LoopTrack {
		s.loopMode = LoopOff
	}
	s.nextQueueRevisionLocked()
	s.nextPlaybackGenerationLocked()
	return true
}
