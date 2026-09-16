package radio

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"gobot/internal/helpers"
)

type Options struct {
	Context                   context.Context
	Now                       func() time.Time
	Settings                  SettingsStore
	Voice                     VoicePort
	Playback                  PlaybackPort
	AutoPlay                  TrackProvider
	TrackResolver             QueryFetcher
	JuiceResolver             QueryFetcher
	RecentPlays               *RecentPlaysTracker
	RecentPath                string
	RecentPersistInterval     time.Duration
	Inactivity                *InactivityManager
	Watchdog                  *PlaybackWatchdog
	Controller                ControllerPort
	ControllerRefreshInterval time.Duration
	LoadingRefreshDelay       time.Duration
	IdleSweepInterval         time.Duration
	IdleSessionAge            time.Duration
	Prebuffer                 AudioPreparer
	Cache                     *LRUSongCache
	Warn                      func(error)
}

type EnqueueResult struct {
	Snapshot Snapshot
	Started  bool
}

type ControlResult struct {
	Snapshot Snapshot
}

type Module struct {
	mu                        sync.Mutex
	sessions                  map[string]*session
	ctx                       context.Context
	workerCtx                 context.Context
	workerCancel              context.CancelFunc
	workerMu                  sync.Mutex
	workersClosed             bool
	workerWg                  sync.WaitGroup
	shutdownMu                sync.Mutex
	shutdownDone              chan struct{}
	shutdownErr               error
	leaseMu                   sync.Mutex
	leasedPaths               map[string]int
	now                       func() time.Time
	settings                  SettingsStore
	voice                     VoicePort
	playback                  PlaybackPort
	autoPlay                  TrackProvider
	trackResolver             QueryFetcher
	resolves                  sync.Map
	recentPlays               *RecentPlaysTracker
	recentPath                string
	recentPersistInterval     time.Duration
	recentPersistMu           sync.Mutex
	recentPersistLastWarn     time.Time
	inactivity                *InactivityManager
	watchdog                  *PlaybackWatchdog
	stallCounts               map[string]int
	stallMu                   sync.Mutex
	controller                ControllerPort
	controllerRefreshInterval time.Duration
	loadingRefreshDelay       time.Duration
	idleSweepInterval         time.Duration
	idleSessionAge            time.Duration
	prebuffer                 AudioPreparer
	cache                     *LRUSongCache
	cacheDiagMu               sync.Mutex
	cacheDiagLastWarn         time.Time
	warn                      func(error)
	stopping                  bool
}

func New(options Options) (*Module, error) {
	ctx := options.Context
	if ctx == nil {
		return nil, ErrMissingContext
	}
	workerCtx, workerCancel := context.WithCancel(ctx)
	now := options.Now
	if now == nil {
		now = time.Now
	}
	recentPlays := options.RecentPlays
	if recentPlays == nil {
		recentPlays = NewRecentPlaysTracker(DefaultRecentPlayCap, DefaultRecentMaxAge)
	}
	inactivity := options.Inactivity
	if inactivity == nil {
		inactivity = NewInactivityManager(DefaultInactivityConfig())
	}
	watchdog := options.Watchdog
	if watchdog == nil {
		watchdog = NewPlaybackWatchdog(DefaultWatchdogConfig())
	}
	controllerRefreshInterval := options.ControllerRefreshInterval
	if controllerRefreshInterval <= 0 {
		controllerRefreshInterval = 15 * time.Second
	}
	loadingRefreshDelay := options.LoadingRefreshDelay
	if loadingRefreshDelay <= 0 {
		loadingRefreshDelay = 5 * time.Second
	}
	idleSweepInterval := options.IdleSweepInterval
	idleSessionAge := options.IdleSessionAge
	if idleSessionAge <= 0 {
		idleSessionAge = 15 * time.Minute
	}
	recentPersistInterval := options.RecentPersistInterval
	if recentPersistInterval <= 0 {
		recentPersistInterval = 10 * time.Minute
	}
	trackResolver := options.TrackResolver
	if trackResolver == nil {
		trackResolver = options.JuiceResolver
	}
	m := &Module{
		sessions:                  make(map[string]*session),
		ctx:                       ctx,
		workerCtx:                 workerCtx,
		workerCancel:              workerCancel,
		now:                       now,
		settings:                  options.Settings,
		voice:                     options.Voice,
		playback:                  options.Playback,
		autoPlay:                  options.AutoPlay,
		trackResolver:             trackResolver,
		recentPlays:               recentPlays,
		recentPath:                options.RecentPath,
		recentPersistInterval:     recentPersistInterval,
		inactivity:                inactivity,
		watchdog:                  watchdog,
		stallCounts:               make(map[string]int),
		leasedPaths:               make(map[string]int),
		controller:                options.Controller,
		controllerRefreshInterval: controllerRefreshInterval,
		loadingRefreshDelay:       loadingRefreshDelay,
		idleSweepInterval:         idleSweepInterval,
		idleSessionAge:            idleSessionAge,
		prebuffer:                 options.Prebuffer,
		cache:                     options.Cache,
		warn:                      options.Warn,
	}
	if m.recentPlays != nil && m.recentPath != "" {
		if err := m.recentPlays.LoadFromFile(m.recentPath); err != nil && !os.IsNotExist(err) && m.warn != nil {
			m.warn(fmt.Errorf("failed to load recent plays from %s: %w", m.recentPath, err))
		}
	}
	if m.cache != nil {
		m.cache.SetLeaseChecker(m.IsPathInUse)
		m.cache.SetDiagHook(func(o CacheOverage) {
			m.cacheDiagMu.Lock()
			shouldWarn := m.now().Sub(m.cacheDiagLastWarn) >= time.Minute
			if shouldWarn {
				m.cacheDiagLastWarn = m.now()
			}
			m.cacheDiagMu.Unlock()
			if shouldWarn && m.warn != nil {
				m.warn(fmt.Errorf("cache overage: %d songs %d bytes over, %d leased skipped", o.OverSongs, o.OverBytes, o.Leased))
			}
		})
	}
	if m.idleSweepInterval > 0 {
		m.startIdleSessionSweeper()
	}
	if m.recentPlays != nil && m.recentPath != "" {
		m.startRecentPersistLoop()
	}
	return m, nil
}

func (m *Module) startRecentPersistLoop() {
	m.startWorker(func() {
		ticker := time.NewTicker(m.recentPersistInterval)
		defer ticker.Stop()
		for {
			select {
			case <-m.workerCtx.Done():
				return
			case <-ticker.C:
				_ = m.persistRecentPlays()
			}
		}
	})
}

func (m *Module) persistRecentPlays() error {
	if m.recentPlays == nil || m.recentPath == "" {
		return nil
	}
	err := m.recentPlays.SaveToFile(m.recentPath)
	if err == nil || m.warn == nil {
		return err
	}
	now := m.now()
	m.recentPersistMu.Lock()
	shouldWarn := now.Sub(m.recentPersistLastWarn) >= time.Minute
	if shouldWarn {
		m.recentPersistLastWarn = now
	}
	m.recentPersistMu.Unlock()
	if shouldWarn {
		m.warn(fmt.Errorf("failed to save recent plays to %s: %w", m.recentPath, err))
	}
	return err
}

func (m *Module) tryAddWorker() bool {
	m.workerMu.Lock()
	defer m.workerMu.Unlock()
	if m.workersClosed {
		return false
	}
	m.workerWg.Add(1)
	return true
}

func (m *Module) closeWorkerAdmission() {
	m.workerMu.Lock()
	m.workersClosed = true
	m.workerMu.Unlock()
}

func (m *Module) startWorker(worker func()) bool {
	if worker == nil || !m.tryAddWorker() {
		return false
	}
	go func() {
		defer m.workerWg.Done()
		defer func() {
			if recovered := recover(); recovered != nil {
				logRecoveredPanic("module worker", recovered)
			}
		}()
		worker()
	}()
	return true
}

func logRecoveredPanic(component string, recovered any) {
	slog.Error("radio worker panic recovered", "component", component, "panic", recovered, "stack", string(debug.Stack()))
}

func (m *Module) LifecycleContext() context.Context {
	return m.ctx
}

func (m *Module) CanControl(guildID, userVoiceChannelID string, isPrivileged bool) (bool, string) {
	mode := RestrictionVoiceChannel
	botChannelID := ""
	isConnected := false
	if m != nil {
		if snap, ok := m.Snapshot(guildID); ok {
			mode = snap.RestrictionMode
			botChannelID = snap.VoiceChannelID
			isConnected = snap.VoiceChannelID != ""
		}
	}
	if isPrivileged {
		return true, ""
	}
	if mode == RestrictionModerators {
		return false, "Radio control is set to **Mods Only**."
	}
	if mode == RestrictionAll {
		return true, ""
	}
	if strings.TrimSpace(userVoiceChannelID) == "" {
		return false, "You must be connected to a voice channel to use radio controls."
	}
	if botChannelID != "" && isConnected && userVoiceChannelID != botChannelID {
		return false, fmt.Sprintf("You must be connected to <#%s> to use radio controls.", botChannelID)
	}
	return true, ""
}

func (m *Module) ResolveQuery(ctx context.Context, query string) ([]*Track, error) {
	if ctx == nil {
		ctx = m.ctx
	}
	clean := strings.TrimSpace(query)
	if clean == "" {
		return nil, fmt.Errorf("empty query")
	}
	call := &resolveCall{done: make(chan struct{})}
	actual, loaded := m.resolves.LoadOrStore(clean, call)
	if loaded {
		inFlight, ok := actual.(*resolveCall)
		if !ok {
			return nil, fmt.Errorf("unexpected resolve call type in module")
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-inFlight.done:
			return cloneTrackPointers(inFlight.tracks), inFlight.err
		}
	}
	tracks, err := ResolveQueryWithFetcher(ctx, m.trackResolver, clean)
	call.tracks = cloneTrackPointers(tracks)
	call.err = err
	close(call.done)
	m.resolves.Delete(clean)
	return cloneTrackPointers(tracks), err
}

func (m *Module) Connect(ctx context.Context, request ConnectRequest) (Snapshot, error) {
	if ctx == nil {
		ctx = m.ctx
	}
	if strings.TrimSpace(request.VoiceChannelID) == "" {
		return Snapshot{}, ErrInvalidRequest
	}
	s, err := m.getSession(ctx, request.GuildID)
	if err != nil {
		return Snapshot{}, err
	}
	s.voiceJoinMu.Lock()
	defer s.voiceJoinMu.Unlock()
	s.mu.Lock()
	if s.stopping {
		s.mu.Unlock()
		return Snapshot{}, ErrShuttingDown
	}
	s.lastActivity = m.now()
	if s.voiceChannelID == request.VoiceChannelID && s.voice != nil && !s.voice.IsClosed() {
		if request.TextChannelID != "" {
			s.textChannelID = request.TextChannelID
		} else if s.textChannelID == "" && request.VoiceChannelID != "" {
			s.textChannelID = request.VoiceChannelID
		}
		snapshot := s.snapshotLocked()
		s.mu.Unlock()
		if request.TextChannelID != "" && snapshot.Current != nil && m.controller != nil {
			m.syncControllerAsync(ctx, request.GuildID, snapshot.SessionEpoch, snapshot.PlaybackGeneration)
		}
		if !request.SuppressAutoplay || len(snapshot.Queue) > 0 || snapshot.Current != nil {
			m.startNext(request.GuildID)
		}
		if updated, ok := m.Snapshot(request.GuildID); ok {
			return updated, nil
		}
		return snapshot, nil
	}
	expectedEpoch := s.sessionEpoch
	movingChannels := s.voiceChannelID != request.VoiceChannelID
	s.mu.Unlock()

	var connection VoiceConnection
	var oldConnection VoiceConnection
	if m.voice != nil {
		s.mu.Lock()
		s.voiceTransition = true
		s.mu.Unlock()
		defer func() {
			s.mu.Lock()
			s.voiceTransition = false
			s.mu.Unlock()
		}()
		joinCtx, cancelJoin := bindVoiceReconnectEpoch(ctx, s, expectedEpoch)
		conn, errJoin := m.voice.Join(withVoiceReconnectEpoch(joinCtx, expectedEpoch), request.GuildID, request.VoiceChannelID)
		cancelJoin()
		if errJoin != nil {
			s.mu.Lock()
			snap := s.snapshotLocked()
			s.mu.Unlock()
			return snap, errJoin
		}
		connection = conn

		s.mu.Lock()
		if s.stopping || s.sessionEpoch != expectedEpoch {
			s.mu.Unlock()
			connection.Close()
			return Snapshot{}, ErrStaleOperation
		}
		oldConnection = s.voice
		s.voice = connection
	} else {
		s.mu.Lock()
		if s.stopping || s.sessionEpoch != expectedEpoch {
			s.mu.Unlock()
			return Snapshot{}, ErrStaleOperation
		}
	}
	if movingChannels {
		s.nextSessionEpochLocked()
		s.nextPlaybackGenerationLocked()
	}

	oldPlayback := s.playback
	oldCancel := s.playbackCancel
	prebuffer := s.prebuffer
	s.playback = nil
	s.playbackPath = ""
	s.playbackCancel = nil
	if s.current != nil {
		s.queue = append([]Track{s.current.Clone()}, s.queue...)
		s.current = nil
		s.nextQueueRevisionLocked()
	}
	s.phase = PhaseIdle
	s.voiceChannelID = request.VoiceChannelID
	if request.TextChannelID != "" {
		s.textChannelID = request.TextChannelID
	} else if s.textChannelID == "" && s.voiceChannelID != "" {
		s.textChannelID = s.voiceChannelID
	}
	snapshot := s.snapshotLocked()
	s.mu.Unlock()

	if oldConnection != nil && oldConnection != connection {
		oldConnection.Close()
	}
	if oldPlayback != nil {
		oldPlayback.Stop()
	}
	if oldCancel != nil {
		oldCancel()
	}
	if prebuffer != nil {
		prebuffer.Cancel()
	}

	if !request.SuppressAutoplay || len(snapshot.Queue) > 0 || snapshot.Current != nil {
		m.startNext(request.GuildID)
	}
	if updated, ok := m.Snapshot(request.GuildID); ok {
		return updated, nil
	}
	return snapshot, nil
}

func (m *Module) Enqueue(ctx context.Context, request EnqueueRequest) (EnqueueResult, error) {
	if ctx == nil {
		ctx = m.ctx
	}
	if len(request.Tracks) == 0 {
		return EnqueueResult{}, ErrInvalidRequest
	}
	for i := range request.Tracks {
		if err := ValidateTrackDuration(&request.Tracks[i]); err != nil {
			return EnqueueResult{}, err
		}
	}
	s, err := m.getSession(ctx, request.GuildID)
	if err != nil {
		return EnqueueResult{}, err
	}
	s.connectMu.Lock()
	defer s.connectMu.Unlock()
	s.mu.Lock()
	if s.stopping {
		s.mu.Unlock()
		return EnqueueResult{}, ErrShuttingDown
	}
	userPending := 0
	for _, t := range s.queue {
		if !t.IsAutoplay {
			userPending++
		}
	}
	if userPending+len(request.Tracks) > MaxQueuedTracks {
		s.mu.Unlock()
		return EnqueueResult{}, &QueueLimitError{
			Limit:     MaxQueuedTracks,
			Pending:   userPending,
			Requested: len(request.Tracks),
		}
	}
	s.lastActivity = m.now()
	if request.TextChannelID != "" {
		s.textChannelID = request.TextChannelID
	} else if s.textChannelID == "" && s.voiceChannelID != "" {
		s.textChannelID = s.voiceChannelID
	}
	s.enqueueLocked(request.Tracks, request.Placement)
	if s.phase == PhaseIdle && len(s.queue) > 0 {
		s.phase = PhaseLoading
	}
	snapshot := s.snapshotLocked()
	shouldStart := s.voice != nil && !s.voice.IsClosed() && s.playback == nil && !s.playbackStarting
	s.mu.Unlock()

	if shouldStart {
		m.startNext(request.GuildID)
		if updated, ok := m.Snapshot(request.GuildID); ok {
			snapshot = updated
		}
	}
	if !shouldStart {
		m.refreshPrebuffer(request.GuildID)
	}
	return EnqueueResult{
		Snapshot: snapshot,
		Started:  shouldStart,
	}, nil
}

func (m *Module) Control(ctx context.Context, request ControlRequest) (ControlResult, error) {
	if ctx == nil {
		ctx = m.ctx
	}
	s, err := m.getSession(ctx, request.GuildID)
	if err != nil {
		return ControlResult{}, err
	}
	s.effectMu.Lock()
	defer s.effectMu.Unlock()
	s.mu.Lock()
	if s.stopping {
		s.mu.Unlock()
		return ControlResult{}, ErrShuttingDown
	}
	s.lastActivity = m.now()
	effects, errControl := s.applyControlLocked(request.Action, m.now())
	if errControl != nil {
		s.mu.Unlock()
		return ControlResult{}, errControl
	}
	snapshot := s.snapshotLocked()
	s.mu.Unlock()

	if effects.stopPlayback && effects.playback != nil {
		effects.playback.Stop()
	}
	if effects.cancelPlayback != nil {
		effects.cancelPlayback()
	}
	if effects.cancelLoading != nil {
		effects.cancelLoading()
	}
	if effects.pausePlayback && effects.playback != nil {
		effects.playback.Pause()
	}
	if effects.resumePlayback && effects.playback != nil {
		effects.playback.Resume()
	}
	if effects.disconnect {
		if effects.connection != nil {
			effects.connection.Close()
		}
		if m.voice != nil {
			_ = m.voice.Disconnect(ctx, request.GuildID)
		}
	}

	result := ControlResult{Snapshot: snapshot}
	if effects.startNext {
		m.startNext(request.GuildID)
		if updated, ok := m.Snapshot(request.GuildID); ok {
			result.Snapshot = updated
		}
	}
	if !effects.startNext {
		m.refreshPrebuffer(request.GuildID)
	}

	if m.controller != nil && result.Snapshot.TextChannelID != "" {
		m.syncControllerAsync(ctx, request.GuildID, result.Snapshot.SessionEpoch, result.Snapshot.PlaybackGeneration)
	}

	return result, nil
}

func (m *Module) Shutdown(ctx context.Context) error {
	if ctx == nil {
		ctx = m.ctx
	}
	m.shutdownMu.Lock()
	if m.shutdownDone == nil {
		m.shutdownDone = make(chan struct{})
		shutdownDone := m.shutdownDone
		helpers.Spawn(func() {
			err := m.shutdown(ctx)
			m.shutdownMu.Lock()
			m.shutdownErr = err
			close(shutdownDone)
			m.shutdownMu.Unlock()
		})
	}
	done := m.shutdownDone
	m.shutdownMu.Unlock()
	select {
	case <-done:
		m.shutdownMu.Lock()
		err := m.shutdownErr
		m.shutdownMu.Unlock()
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *Module) shutdown(ctx context.Context) error {
	m.mu.Lock()
	m.stopping = true
	sessions := make([]*session, 0, len(m.sessions))
	for _, s := range m.sessions {
		sessions = append(sessions, s)
	}
	m.mu.Unlock()
	m.closeWorkerAdmission()

	if m.workerCancel != nil {
		m.workerCancel()
	}

	now := m.now()
	var cleanupErrs []error
	type sessionCleanup struct {
		guildID    string
		connection VoiceConnection
		playback   PlaybackHandle
		cancel     context.CancelFunc
		prebuffer  *PrebufferTask
	}
	cleanups := make([]sessionCleanup, 0, len(sessions))
	for _, s := range sessions {
		s.effectMu.Lock()
		s.mu.Lock()
		if m.inactivity != nil {
			m.inactivity.StopTimer(s.guildID)
		}
		if m.watchdog != nil {
			m.watchdog.StopExhaustionTimer(s.guildID)
		}
		cleanup := sessionCleanup{
			guildID:    s.guildID,
			connection: s.voice,
			playback:   s.playback,
			cancel:     s.playbackCancel,
			prebuffer:  s.prebuffer,
		}
		s.playback = nil
		s.playbackPath = ""
		s.playbackCancel = nil
		s.beginStoppingLocked()
		s.finishStoppingLocked(now)
		s.mu.Unlock()
		s.effectMu.Unlock()
		cleanups = append(cleanups, cleanup)
	}
	for _, cleanup := range cleanups {
		if cleanup.prebuffer != nil {
			if err := cleanup.prebuffer.closeAndWait(ctx); err != nil {
				cleanupErrs = append(cleanupErrs, err)
			}
		}
		if cleanup.playback != nil {
			cleanup.playback.Stop()
		}
		if cleanup.cancel != nil {
			cleanup.cancel()
		}
		if cleanup.connection != nil {
			cleanup.connection.Close()
		}
		if cleanup.connection != nil && m.voice != nil {
			if err := m.voice.Disconnect(ctx, cleanup.guildID); err != nil {
				cleanupErrs = append(cleanupErrs, err)
			}
		}
	}

	workerDone := make(chan struct{})
	helpers.Spawn(func() {
		m.workerWg.Wait()
		close(workerDone)
	})

	select {
	case <-workerDone:
	case <-ctx.Done():
		cleanupErrs = append(cleanupErrs, ctx.Err())
	}

	if m.controller != nil {
		if err := m.drainPendingControllers(ctx, m.controller, true); err != nil {
			cleanupErrs = append(cleanupErrs, err)
		}
	}
	if m.recentPlays != nil && m.recentPath != "" {
		if err := m.persistRecentPlays(); err != nil {
			cleanupErrs = append(cleanupErrs, err)
		}
	}
	return errors.Join(cleanupErrs...)
}
