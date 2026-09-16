package radio

import (
	"context"
	"strings"
	"sync"
	"time"
)

type controlEffects struct {
	connection     VoiceConnection
	playback       PlaybackHandle
	cancelPlayback context.CancelFunc
	cancelLoading  context.CancelFunc
	stopPlayback   bool
	pausePlayback  bool
	resumePlayback bool
	startNext      bool
	disconnect     bool
}

// Module.mu may nest before session.mu when callers access the session map.
// The connect path uses voiceJoinMu to serialize joins without holding mu.
// Effect operations may nest effectMu before settingsMu or controllerMu.
// Acquire settingsMu before session.mu when loading or updating settings.
// Controller operations may hold controllerMu before session.mu.
// Cache eviction may call back from cache.mu into Module.mu for lease checks.
// Do not acquire cache.mu while holding Module.mu.
// Do not hold session.mu while calling cache lease checks. // llm written comment.
type session struct {
	mu           sync.Mutex
	settingsMu   sync.Mutex
	connectMu    sync.Mutex
	voiceJoinMu  sync.Mutex
	effectMu     sync.Mutex
	controllerMu sync.Mutex
	settings     settingsLoadState

	guildID                     string
	voiceChannelID              string
	voice                       VoiceConnection
	textChannelID               string
	phase                       Phase
	restrictionMode             string
	autojoinChannelID           string
	playerChannelID             string
	is247                       bool
	loopMode                    LoopMode
	mode                        string
	volume                      float64
	queue                       []Track
	history                     []Track
	current                     *Track
	playback                    PlaybackHandle
	playbackCancel              context.CancelFunc
	playbackPath                string
	playbackStarting            bool
	autoplayInFlight            bool
	autoplayToken               uint64
	restartingAfterStall        bool
	consecutiveStartupFailures  int
	playbackGeneration          uint64
	epochDone                   chan struct{}
	loadingSince                time.Time
	loadingEpoch                uint64
	loadingGeneration           uint64
	loadingRefreshCancel        context.CancelFunc
	queueRevision               uint64
	sessionEpoch                uint64
	trackStartTime              time.Time
	pausedDuration              time.Duration
	pauseStartTime              time.Time
	pauseCause                  PauseCause
	lastActivity                time.Time
	controllerChannelID         string
	controllerMessageID         string
	controllerCleanup           bool
	pendingControllerCleanup    map[controllerKey]ControllerRef
	controllerBackoffUntil      time.Time
	controllerBackoffChannel    string
	controllerBackoffFailures   int
	controllerSuppressedChannel string
	controllerView              ControllerViewType
	controllerQueuePage         int
	controllerViewActivity      time.Time
	controllerRevision          uint64
	prebuffer                   *PrebufferTask
	voiceTransition             bool
	externalMoveInFlight        bool
	externalMoveTarget          string
	stopping                    bool
}

func newSession(guildID string, now time.Time) *session {
	return &session{
		guildID:         guildID,
		phase:           PhaseIdle,
		restrictionMode: "vc",
		loopMode:        LoopOff,
		mode:            "all",
		volume:          1,
		queue:           make([]Track, 0),
		history:         make([]Track, 0),
		sessionEpoch:    1,
		epochDone:       make(chan struct{}),
		trackStartTime:  now,
		lastActivity:    now,
		prebuffer:       NewPrebufferTask(),
	}
}

func (s *session) snapshotLocked() Snapshot {
	var current *Track
	if s.current != nil {
		value := s.current.Clone()
		current = &value
	}
	return Snapshot{
		GuildID:             s.guildID,
		VoiceChannelID:      s.voiceChannelID,
		TextChannelID:       s.textChannelID,
		Phase:               s.phase,
		PauseCause:          s.pauseCause,
		RestrictionMode:     s.restrictionMode,
		AutojoinChannelID:   s.autojoinChannelID,
		PlayerChannelID:     s.playerChannelID,
		Is247:               s.is247,
		LoopMode:            s.loopMode,
		Mode:                s.mode,
		Volume:              s.volume,
		Queue:               cloneTracks(s.queue),
		History:             cloneTracks(s.history),
		Current:             current,
		PlaybackGeneration:  s.playbackGeneration,
		SessionEpoch:        s.sessionEpoch,
		LoadingSince:        s.loadingSince,
		TrackStartTime:      s.trackStartTime,
		PausedDuration:      s.pausedDuration,
		PauseStartTime:      s.pauseStartTime,
		ControllerChannelID: s.controllerChannelID,
		ControllerMessageID: s.controllerMessageID,
		ControllerCleanup:   s.controllerCleanup,
		PendingControllers:  s.pendingControllersLocked(),
		ControllerView:      s.controllerView,
		QueuePage:           s.controllerQueuePage,
		ControllerRevision:  s.controllerRevision,
	}
}

func (s *session) nextPlaybackGenerationLocked() uint64 {
	s.playbackGeneration++
	if s.loadingRefreshCancel != nil {
		s.loadingRefreshCancel()
		s.loadingRefreshCancel = nil
	}
	s.loadingSince = time.Time{}
	s.loadingEpoch = 0
	s.loadingGeneration = 0
	s.controllerView = ControllerViewPlayer
	s.controllerQueuePage = 1
	s.controllerRevision++
	return s.playbackGeneration
}

func (s *session) nextQueueRevisionLocked() uint64 {
	s.queueRevision++
	return s.queueRevision
}

func (s *session) nextSessionEpochLocked() uint64 {
	s.sessionEpoch++
	if s.epochDone != nil {
		close(s.epochDone)
	}
	s.epochDone = make(chan struct{})
	if s.loadingRefreshCancel != nil {
		s.loadingRefreshCancel()
		s.loadingRefreshCancel = nil
	}
	s.loadingSince = time.Time{}
	s.loadingEpoch = 0
	s.loadingGeneration = 0
	return s.sessionEpoch
}

func (s *session) invalidateLoadingRefreshLocked() {
	if s.loadingRefreshCancel != nil {
		s.loadingRefreshCancel()
		s.loadingRefreshCancel = nil
	}
	s.loadingSince = time.Time{}
	s.loadingEpoch = 0
	s.loadingGeneration = 0
}

func (s *session) applyControlLocked(action ControlAction, now time.Time) (controlEffects, error) {
	effects := controlEffects{}

	switch action {
	case ControlPause, ControlPauseForEmptyChannel:
		if s.current == nil || s.phase != PhasePlaying {
			return controlEffects{}, ErrInvalidPhase
		}
		s.phase = PhasePaused
		s.pauseStartTime = now
		if action == ControlPauseForEmptyChannel {
			s.pauseCause = PauseCauseEmptyChannel
		} else {
			s.pauseCause = PauseCauseManual
		}
		effects.playback = s.playback
		effects.pausePlayback = true

	case ControlResume, ControlResumeForHumanReturn:
		if s.current == nil || s.phase != PhasePaused {
			return controlEffects{}, ErrInvalidPhase
		}
		if action == ControlResumeForHumanReturn && s.pauseCause != PauseCauseEmptyChannel {
			return controlEffects{}, ErrInvalidPhase
		}
		if !s.pauseStartTime.IsZero() {
			s.pausedDuration += now.Sub(s.pauseStartTime)
			s.pauseStartTime = time.Time{}
		}
		s.pauseCause = PauseCauseNone
		s.phase = PhasePlaying
		effects.playback = s.playback
		effects.resumePlayback = true

	case ControlSkip:
		if s.current == nil && len(s.queue) == 0 && !s.playbackStarting {
			return controlEffects{}, ErrInvalidPhase
		}
		effects.playback = s.playback
		effects.cancelPlayback = s.playbackCancel
		effects.cancelLoading = s.loadingRefreshCancel
		effects.stopPlayback = effects.playback != nil
		s.playback = nil
		s.playbackCancel = nil
		s.invalidateLoadingRefreshLocked()
		s.skipCurrentLocked()
		effects.startNext = true
		s.phase = PhaseLoading

	case ControlPrevious:
		if !s.selectPreviousLocked() {
			return controlEffects{}, ErrInvalidPhase
		}
		effects.playback = s.playback
		effects.cancelPlayback = s.playbackCancel
		effects.cancelLoading = s.loadingRefreshCancel
		effects.stopPlayback = effects.playback != nil
		s.playback = nil
		s.playbackCancel = nil
		s.invalidateLoadingRefreshLocked()
		effects.startNext = true
		s.phase = PhaseLoading

	case ControlLoop:
		s.loopMode = nextLoopMode(s.loopMode)
		s.nextQueueRevisionLocked()
	case ControlLoopOff:
		if s.loopMode != LoopOff {
			s.loopMode = LoopOff
			s.nextQueueRevisionLocked()
		}
	case ControlLoopTrack:
		if s.loopMode != LoopTrack {
			s.loopMode = LoopTrack
			s.nextQueueRevisionLocked()
		}
	case ControlLoopQueue:
		if s.loopMode != LoopQueue {
			s.loopMode = LoopQueue
			s.nextQueueRevisionLocked()
		}

	case ControlShuffle:
		s.shuffleLocked()
	case ControlClearQueue:
		effects.cancelLoading = s.loadingRefreshCancel
		s.clearQueueLocked()
		s.invalidateLoadingRefreshLocked()

	case ControlStop, ControlLeave:
		effects.connection = s.voice
		effects.playback = s.playback
		effects.cancelPlayback = s.playbackCancel
		effects.stopPlayback = effects.playback != nil
		effects.disconnect = true
		s.playback = nil
		s.playbackCancel = nil
		s.beginStoppingLocked()
		s.finishStoppingLocked(now)

	default:
		return controlEffects{}, ErrInvalidControlAction
	}

	return effects, nil
}

func (s *session) beginStoppingLocked() {
	s.nextSessionEpochLocked()
	s.nextPlaybackGenerationLocked()
	s.phase = PhaseStopping
	s.stopping = true
	s.retainControllerLocked()
}

func (s *session) finishStoppingLocked(now time.Time) {
	s.voiceChannelID = ""
	s.voice = nil
	s.queue = s.queue[:0]
	s.history = s.history[:0]
	s.current = nil
	s.playback = nil
	s.playbackPath = ""
	s.playbackCancel = nil
	s.phase = PhaseIdle
	s.invalidateLoadingRefreshLocked()
	s.stopping = false
	s.trackStartTime = now
	s.lastActivity = now
	s.pausedDuration = 0
	s.pauseStartTime = time.Time{}
	s.pauseCause = PauseCauseNone
	s.is247 = false
	s.voiceTransition = false
	s.externalMoveInFlight = false
	s.externalMoveTarget = ""
	if !s.controllerCleanup {
		s.controllerChannelID = ""
		s.controllerMessageID = ""
	}
}

func (m *Module) startIdleSessionSweeper() {
	m.startWorker(func() {
		ticker := time.NewTicker(m.idleSweepInterval)
		defer ticker.Stop()
		for {
			select {
			case <-m.workerCtx.Done():
				return
			case <-ticker.C:
				m.evictIdleSessions()
			}
		}
	})
}

func (m *Module) evictIdleSessions() {
	cutoff := m.now().Add(-m.idleSessionAge)
	type idleSession struct {
		s         *session
		prebuffer *PrebufferTask
	}
	var evicted []idleSession

	m.mu.Lock()
	for guildID, s := range m.sessions {
		s.mu.Lock()
		prebuffer := s.prebuffer
		idle := !s.stopping && s.phase == PhaseIdle && s.voice == nil && s.current == nil &&
			len(s.queue) == 0 && s.playback == nil && !s.playbackStarting && !s.is247 &&
			s.controllerMessageID == "" && len(s.pendingControllerCleanup) == 0 &&
			s.lastActivity.Before(cutoff)
		s.mu.Unlock()
		if !idle || prebuffer != nil && !prebuffer.IsIdle() {
			continue
		}

		s.mu.Lock()
		stillIdle := !s.stopping && s.phase == PhaseIdle && s.voice == nil && s.current == nil &&
			len(s.queue) == 0 && s.playback == nil && !s.playbackStarting && !s.is247 &&
			s.controllerMessageID == "" && len(s.pendingControllerCleanup) == 0 &&
			s.prebuffer == prebuffer && s.lastActivity.Before(cutoff)
		s.mu.Unlock()
		if stillIdle {
			delete(m.sessions, guildID)
			evicted = append(evicted, idleSession{s: s, prebuffer: prebuffer})
		}
	}
	m.mu.Unlock()

	for _, candidate := range evicted {
		if candidate.prebuffer == nil {
			continue
		}
		_ = candidate.prebuffer.closeAndWait(m.workerCtx)
	}
}

func (m *Module) getSession(ctx context.Context, guildID string) (*session, error) {
	guildID = strings.TrimSpace(guildID)
	if guildID == "" {
		return nil, ErrInvalidGuildID
	}

	m.mu.Lock()
	if m.stopping {
		m.mu.Unlock()
		return nil, ErrShuttingDown
	}
	if current := m.sessions[guildID]; current != nil {
		m.mu.Unlock()
		m.loadSettings(ctx, current)
		return current, nil
	}
	current := newSession(guildID, m.now())
	if current.prebuffer != nil && m.cache != nil {
		current.prebuffer.SetSharedPathChecker(m.cache.OwnsPath)
	}
	m.sessions[guildID] = current
	m.mu.Unlock()
	m.loadSettings(ctx, current)
	return current, nil
}

func (m *Module) existingSession(guildID string) *session {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sessions[strings.TrimSpace(guildID)]
}

func (m *Module) Snapshot(guildID string) (Snapshot, bool) {
	guildID = strings.TrimSpace(guildID)
	if guildID == "" {
		return Snapshot{}, false
	}
	m.mu.Lock()
	current := m.sessions[guildID]
	m.mu.Unlock()
	if current == nil {
		return Snapshot{}, false
	}
	current.mu.Lock()
	defer current.mu.Unlock()
	return current.snapshotLocked(), true
}

func (m *Module) VoiceConnection(guildID string) VoiceConnection {
	guildID = strings.TrimSpace(guildID)
	if guildID == "" {
		return nil
	}
	m.mu.Lock()
	current := m.sessions[guildID]
	m.mu.Unlock()
	if current == nil {
		return nil
	}
	current.mu.Lock()
	defer current.mu.Unlock()
	return current.voice
}

func (m *Module) recentTitles(guildID string) []string {
	if m.recentPlays == nil {
		return nil
	}
	return m.recentPlays.Recent(guildID)
}
