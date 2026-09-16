package radio

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jonas747/ogg"

	"gobot/internal/helpers"
)

type PlaybackRequest struct {
	GuildID            string
	Track              Track
	SessionEpoch       uint64
	PlaybackGeneration uint64
	PreparedPath       string
	PreparedPathOwned  bool
}

type PlaybackHandle interface {
	Done() <-chan struct{}
	Err() error
	Pause()
	Resume()
	Stop()
	LastFrameTime() time.Time
}

type PlaybackPort interface {
	Start(context.Context, PlaybackRequest) (PlaybackHandle, error)
}

type AudioPreparer interface {
	PrepareTrack(ctx context.Context, track *Track) (string, bool, error)
}

type PlaybackConfig struct {
	MaxRetries    int
	RetryDelay    time.Duration
	StreamOptions StreamOptions
	ProbeDuration func(string) (int, error)
}

func DefaultPlaybackConfig() PlaybackConfig {
	return PlaybackConfig{
		MaxRetries:    5,
		RetryDelay:    200 * time.Millisecond,
		StreamOptions: DefaultStreamOptions(),
		ProbeDuration: func(path string) (int, error) {
			duration := ProbeFileDuration(path)
			if duration <= 0 {
				return 0, fmt.Errorf("could not inspect the media file")
			}
			return duration, nil
		},
	}
}

type VoiceAccessor func(guildID string) VoiceConnection

type Engine struct {
	preparer AudioPreparer
	voice    VoiceAccessor
	config   PlaybackConfig
	warn     func(error)
}

func NewEngine(preparer AudioPreparer, voice VoiceAccessor, config PlaybackConfig, warn func(error)) *Engine {
	if preparer == nil {
		preparer = (*LRUSongCache)(nil)
	}
	if config.MaxRetries <= 0 {
		config.MaxRetries = 5
	}
	if config.RetryDelay <= 0 {
		config.RetryDelay = 200 * time.Millisecond
	}
	return &Engine{
		preparer: preparer,
		voice:    voice,
		config:   config,
		warn:     warn,
	}
}

func cleanupOwnedPreparedPath(path string, owned bool) {
	if !owned || path == "" || !pathWithinRoot(RadioBufferDir(), path) {
		return
	}
	_ = os.Remove(path)
}

func (e *Engine) Start(ctx context.Context, req PlaybackRequest) (PlaybackHandle, error) {
	if ctx == nil {
		return nil, ErrMissingContext
	}

	var opusPath string
	var opusOwned bool
	var lastErr error
	cleanupOnReturn := false
	defer func() {
		if cleanupOnReturn {
			cleanupOwnedPreparedPath(opusPath, opusOwned)
		}
	}()

	trackCopy := req.Track.Clone()
	if req.PreparedPath != "" {
		if _, err := os.Stat(req.PreparedPath); err == nil {
			opusPath = req.PreparedPath
			opusOwned = req.PreparedPathOwned
			cleanupOnReturn = true
		}
	}

	for attempt := 1; opusPath == "" && attempt <= e.config.MaxRetries; attempt++ {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		path, owned, err := e.preparer.PrepareTrack(ctx, &trackCopy)
		if err == nil && path != "" {
			opusPath = path
			opusOwned = owned
			cleanupOnReturn = true
			lastErr = nil
			break
		}

		lastErr = err
		if attempt < e.config.MaxRetries {
			timer := time.NewTimer(e.config.RetryDelay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, ctx.Err()
			case <-timer.C:
			}
		}
	}

	if lastErr != nil {
		if e.warn != nil {
			e.warn(fmt.Errorf("track preparation failed after %d attempts: %w", e.config.MaxRetries, lastErr))
		}
		return nil, lastErr
	}

	probedDuration := 0
	if e.config.ProbeDuration != nil {
		duration, errProbe := e.config.ProbeDuration(opusPath)
		if errProbe != nil || duration < 2 || duration > MaxTrackDurationSec {
			if errProbe == nil {
				if duration < 2 {
					errProbe = fmt.Errorf("media duration is %d seconds; minimum is 2 seconds", duration)
				} else {
					errProbe = fmt.Errorf("media duration is %d seconds; maximum allowed is %d seconds", duration, MaxTrackDurationSec)
				}
			}
			return nil, fmt.Errorf("%w: %v", ErrPlaybackSource, errProbe)
		}
		probedDuration = duration
	}

	pathOwned := opusOwned
	for attempt := 1; attempt <= e.config.MaxRetries; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		var voice VoiceConnection
		if e.voice != nil {
			voice = e.voice(req.GuildID)
		}
		if voice == nil || voice.IsClosed() {
			lastErr = ErrVoiceUnavailable
		} else {
			handle, err := startOpusStream(ctx, opusPath, voice, e.config.StreamOptions, pathOwned, probedDuration)
			if err == nil {
				cleanupOnReturn = false
				return handle, nil
			}
			lastErr = err
		}

		if attempt < e.config.MaxRetries {
			if _, err := os.Stat(opusPath); err != nil {
				path, owned, errPrepare := e.preparer.PrepareTrack(ctx, &trackCopy)
				if errPrepare == nil && path != "" {
					if opusPath != path && opusOwned {
						cleanupOwnedPreparedPath(opusPath, opusOwned)
					}
					opusPath = path
					opusOwned = owned
					pathOwned = owned
					cleanupOnReturn = true
				} else if errPrepare != nil {
					lastErr = errPrepare
				}
			}
			timer := time.NewTimer(e.config.RetryDelay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, ctx.Err()
			case <-timer.C:
			}
		}
	}
	if e.warn != nil && lastErr != nil {
		e.warn(fmt.Errorf("stream start failed after %d attempts: %w", e.config.MaxRetries, lastErr))
	}
	return nil, lastErr
}

type StreamHandle struct {
	doneOnce      sync.Once
	doneChan      chan struct{}
	terminalErr   error
	stopOnce      sync.Once
	cancel        context.CancelFunc
	paused        atomic.Bool
	closed        atomic.Bool
	lastFrameTime atomic.Int64
	path          string
	duration      int
	healthMu      sync.Mutex
	health        *localSendHealth
}

func (h *StreamHandle) PlaybackPath() string { return h.path }
func (h *StreamHandle) Duration() int        { return h.duration }

func newStreamHandle(cancel context.CancelFunc) *StreamHandle {
	h := &StreamHandle{
		doneChan: make(chan struct{}),
		cancel:   cancel,
		health:   newLocalSendHealth(time.Now()),
	}
	h.lastFrameTime.Store(time.Now().UnixNano())
	return h
}

func (h *StreamHandle) LastFrameTime() time.Time {
	return time.Unix(0, h.lastFrameTime.Load())
}

func (h *StreamHandle) Done() <-chan struct{} {
	return h.doneChan
}

func (h *StreamHandle) Err() error {
	return h.terminalErr
}

func (h *StreamHandle) Pause() {
	h.paused.Store(true)
}

func (h *StreamHandle) Resume() {
	h.paused.Store(false)
}

func (h *StreamHandle) Stop() {
	h.stopOnce.Do(func() {
		h.closed.Store(true)
		if h.cancel != nil {
			h.cancel()
		}
	})
}

func (h *StreamHandle) signalDone(err error) {
	h.doneOnce.Do(func() {
		if err != io.EOF && err != context.Canceled {
			h.terminalErr = err
		}
		close(h.doneChan)
		if h.cancel != nil {
			h.cancel()
		}
	})
}

type StreamOptions struct {
	FrameDuration            time.Duration
	MaxConsecutiveSendErrors int
}

func DefaultStreamOptions() StreamOptions {
	return StreamOptions{
		FrameDuration:            20 * time.Millisecond,
		MaxConsecutiveSendErrors: 15,
	}
}

type opusFrameReader interface {
	ReadOpusFrame() ([]byte, error)
}

func startOpusStream(parentCtx context.Context, filePath string, voice VoiceConnection, options StreamOptions, ownedPath bool, duration ...int) (*StreamHandle, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}

	return startOpusStreamWithReader(parentCtx, filePath, voice, options, ownedPath, NewOggOpusReader(file), file, duration...)
}

func startOpusStreamWithReader(parentCtx context.Context, filePath string, voice VoiceConnection, options StreamOptions, ownedPath bool, reader opusFrameReader, file io.Closer, duration ...int) (*StreamHandle, error) {
	if reader == nil || file == nil {
		return nil, fmt.Errorf("invalid Opus stream reader")
	}

	ctx, cancel := context.WithCancel(parentCtx)
	handle := newStreamHandle(cancel)
	handle.path = filePath
	if len(duration) > 0 && duration[0] > 0 {
		handle.duration = duration[0]
	}

	go func() {
		var result error
		defer func() {
			if recovered := recover(); recovered != nil {
				logRecoveredPanic("Opus stream", recovered)
				result = fmt.Errorf("%w: %v", ErrPlaybackPanic, recovered)
			}
			_ = file.Close()
			if ownedPath && pathWithinRoot(RadioBufferDir(), filePath) {
				_ = os.Remove(filePath)
			}
			if voice != nil && !voice.IsClosed() {
				_ = voice.SetSpeaking(false)
			}
			handle.signalDone(result)
		}()

		if voice != nil && !voice.IsClosed() {
			if errSpeaking := voice.SetSpeaking(true); errSpeaking != nil {
				result = fmt.Errorf("%w: failed to set speaking state: %v", ErrVoiceTransport, errSpeaking)
				return
			}
		}

		frameDuration := options.FrameDuration
		if frameDuration <= 0 {
			frameDuration = 20 * time.Millisecond
		}

		ticker := time.NewTicker(frameDuration)
		defer ticker.Stop()

		var consecutivePendingSince time.Time

		for {
			select {
			case <-ctx.Done():
				result = ctx.Err()
				return
			case <-ticker.C:
			}

			if handle.paused.Load() {
				continue
			}

			frame, errRead := reader.ReadOpusFrame()
			if errRead != nil {
				result = errRead
				if !errors.Is(errRead, io.EOF) && !errors.Is(errRead, context.Canceled) {
					result = fmt.Errorf("%w: %v", ErrPlaybackSource, errRead)
				}
				return
			}
			if len(frame) == 0 {
				continue
			}

			if voice == nil || voice.IsClosed() {
				result = ErrVoiceUnavailable
				return
			}

			errSend := voice.SendOpus(frame)
			if errors.Is(errSend, ErrDAVEFrameHeld) {
				if consecutivePendingSince.IsZero() {
					consecutivePendingSince = time.Now()
				} else if time.Since(consecutivePendingSince) > daveReadyTimeout() {
					result = fmt.Errorf("%w: DAVE handshake timed out during playback", ErrVoiceTransport)
					return
				}
				continue
			}
			if !consecutivePendingSince.IsZero() {
				consecutivePendingSince = time.Time{}
			}

			handle.healthMu.Lock()
			handle.health.record(time.Now(), errSend)
			unhealthy := handle.health.unhealthy(time.Now())
			handle.healthMu.Unlock()
			if unhealthy {
				result = fmt.Errorf("%w: sustained local SendOpus errors", ErrVoiceTransport)
				return
			}
			if errSend == nil {
				handle.lastFrameTime.Store(time.Now().UnixNano())
			}
		}
	}()

	return handle, nil
}

type OggOpusReader struct {
	decoder *ogg.PacketDecoder
}

func NewOggOpusReader(r io.Reader) *OggOpusReader {
	return &OggOpusReader{
		decoder: ogg.NewPacketDecoder(ogg.NewDecoder(r)),
	}
}

func (r *OggOpusReader) ReadOpusFrame() ([]byte, error) {
	for {
		packet, _, err := r.decoder.Decode()
		if err != nil {
			return nil, err
		}
		if len(packet) == 0 {
			continue
		}
		if len(packet) >= 8 && string(packet[0:8]) == "OpusHead" {
			continue
		}
		if len(packet) >= 8 && string(packet[0:8]) == "OpusTags" {
			continue
		}
		return packet, nil
	}
}

func pathWithinRoot(root, path string) bool {
	if root == "" || path == "" {
		return false
	}
	root, err := filepath.Abs(filepath.Clean(root))
	if err != nil {
		return false
	}
	path, err = filepath.Abs(filepath.Clean(path))
	if err != nil {
		return false
	}
	if resolved, resolveErr := filepath.EvalSymlinks(root); resolveErr == nil {
		root = resolved
	}
	if resolved, resolveErr := filepath.EvalSymlinks(path); resolveErr == nil {
		path = resolved
	}
	relative, err := filepath.Rel(root, path)
	if err != nil || relative == "." || relative == ".." || filepath.IsAbs(relative) {
		return false
	}
	return !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

type playbackCompletion struct {
	once   sync.Once
	finish func(error)
}

func (c *playbackCompletion) complete(err error) {
	if c == nil || c.finish == nil {
		return
	}
	c.once.Do(func() { c.finish(err) })
}

func (m *Module) retainLease(path string) {
	if path == "" {
		return
	}
	m.leaseMu.Lock()
	m.leasedPaths[path]++
	m.leaseMu.Unlock()
}

func (m *Module) releaseLease(path string) {
	if path == "" {
		return
	}
	m.leaseMu.Lock()
	if count := m.leasedPaths[path]; count <= 1 {
		delete(m.leasedPaths, path)
	} else {
		m.leasedPaths[path] = count - 1
	}
	m.leaseMu.Unlock()
}

func (m *Module) IsPathInUse(path string) bool {
	m.leaseMu.Lock()
	leased := m.leasedPaths[path] > 0
	m.leaseMu.Unlock()
	if leased {
		return true
	}
	m.mu.Lock()
	sessions := make([]*session, 0, len(m.sessions))
	for _, s := range m.sessions {
		sessions = append(sessions, s)
	}
	m.mu.Unlock()
	for _, s := range sessions {
		s.mu.Lock()
		inUse := s.playbackPath == path || (s.prebuffer != nil && s.prebuffer.HasPath(path))
		if !inUse && s.playback != nil {
			if pathProvider, ok := s.playback.(interface{ PlaybackPath() string }); ok {
				inUse = pathProvider.PlaybackPath() == path
			}
		}
		s.mu.Unlock()
		if inUse {
			return true
		}
	}
	return false
}

func (m *Module) startPrebuffer(task *PrebufferTask, generation, revision uint64, track Track) {
	if task == nil || m.prebuffer == nil {
		return
	}
	task.StartForRevisionOwned(m.workerCtx, generation, revision, track, m.prebuffer, func(worker func()) bool {
		return m.startWorker(worker)
	})
}

func (m *Module) startAutoplayPrebuffer(guildID string, epoch, generation uint64) {
	if m.autoPlay == nil || m.prebuffer == nil {
		return
	}
	m.startWorker(func() {
		m.mu.Lock()
		s := m.sessions[guildID]
		m.mu.Unlock()
		if s == nil {
			return
		}
		s.mu.Lock()
		if s.stopping || s.sessionEpoch != epoch || s.playbackGeneration != generation || len(s.queue) > 0 {
			s.mu.Unlock()
			return
		}
		mode := s.mode
		s.mu.Unlock()

		slog.Info("autoplay background fetch started", "guild_id", guildID, "mode", mode)
		fetchCtx, cancelFetch := context.WithTimeout(m.workerCtx, 10*time.Second)
		track, errFetch := m.autoPlay.NextTrack(fetchCtx, guildID, mode, m.recentTitles(guildID))
		cancelFetch()
		if errFetch != nil || track == nil {
			slog.Warn("autoplay background fetch failed", "guild_id", guildID, "error", errFetch)
			return
		}

		s.mu.Lock()
		if s.stopping || s.sessionEpoch != epoch || s.playbackGeneration != generation || len(s.queue) > 0 {
			s.mu.Unlock()
			return
		}
		autoClone := track.Clone()
		autoClone.IsAutoplay = true
		s.queue = append(s.queue, autoClone)
		s.nextQueueRevisionLocked()
		task := s.prebuffer
		revision := s.queueRevision
		queuedTrack := autoClone
		s.mu.Unlock()

		slog.Info("autoplay prebuffering candidate track", "guild_id", guildID, "title", queuedTrack.Title, "duration", queuedTrack.Duration)
		if task != nil {
			m.startPrebuffer(task, generation, revision, queuedTrack)
		}
		m.syncControllerAsync(m.workerCtx, guildID, epoch, generation)
	})
}

func (m *Module) scheduleLoadingRefresh(guildID string, epoch, generation uint64) {
	m.mu.Lock()
	s := m.sessions[guildID]
	m.mu.Unlock()
	if s == nil {
		return
	}
	var ctx context.Context
	var cancel context.CancelFunc
	if m.controller != nil && m.loadingRefreshDelay > 0 {
		ctx, cancel = context.WithCancel(m.workerCtx)
	}
	s.mu.Lock()
	s.invalidateLoadingRefreshLocked()
	s.loadingSince = m.now()
	s.loadingEpoch = epoch
	s.loadingGeneration = generation
	s.loadingRefreshCancel = cancel
	s.mu.Unlock()
	if cancel == nil {
		return
	}
	if !m.startWorker(func() {
		timer := time.NewTimer(m.loadingRefreshDelay)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
			return
		}
		s.mu.Lock()
		active := !s.stopping && s.sessionEpoch == epoch && s.playbackGeneration == generation && s.phase == PhaseLoading && s.loadingEpoch == epoch && s.loadingGeneration == generation && !s.loadingSince.IsZero()
		s.mu.Unlock()
		if active {
			m.syncController(ctx, guildID, epoch, generation)
		}
	}) {
		cancel()
		s.mu.Lock()
		if s.loadingEpoch == epoch && s.loadingGeneration == generation {
			s.invalidateLoadingRefreshLocked()
		}
		s.mu.Unlock()
	}
}

func (m *Module) invalidateLoadingRefresh(guildID string, epoch, generation uint64) {
	m.mu.Lock()
	s := m.sessions[guildID]
	m.mu.Unlock()
	if s == nil {
		return
	}
	s.mu.Lock()
	if s.loadingEpoch == epoch && s.loadingGeneration == generation {
		s.invalidateLoadingRefreshLocked()
	}
	s.mu.Unlock()
}

func (m *Module) refreshPrebuffer(guildID string) {
	m.mu.Lock()
	s := m.sessions[guildID]
	m.mu.Unlock()
	if s == nil || m.prebuffer == nil {
		return
	}
	s.mu.Lock()
	task := s.prebuffer
	var next *Track
	var generation, revision uint64
	if task != nil && s.playback != nil && len(s.queue) > 0 && s.phase == PhasePlaying {
		clone := s.queue[0].Clone()
		next = &clone
		generation = s.playbackGeneration
		revision = s.queueRevision
	}
	s.mu.Unlock()
	if task == nil {
		return
	}
	if next == nil {
		task.Cancel()
		return
	}
	m.startPrebuffer(task, generation, revision, *next)
}

func (m *Module) startNext(guildID string) {
	m.mu.Lock()
	s := m.sessions[guildID]
	m.mu.Unlock()
	if s == nil || m.playback == nil {
		return
	}

	s.mu.Lock()
	if s.stopping || s.voice == nil || s.voice.IsClosed() || s.playback != nil || s.playbackStarting {
		s.mu.Unlock()
		return
	}
	s.playbackStarting = true
	s.phase = PhaseLoading
	s.mu.Unlock()

	if !m.startWorker(func() { m.startNextWorker(guildID) }) {
		s.mu.Lock()
		if s.playbackStarting {
			s.playbackStarting = false
			s.phase = PhaseIdle
		}
		s.mu.Unlock()
	}
}

func (m *Module) startNextWorker(guildID string) {
	var (
		session            *session
		attemptEpoch       uint64
		attemptGeneration  uint64
		playbackHandle     PlaybackHandle
		playbackCancel     context.CancelFunc
		leasedPath         string
		playbackCommitted  bool
		playbackWorkerLive bool
		sessionLocked      bool
	)
	defer func() {
		if recovered := recover(); recovered != nil {
			logRecoveredPanic("playback transition", recovered)
			if sessionLocked && session != nil {
				session.mu.Unlock()
			}
			if playbackCancel != nil {
				playbackCancel()
			}
			if playbackHandle != nil && !playbackWorkerLive {
				playbackHandle.Stop()
				select {
				case <-playbackHandle.Done():
				case <-m.workerCtx.Done():
				}
			}
			if leasedPath != "" {
				m.releaseLease(leasedPath)
			}
			m.recoverPlaybackTransition(guildID, attemptEpoch, attemptGeneration, playbackHandle, playbackCommitted)
		}
	}()

	m.mu.Lock()
	s := m.sessions[guildID]
	m.mu.Unlock()
	if s == nil {
		return
	}
	session = s

	if !m.tryFetchAutoplayCandidate(s, guildID) {
		return
	}

	track, preparedPath, preparedOwned, epoch, generation, playbackContext, cancel, ok := m.selectAndPrepareNextTrack(s)
	if !ok {
		return
	}
	attemptEpoch = epoch
	attemptGeneration = generation
	playbackCancel = cancel

	m.scheduleLoadingRefresh(guildID, epoch, generation)
	m.retainLease(preparedPath)
	leasedPath = preparedPath

	handle, err := m.playback.Start(playbackContext, PlaybackRequest{
		GuildID:            guildID,
		Track:              track,
		SessionEpoch:       epoch,
		PlaybackGeneration: generation,
		PreparedPath:       preparedPath,
		PreparedPathOwned:  preparedOwned,
	})
	playbackHandle = handle
	m.invalidateLoadingRefresh(guildID, epoch, generation)

	if err != nil || handle == nil {
		playbackCancel = nil
		cancel()
		m.handlePlaybackStartError(s, guildID, track, epoch, generation, preparedPath, err)
		return
	}

	committed, live := m.commitPlaybackSession(s, guildID, track, epoch, generation, handle, leasedPath, cancel)
	playbackCommitted = committed
	playbackWorkerLive = live
}

func (m *Module) tryFetchAutoplayCandidate(s *session, guildID string) bool {
	s.mu.Lock()
	if s.stopping || s.voice == nil || s.voice.IsClosed() || s.playback != nil {
		s.playbackStarting = false
		s.mu.Unlock()
		return false
	}
	needAutoplay := len(s.queue) == 0 && s.current == nil && s.loopMode == LoopOff && m.autoPlay != nil
	if !needAutoplay {
		s.mu.Unlock()
		return true
	}
	epoch := s.sessionEpoch
	revision := s.queueRevision
	mode := s.mode
	s.autoplayInFlight = true
	s.autoplayToken++
	autoplayToken := s.autoplayToken
	s.mu.Unlock()

	slog.Info("autoplay cold fetching candidate", "guild_id", guildID, "mode", mode)
	fetchCtx, cancelFetch := context.WithTimeout(m.workerCtx, 10*time.Second)
	autoTrack, errFetch := m.autoPlay.NextTrack(fetchCtx, guildID, mode, m.recentTitles(guildID))
	cancelFetch()

	s.mu.Lock()
	defer s.mu.Unlock()
	stale := s.stopping || s.sessionEpoch != epoch || s.autoplayToken != autoplayToken
	if s.autoplayToken == autoplayToken {
		s.autoplayInFlight = false
	}
	if !stale && s.queueRevision == revision && len(s.queue) == 0 && s.current == nil && errFetch == nil && autoTrack != nil {
		autoClone := autoTrack.Clone()
		autoClone.IsAutoplay = true
		slog.Info("autoplay cold candidate acquired", "guild_id", guildID, "title", autoClone.Title, "duration", autoClone.Duration)
		s.queue = append(s.queue, autoClone)
		s.nextQueueRevisionLocked()
	} else if errFetch != nil && !stale && s.queueRevision == revision && len(s.queue) == 0 && s.current == nil {
		s.playbackStarting = false
		s.phase = PhaseIdle
		if m.warn != nil {
			m.warn(fmt.Errorf("autoplay candidate fetch failed for guild %s: %w", guildID, errFetch))
		}
		return false
	}
	if stale || s.voice == nil || s.voice.IsClosed() || s.playback != nil {
		s.playbackStarting = false
		return false
	}
	return true
}

func (m *Module) selectAndPrepareNextTrack(s *session) (track Track, preparedPath string, preparedOwned bool, epoch, generation uint64, playbackContext context.Context, cancel context.CancelFunc, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.stopping || s.voice == nil || s.voice.IsClosed() || s.playback != nil {
		s.playbackStarting = false
		return Track{}, "", false, 0, 0, nil, nil, false
	}
	if len(s.queue) == 0 && s.current == nil {
		s.playbackStarting = false
		s.phase = PhaseIdle
		return Track{}, "", false, 0, 0, nil, nil, false
	}

	previousGeneration := s.playbackGeneration
	selectionRevision := s.queueRevision
	if !s.selectNextQueuedLocked() {
		s.playbackStarting = false
		s.phase = PhaseIdle
		return Track{}, "", false, 0, 0, nil, nil, false
	}

	track = s.current.Clone()
	track.Title = helpers.CleanTrackTitle(track.Title)
	if s.current != nil {
		s.current.Title = track.Title
	}
	if s.prebuffer != nil {
		preparedPath, preparedOwned = s.prebuffer.TakeFor(previousGeneration, selectionRevision, track)
	}
	if preparedPath != "" && (track.Duration <= 0 || track.Duration == 180) {
		if dur := ProbeFileDuration(preparedPath); dur > 0 {
			track.Duration = dur
			if s.current != nil {
				s.current.Duration = dur
			}
		}
	}
	slog.Info("next track selected for playback", "guild_id", s.guildID, "title", track.Title, "has_prebuffered_path", preparedPath != "")

	generation = s.nextPlaybackGenerationLocked()
	epoch = s.sessionEpoch
	playbackContext, cancel = context.WithCancel(m.workerCtx)
	s.playbackCancel = cancel
	s.playbackPath = preparedPath
	return track, preparedPath, preparedOwned, epoch, generation, playbackContext, cancel, true
}

func (m *Module) handlePlaybackStartError(s *session, guildID string, track Track, epoch, generation uint64, preparedPath string, err error) {
	s.mu.Lock()
	currentAttempt := s.sessionEpoch == epoch && s.playbackGeneration == generation && !s.stopping
	sourceFailed := err != nil && errors.Is(err, ErrPlaybackSource)
	isCanceled := err != nil && (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded))
	syncController := false
	tooManyFailures := false

	if currentAttempt {
		if sourceFailed {
			s.consecutiveStartupFailures++
			if s.consecutiveStartupFailures >= 5 {
				tooManyFailures = true
			}
		}
		if sourceFailed && s.current != nil {
			failed := s.current.Clone()
			s.current = nil
			s.addHistoryLocked(failed)
			s.nextQueueRevisionLocked()
		} else if s.current != nil {
			s.queue = append([]Track{s.current.Clone()}, s.queue...)
			s.current = nil
			s.nextQueueRevisionLocked()
		}
		s.playbackCancel = nil
		s.playbackPath = ""
		s.playbackStarting = false
		shouldContinue := sourceFailed && !tooManyFailures && s.voice != nil && !s.voice.IsClosed() && (len(s.queue) > 0 || m.autoPlay != nil)
		if shouldContinue {
			s.phase = PhaseLoading
		} else {
			s.phase = PhaseIdle
			syncController = true
			if tooManyFailures {
				s.consecutiveStartupFailures = 0
			}
		}
	} else if !s.stopping && s.playback == nil && s.current == nil {
		if s.voice != nil && !s.voice.IsClosed() && (len(s.queue) > 0 || m.autoPlay != nil) {
			s.phase = PhaseLoading
		} else if s.phase == PhaseLoading {
			s.phase = PhaseIdle
			syncController = true
		}
	}
	s.playbackStarting = false

	latestEpoch := s.sessionEpoch
	latestGen := s.playbackGeneration
	shouldContinue := currentAttempt && sourceFailed && !tooManyFailures && !s.stopping && s.current == nil && s.voice != nil && !s.voice.IsClosed() && (len(s.queue) > 0 || m.autoPlay != nil)
	if !currentAttempt && !s.stopping && s.playback == nil && s.current == nil && s.voice != nil && !s.voice.IsClosed() {
		shouldContinue = len(s.queue) > 0 || m.autoPlay != nil
	}
	s.mu.Unlock()

	m.releaseLease(preparedPath)
	if sourceFailed && m.warn != nil {
		m.warn(fmt.Errorf("skipping track %q: source is unusable", track.Title))
	} else if err != nil && !isCanceled && m.warn != nil {
		m.warn(err)
	}
	if tooManyFailures && m.warn != nil {
		m.warn(fmt.Errorf("stopped playback for guild %s: too many consecutive track startup failures", guildID))
	}
	if sourceFailed && m.recentPlays != nil && track.Title != "" {
		m.recentPlays.Add(guildID, track.Title)
		if idx := strings.Index(track.Title, " ("); idx > 0 {
			m.recentPlays.Add(guildID, strings.TrimSpace(track.Title[:idx]))
		}
	}
	if syncController {
		m.syncControllerAsync(m.workerCtx, guildID, latestEpoch, latestGen)
	}
	if shouldContinue {
		m.startNext(guildID)
	}
}

func (m *Module) commitPlaybackSession(s *session, guildID string, track Track, epoch, generation uint64, handle PlaybackHandle, leasedPath string, cancel context.CancelFunc) (committed bool, live bool) {
	s.mu.Lock()
	currentAttempt := s.sessionEpoch == epoch && s.playbackGeneration == generation && !s.stopping

	if currentAttempt && s.current != nil {
		s.playback = handle
		committed = true
		track.Title = helpers.CleanTrackTitle(track.Title)
		if s.current != nil {
			s.current.Title = track.Title
		}
		if pathProvider, ok := handle.(interface{ PlaybackPath() string }); ok && s.playbackPath == "" {
			s.playbackPath = pathProvider.PlaybackPath()
		}
		if durProvider, ok := handle.(interface{ Duration() int }); ok && durProvider.Duration() > 0 {
			probedDur := durProvider.Duration()
			track.Duration = probedDur
			if s.current != nil {
				s.current.Duration = probedDur
			}
		}
		if leasedPath == "" && s.playbackPath != "" {
			leasedPath = s.playbackPath
			m.retainLease(leasedPath)
		}
		s.playbackStarting = false
		s.consecutiveStartupFailures = 0
		s.phase = PhasePlaying
		s.trackStartTime = m.now()
		s.pausedDuration = 0
		s.pauseStartTime = time.Time{}
		s.pauseCause = PauseCauseNone
		recoveryRestart := s.restartingAfterStall
		s.restartingAfterStall = false
		if m.recentPlays != nil {
			m.recentPlays.Add(guildID, s.current.Title)
		}
		var nextTrack *Track
		if len(s.queue) > 0 {
			clone := s.queue[0].Clone()
			nextTrack = &clone
		}
		prebufferTask := s.prebuffer
		prebufferRevision := s.queueRevision
		s.mu.Unlock()

		if !recoveryRestart {
			m.resetStallCount(guildID)
		}

		slog.Info("playback stream live and committed", "guild_id", guildID, "generation", generation, "epoch", epoch, "title", track.Title, "duration", track.Duration)
		m.syncControllerAsync(m.workerCtx, guildID, epoch, generation)
		m.startControllerUpdater(guildID, epoch, generation)
		if nextTrack != nil && prebufferTask != nil && m.prebuffer != nil {
			m.startPrebuffer(prebufferTask, generation, prebufferRevision, *nextTrack)
		} else if nextTrack == nil && prebufferTask != nil && m.prebuffer != nil && m.autoPlay != nil {
			m.startAutoplayPrebuffer(guildID, epoch, generation)
		}
		if m.watchdog != nil {
			m.startWorker(func() { m.watchdog.Monitor(m.workerCtx, m, guildID, epoch, generation, handle) })
		}
		completion := &playbackCompletion{finish: func(playbackErr error) {
			m.releaseLease(leasedPath)
			slog.Info("playback stream finished", "guild_id", guildID, "generation", generation, "title", track.Title, "error", playbackErr)
			if playbackErr != nil && m.warn != nil {
				if errors.Is(playbackErr, ErrPlaybackSource) {
					m.warn(fmt.Errorf("skipping track %q: source is unusable", track.Title))
				} else {
					m.warn(fmt.Errorf("playback failed for guild %s generation %d track %q: %w", guildID, generation, track.Title, playbackErr))
				}
			}
			m.completePlayback(guildID, epoch, generation, playbackErr)
		}}
		if m.startWorker(func() {
			<-handle.Done()
			completion.complete(handle.Err())
		}) {
			live = true
		} else {
			handle.Stop()
			<-handle.Done()
			m.releaseLease(leasedPath)
		}
		return committed, live
	}

	s.playbackStarting = false
	s.playbackPath = ""
	shouldContinue := !s.stopping && s.current == nil && len(s.queue) > 0
	s.mu.Unlock()

	cancel()
	handle.Stop()
	<-handle.Done()
	m.releaseLease(leasedPath)
	if shouldContinue {
		m.startNext(guildID)
	}
	return false, false
}

func (m *Module) recoverPlaybackTransition(guildID string, epoch, generation uint64, handle PlaybackHandle, committed bool) {
	m.mu.Lock()
	s := m.sessions[guildID]
	m.mu.Unlock()
	if s == nil {
		return
	}

	s.mu.Lock()
	if (epoch != 0 && s.sessionEpoch != epoch) || (generation != 0 && s.playbackGeneration != generation) || s.stopping {
		s.mu.Unlock()
		return
	}
	s.invalidateLoadingRefreshLocked()
	if s.playback == handle || !committed {
		s.playback = nil
	}
	s.playbackCancel = nil
	s.playbackPath = ""
	s.playbackStarting = false
	if !committed && s.current != nil {
		failed := s.current.Clone()
		s.current = nil
		s.addHistoryLocked(failed)
		s.nextQueueRevisionLocked()
	}
	s.phase = PhaseIdle
	shouldContinue := s.voice != nil && !s.voice.IsClosed() && (len(s.queue) > 0 || m.autoPlay != nil)
	s.mu.Unlock()
	if shouldContinue {
		m.startNext(guildID)
	}
}

func (m *Module) completePlayback(guildID string, epoch, generation uint64, playbackErr error) {
	if errors.Is(playbackErr, ErrVoiceUnavailable) || errors.Is(playbackErr, ErrVoiceTransport) {
		m.mu.Lock()
		s := m.sessions[guildID]
		m.mu.Unlock()
		if s == nil {
			return
		}
		s.mu.Lock()
		current := !s.stopping && s.sessionEpoch == epoch && s.playbackGeneration == generation
		s.mu.Unlock()
		if !current {
			return
		}
		m.processVoiceLoss(m.workerCtx, guildID, "", nil)
		return
	}

	m.mu.Lock()
	s := m.sessions[guildID]
	m.mu.Unlock()
	if s == nil {
		return
	}
	s.mu.Lock()
	if s.sessionEpoch != epoch || s.playbackGeneration != generation || s.stopping {
		s.mu.Unlock()
		return
	}
	s.playback = nil
	s.playbackPath = ""
	replay := !errors.Is(playbackErr, ErrPlaybackPanic) && !errors.Is(playbackErr, ErrPlaybackSource)
	s.finishCurrentLocked(replay)
	s.lastActivity = m.now()
	shouldContinue := s.voice != nil && !s.voice.IsClosed() && (s.current != nil || len(s.queue) > 0 || m.autoPlay != nil)
	if shouldContinue {
		s.phase = PhaseLoading
	} else {
		s.phase = PhaseIdle
	}
	s.mu.Unlock()

	if shouldContinue {
		m.startNext(guildID)
	}
}
