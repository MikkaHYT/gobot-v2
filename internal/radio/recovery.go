package radio

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"gobot/internal/helpers"

	"github.com/bwmarrin/discordgo"
)

type InactivityConfig struct {
	EmptyChannelDelay time.Duration
}

func DefaultInactivityConfig() InactivityConfig {
	return InactivityConfig{
		EmptyChannelDelay: 60 * time.Second,
	}
}

type InactivityManager struct {
	config InactivityConfig
	timers map[string]*time.Timer
	mu     sync.Mutex
}

func NewInactivityManager(config InactivityConfig) *InactivityManager {
	if config.EmptyChannelDelay <= 0 {
		config.EmptyChannelDelay = 60 * time.Second
	}
	return &InactivityManager{
		config: config,
		timers: make(map[string]*time.Timer),
	}
}

func (m *InactivityManager) StopTimer(guildID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if t, ok := m.timers[guildID]; ok && t != nil {
		t.Stop()
		delete(m.timers, guildID)
	}
}

func (m *InactivityManager) EvaluateInactivity(s *session, humanCount int, now time.Time, onLeave func(guildID string, epoch uint64)) (pause bool, resume bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	guildID := s.guildID
	epoch := s.sessionEpoch

	if humanCount == 0 && !s.is247 {
		if s.phase == PhasePlaying && s.current != nil {
			s.phase = PhasePaused
			s.pauseCause = PauseCauseEmptyChannel
			s.pauseStartTime = now
			pause = true
		}

		m.mu.Lock()
		if _, exists := m.timers[guildID]; !exists {
			m.timers[guildID] = time.AfterFunc(m.config.EmptyChannelDelay, func() {
				m.mu.Lock()
				delete(m.timers, guildID)
				m.mu.Unlock()

				if onLeave != nil {
					onLeave(guildID, epoch)
				}
			})
		}
		m.mu.Unlock()
		return pause, false
	}

	if humanCount > 0 {
		m.StopTimer(guildID)

		if s.phase == PhasePaused && s.pauseCause == PauseCauseEmptyChannel {
			s.phase = PhasePlaying
			s.pauseCause = PauseCauseNone
			if !s.pauseStartTime.IsZero() {
				s.pausedDuration += now.Sub(s.pauseStartTime)
				s.pauseStartTime = time.Time{}
			}
			resume = true
		}
	}

	return false, resume
}

type WatchdogConfig struct {
	PollInterval         time.Duration
	StallThreshold       time.Duration
	MaxRecoveryAttempts  int
	ExhaustionLeaveDelay time.Duration
}

func DefaultWatchdogConfig() WatchdogConfig {
	return WatchdogConfig{
		PollInterval:         3 * time.Second,
		StallThreshold:       15 * time.Second,
		MaxRecoveryAttempts:  3,
		ExhaustionLeaveDelay: 3 * time.Minute,
	}
}

type PlaybackWatchdog struct {
	config WatchdogConfig
	mu     sync.Mutex
	timers map[string]*time.Timer
}

func NewPlaybackWatchdog(config WatchdogConfig) *PlaybackWatchdog {
	if config.PollInterval <= 0 {
		config.PollInterval = 3 * time.Second
	}
	if config.StallThreshold <= 0 {
		config.StallThreshold = 15 * time.Second
	}
	if config.MaxRecoveryAttempts <= 0 {
		config.MaxRecoveryAttempts = 3
	}
	if config.ExhaustionLeaveDelay <= 0 {
		config.ExhaustionLeaveDelay = 3 * time.Minute
	}
	return &PlaybackWatchdog{
		config: config,
		timers: make(map[string]*time.Timer),
	}
}

func (w *PlaybackWatchdog) StopExhaustionTimer(guildID string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if t, ok := w.timers[guildID]; ok && t != nil {
		t.Stop()
		delete(w.timers, guildID)
	}
}

func (w *PlaybackWatchdog) ScheduleExhaustionTimer(guildID string, epoch uint64, onLeave func(guildID string, epoch uint64)) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if t, ok := w.timers[guildID]; ok && t != nil {
		t.Stop()
	}
	w.timers[guildID] = time.AfterFunc(w.config.ExhaustionLeaveDelay, func() {
		w.mu.Lock()
		delete(w.timers, guildID)
		w.mu.Unlock()

		if onLeave != nil {
			onLeave(guildID, epoch)
		}
	})
}

func (w *PlaybackWatchdog) Monitor(ctx context.Context, m *Module, guildID string, epoch, generation uint64, handle PlaybackHandle) {
	if handle == nil {
		return
	}
	ticker := time.NewTicker(w.config.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-handle.Done():
			return
		case <-ticker.C:
		}

		s, err := m.getSession(ctx, guildID)
		if err != nil || s == nil {
			return
		}

		s.mu.Lock()
		if s.stopping || s.sessionEpoch != epoch || s.playbackGeneration != generation {
			s.mu.Unlock()
			return
		}
		if s.phase != PhasePlaying {
			s.mu.Unlock()
			continue
		}
		lastFrame := handle.LastFrameTime()
		now := m.now()
		s.mu.Unlock()

		if !lastFrame.IsZero() {
			if now.Sub(lastFrame) >= w.config.StallThreshold {
				m.handleWatchdogStall(guildID, epoch, generation)
				return
			}
		}
	}
}

type gatewaySetupState struct {
	endpoint  string
	token     string
	sessionID string
}

func (s *gatewaySetupState) applyServer(endpoint, token string) {
	s.endpoint = endpoint
	s.token = token
}

func (s *gatewaySetupState) applyVoiceState(sessionID string) {
	s.sessionID = sessionID
}

func (s gatewaySetupState) ready() bool {
	return s.endpoint != "" && s.token != "" && s.sessionID != ""
}

func (s gatewaySetupState) needsNudge() bool {
	return !s.ready()
}

type localSendSample struct {
	at     time.Time
	failed bool
}

const (
	gatewaySetupNudgeInterval = 2 * time.Second
	localSendWindow           = 10 * time.Second
	localSendMinimumDuration  = 5 * time.Second
	localSendMinimumFrames    = 100
	localSendErrorPercent     = 3
	localSendConsecutiveMin   = 3
)

func waitForGatewaySetup(
	ctx context.Context,
	initial gatewaySetupState,
	serverUpdates <-chan *discordgo.VoiceServerUpdate,
	stateUpdates <-chan *discordgo.VoiceStateUpdate,
	nudges <-chan time.Time,
	requestNudge func() error,
) (gatewaySetupState, error) {
	state := initial
	for {
		if state.ready() {
			return state, nil
		}
		select {
		case <-ctx.Done():
			return state, ctx.Err()
		case update, ok := <-serverUpdates:
			if !ok {
				serverUpdates = nil
				continue
			}
			if update != nil {
				state.applyServer(update.Endpoint, update.Token)
			}
		case update, ok := <-stateUpdates:
			if !ok {
				stateUpdates = nil
				continue
			}
			if update != nil && update.VoiceState != nil {
				state.applyVoiceState(update.SessionID)
			}
		case <-nudges:
			if state.needsNudge() && requestNudge != nil {
				_ = requestNudge()
			}
		}
	}
}

type localSendHealth struct {
	start       time.Time
	samples     []localSendSample
	consecutive int
}

func newLocalSendHealth(start time.Time) *localSendHealth {
	return &localSendHealth{start: start}
}

func (h *localSendHealth) record(now time.Time, err error) {
	if h == nil {
		return
	}
	if h.start.IsZero() {
		h.start = now
	}
	h.samples = append(h.samples, localSendSample{at: now, failed: err != nil})
	if err != nil {
		h.consecutive++
	} else {
		h.consecutive = 0
	}
	cutoff := now.Add(-localSendWindow)
	first := 0
	for first < len(h.samples) && h.samples[first].at.Before(cutoff) {
		first++
	}
	if first > 0 {
		h.samples = append([]localSendSample(nil), h.samples[first:]...)
		h.consecutive = 0
		for i := len(h.samples) - 1; i >= 0 && h.samples[i].failed; i-- {
			h.consecutive++
		}
		if len(h.samples) > 0 {
			h.start = h.samples[0].at
		} else {
			h.start = now
		}
	}
}

func (h *localSendHealth) unhealthy(now time.Time) bool {
	if h == nil || len(h.samples) < localSendMinimumFrames || now.Sub(h.start) < localSendMinimumDuration || h.consecutive < localSendConsecutiveMin {
		return false
	}
	failed := 0
	for _, sample := range h.samples {
		if sample.failed {
			failed++
		}
	}
	return failed*100 > len(h.samples)*localSendErrorPercent
}

type voiceReconnectEpochContextKey struct{}

func withVoiceReconnectEpoch(ctx context.Context, epoch uint64) context.Context {
	return context.WithValue(ctx, voiceReconnectEpochContextKey{}, epoch)
}

func voiceReconnectEpoch(ctx context.Context) uint64 {
	if ctx == nil {
		return 0
	}
	epoch, _ := ctx.Value(voiceReconnectEpochContextKey{}).(uint64)
	return epoch
}

func bindVoiceReconnectEpoch(ctx context.Context, s *session, epoch uint64) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	bound, cancel := context.WithCancel(ctx)
	if s == nil {
		cancel()
		return bound, cancel
	}
	s.mu.Lock()
	valid := !s.stopping && s.sessionEpoch == epoch
	epochDone := s.epochDone
	s.mu.Unlock()
	if !valid || epochDone == nil {
		if !valid {
			cancel()
		}
		return bound, cancel
	}
	helpers.Spawn(func() {
		select {
		case <-epochDone:
			cancel()
		case <-bound.Done():
		}
	})
	return bound, cancel
}

var gatewayDiagnosticState = struct {
	sync.Mutex
	lastGlobal time.Time
	lastGuild  map[string]time.Time
}{lastGuild: make(map[string]time.Time)}

func logGatewaySetupDiagnostic(guildID, channelID string, epoch uint64, started time.Time, state gatewaySetupState, gatewayState string) {
	now := time.Now()
	gatewayDiagnosticState.Lock()
	if now.Sub(gatewayDiagnosticState.lastGlobal) < time.Second || now.Sub(gatewayDiagnosticState.lastGuild[guildID]) < 10*time.Second {
		gatewayDiagnosticState.Unlock()
		return
	}
	gatewayDiagnosticState.lastGlobal = now
	gatewayDiagnosticState.lastGuild[guildID] = now
	gatewayDiagnosticState.Unlock()

	slog.Warn("RADIO_VOICE_RECONNECT",
		"guild_id", guildID,
		"epoch", epoch,
		"target_channel", channelID,
		"elapsed_ms", now.Sub(started).Milliseconds(),
		"endpoint_arrived", state.endpoint != "",
		"token_arrived", state.token != "",
		"session_id_arrived", state.sessionID != "",
		"gateway_state", gatewayState,
	)
}

func (m *Module) resetStallCount(guildID string) {
	m.stallMu.Lock()
	delete(m.stallCounts, guildID)
	m.stallMu.Unlock()
}

func (m *Module) HandleVoiceState(ctx context.Context, event VoiceStateEvent) {
	if ctx == nil {
		ctx = m.ctx
	}
	if strings.TrimSpace(event.GuildID) == "" {
		return
	}
	m.mu.Lock()
	s := m.sessions[event.GuildID]
	m.mu.Unlock()
	if s == nil {
		return
	}
	s.effectMu.Lock()
	defer s.effectMu.Unlock()

	if event.BotUserID != "" && event.UserID == event.BotUserID {
		if event.ChannelID == "" {
			s.mu.Lock()
			recoverVoice := !s.stopping && !s.voiceTransition && s.phase != PhaseRecovering && s.phase != PhaseStopping && s.voice != nil && s.voiceChannelID != ""
			s.mu.Unlock()
			if recoverVoice {
				m.startWorker(func() { m.HandleVoiceLoss(ctx, event.GuildID, "") })
			}
			return
		}
		s.mu.Lock()
		if s.voiceTransition {
			s.mu.Unlock()
			return
		}
		if s.voice != nil && s.voiceChannelID != "" && s.voiceChannelID != event.ChannelID {
			if s.externalMoveInFlight {
				s.mu.Unlock()
				return
			}
			oldConnection := s.voice
			oldPlayback := s.playback
			oldCancel := s.playbackCancel
			targetChannel := event.ChannelID
			epoch := s.nextSessionEpochLocked()
			s.nextPlaybackGenerationLocked()
			s.externalMoveInFlight = true
			s.externalMoveTarget = targetChannel
			s.voiceTransition = true
			s.voice = nil
			s.voiceChannelID = targetChannel
			s.playback = nil
			s.playbackPath = ""
			s.playbackCancel = nil
			if s.current != nil {
				s.queue = append([]Track{s.current.Clone()}, s.queue...)
				s.current = nil
				s.nextQueueRevisionLocked()
			}
			s.phase = PhaseConnecting
			s.mu.Unlock()

			if oldConnection != nil {
				oldConnection.Close()
			}
			if oldPlayback != nil {
				oldPlayback.Stop()
			}
			if oldCancel != nil {
				oldCancel()
			}
			if !m.startWorker(func() {
				s.voiceJoinMu.Lock()
				joinCtx, cancelJoin := bindVoiceReconnectEpoch(m.workerCtx, s, epoch)
				conn, errJoin := m.voice.Join(withVoiceReconnectEpoch(joinCtx, epoch), event.GuildID, targetChannel)
				cancelJoin()
				s.voiceJoinMu.Unlock()
				s.mu.Lock()
				valid := !s.stopping && s.sessionEpoch == epoch && s.externalMoveInFlight && s.externalMoveTarget == targetChannel
				if !valid {
					s.mu.Unlock()
					if conn != nil {
						conn.Close()
					}
					return
				}
				if errJoin != nil || conn == nil {
					s.externalMoveInFlight = false
					s.externalMoveTarget = ""
					s.voiceTransition = false
					s.voiceChannelID = ""
					s.phase = PhaseIdle
					s.mu.Unlock()
					if conn != nil {
						conn.Close()
					}
					if m.warn != nil && errJoin != nil {
						m.warn(fmt.Errorf("external voice move failed for guild %s: %w", event.GuildID, errJoin))
					}
					return
				}
				s.voice = conn
				s.externalMoveInFlight = false
				s.externalMoveTarget = ""
				s.voiceTransition = false
				s.phase = PhaseLoading
				s.mu.Unlock()
				m.startNext(event.GuildID)
			}) {
				s.mu.Lock()
				if s.sessionEpoch == epoch && s.externalMoveInFlight {
					s.externalMoveInFlight = false
					s.externalMoveTarget = ""
					s.voiceTransition = false
					s.voiceChannelID = ""
					s.phase = PhaseIdle
				}
				s.mu.Unlock()
			}
			return
		}
		if s.phase == PhaseRecovering && s.voiceChannelID != event.ChannelID {
			s.nextSessionEpochLocked()
			s.phase = PhaseIdle
			s.autoplayToken++
			s.playbackStarting = false
		}
		s.voiceChannelID = event.ChannelID
		s.mu.Unlock()
	}

	if m.inactivity != nil && event.HumanCountSet {
		pause, resume := m.inactivity.EvaluateInactivity(s, event.HumanCount, m.now(), func(guildID string, epoch uint64) {
			m.startWorker(func() { m.handleInactivityTimeout(guildID, epoch) })
		})
		if pause {
			s.mu.Lock()
			if s.playback != nil {
				s.playback.Pause()
			}
			epoch := s.sessionEpoch
			generation := s.playbackGeneration
			s.mu.Unlock()
			m.syncControllerAsync(ctx, event.GuildID, epoch, generation)
		}
		if resume {
			s.mu.Lock()
			if s.playback != nil {
				s.playback.Resume()
			}
			epoch := s.sessionEpoch
			generation := s.playbackGeneration
			s.mu.Unlock()
			m.syncControllerAsync(ctx, event.GuildID, epoch, generation)
		}
	}
}

func (m *Module) HandleVoiceLoss(ctx context.Context, guildID, channelID string) {
	m.processVoiceLoss(ctx, guildID, channelID, nil)
}

func (m *Module) HandleVoiceConnectionLoss(ctx context.Context, guildID, channelID string, connection VoiceConnection) {
	m.processVoiceLoss(ctx, guildID, channelID, connection)
}

func (m *Module) processVoiceLoss(ctx context.Context, guildID, channelID string, lostConnection VoiceConnection) {
	if ctx == nil {
		ctx = m.workerCtx
	}
	if strings.TrimSpace(guildID) == "" {
		return
	}
	s, err := m.getSession(ctx, guildID)
	if err != nil || s == nil {
		return
	}

	s.connectMu.Lock()
	defer s.connectMu.Unlock()
	s.effectMu.Lock()
	defer s.effectMu.Unlock()

	s.mu.Lock()
	if s.stopping {
		s.mu.Unlock()
		return
	}
	if s.phase == PhaseRecovering {
		s.mu.Unlock()
		return
	}
	if lostConnection != nil && s.voice != lostConnection {
		s.mu.Unlock()
		return
	}
	epoch := s.nextSessionEpochLocked()
	s.phase = PhaseRecovering
	generation := s.playbackGeneration
	refreshController := m.controller != nil && s.textChannelID != ""
	connection := s.voice
	playback := s.playback
	cancelPlayback := s.playbackCancel
	s.voice = nil
	s.playback = nil
	s.playbackPath = ""
	s.playbackCancel = nil
	if s.current != nil {
		s.queue = append([]Track{s.current.Clone()}, s.queue...)
		s.current = nil
		s.nextQueueRevisionLocked()
	}
	targetChannel := s.voiceChannelID
	if targetChannel == "" {
		targetChannel = channelID
	}
	if targetChannel == "" {
		targetChannel = s.autojoinChannelID
	}
	hasWork := len(s.queue) > 0 || s.is247
	s.mu.Unlock()
	if refreshController {
		m.syncControllerAsync(ctx, guildID, epoch, generation)
	}

	if connection != nil {
		connection.Close()
	}
	if playback != nil {
		playback.Stop()
	}
	if cancelPlayback != nil {
		cancelPlayback()
	}

	if !hasWork || targetChannel == "" || m.voice == nil {
		s.mu.Lock()
		stopped := false
		var finalEpoch, finalGen uint64
		if s.sessionEpoch == epoch && !s.stopping {
			s.beginStoppingLocked()
			s.finishStoppingLocked(m.now())
			stopped = true
			finalEpoch = s.sessionEpoch
			finalGen = s.playbackGeneration
		}
		s.mu.Unlock()
		if stopped && m.controller != nil {
			m.syncControllerAsync(ctx, guildID, finalEpoch, finalGen)
		}
		return
	}

	if !m.startWorker(func() {
		waitTimer := time.NewTimer(2 * time.Second)
		select {
		case <-m.workerCtx.Done():
			waitTimer.Stop()
			return
		case <-waitTimer.C:
		}

		s.mu.Lock()
		if s.stopping || s.sessionEpoch != epoch || s.phase != PhaseRecovering {
			s.mu.Unlock()
			return
		}
		s.voiceTransition = true
		s.mu.Unlock()
		defer func() {
			s.mu.Lock()
			s.voiceTransition = false
			s.mu.Unlock()
		}()

		s.voiceJoinMu.Lock()
		joinCtx, cancelJoin := bindVoiceReconnectEpoch(m.workerCtx, s, epoch)
		conn, errJoin := m.voice.Join(withVoiceReconnectEpoch(joinCtx, epoch), guildID, targetChannel)
		cancelJoin()
		s.voiceJoinMu.Unlock()

		s.mu.Lock()
		if s.stopping || s.sessionEpoch != epoch || s.phase != PhaseRecovering {
			s.mu.Unlock()
			if conn != nil {
				conn.Close()
			}
			return
		}
		if errJoin != nil || conn == nil {
			s.beginStoppingLocked()
			s.finishStoppingLocked(m.now())
			finalEpoch := s.sessionEpoch
			finalGen := s.playbackGeneration
			s.mu.Unlock()
			if conn != nil {
				conn.Close()
			}
			if m.voice != nil {
				_ = m.voice.Disconnect(m.workerCtx, guildID)
			}
			if m.controller != nil {
				m.syncControllerAsync(m.workerCtx, guildID, finalEpoch, finalGen)
			}
			if m.warn != nil && errJoin != nil {
				m.warn(fmt.Errorf("voice loss reconnect failed for guild %s: %w", guildID, errJoin))
			}
			return
		}

		s.voice = conn
		s.voiceChannelID = targetChannel
		if s.textChannelID == "" {
			s.textChannelID = targetChannel
		}
		s.phase = PhaseLoading
		s.mu.Unlock()

		m.startNext(guildID)
	}) {
		return
	}
}

func (m *Module) handleWatchdogStall(guildID string, epoch, generation uint64) {
	s, err := m.getSession(m.workerCtx, guildID)
	if err != nil || s == nil {
		return
	}
	s.mu.Lock()
	if s.stopping || s.sessionEpoch != epoch || s.playbackGeneration != generation {
		s.mu.Unlock()
		return
	}

	m.stallMu.Lock()
	attempts := m.stallCounts[guildID] + 1
	m.stallCounts[guildID] = attempts
	m.stallMu.Unlock()

	if attempts <= m.watchdog.config.MaxRecoveryAttempts {
		playback := s.playback
		cancelPlayback := s.playbackCancel
		s.nextPlaybackGenerationLocked()
		s.playback = nil
		s.playbackPath = ""
		s.playbackCancel = nil
		if s.current != nil {
			s.queue = append([]Track{s.current.Clone()}, s.queue...)
			s.current = nil
			s.nextQueueRevisionLocked()
		}
		s.restartingAfterStall = true
		s.phase = PhaseLoading
		s.mu.Unlock()
		if playback != nil {
			playback.Stop()
		}
		if cancelPlayback != nil {
			cancelPlayback()
		}
		if m.warn != nil {
			m.warn(fmt.Errorf("watchdog detected stall in guild %s (attempt %d/%d), restarting track", guildID, attempts, m.watchdog.config.MaxRecoveryAttempts))
		}
		m.startNext(guildID)
		return
	}

	s.beginStoppingLocked()
	exhaustionEpoch := s.sessionEpoch
	connection := s.voice
	playback := s.playback
	cancelPlayback := s.playbackCancel
	s.voice = nil
	s.playback = nil
	s.playbackPath = ""
	s.playbackCancel = nil
	s.mu.Unlock()

	if connection != nil {
		connection.Close()
	}
	if playback != nil {
		playback.Stop()
	}
	if cancelPlayback != nil {
		cancelPlayback()
	}

	if m.warn != nil {
		m.warn(fmt.Errorf("watchdog recovery exhausted for guild %s after %d attempts, scheduling delayed leave", guildID, m.watchdog.config.MaxRecoveryAttempts))
	}

	m.watchdog.ScheduleExhaustionTimer(guildID, exhaustionEpoch, func(gID string, ep uint64) {
		m.startWorker(func() { m.handleWatchdogExhaustionCleanup(gID, ep) })
	})
}

func (m *Module) handleWatchdogExhaustionCleanup(guildID string, expectedEpoch uint64) {
	s := m.existingSession(guildID)
	if s == nil {
		return
	}

	s.voiceJoinMu.Lock()
	defer s.voiceJoinMu.Unlock()
	s.effectMu.Lock()
	defer s.effectMu.Unlock()

	s.mu.Lock()
	if s.sessionEpoch != expectedEpoch {
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()

	if m.voice != nil {
		_ = m.voice.Disconnect(m.ctx, guildID)
	}

	s.mu.Lock()
	if s.sessionEpoch == expectedEpoch {
		s.finishStoppingLocked(m.now())
	}
	s.mu.Unlock()
}

func (m *Module) handleInactivityTimeout(guildID string, epoch uint64) {
	m.mu.Lock()
	s := m.sessions[guildID]
	m.mu.Unlock()
	if s == nil {
		return
	}

	s.effectMu.Lock()
	defer s.effectMu.Unlock()
	s.mu.Lock()
	if s.sessionEpoch != epoch || s.stopping || s.is247 {
		s.mu.Unlock()
		return
	}
	connection := s.voice
	playback := s.playback
	cancelPlayback := s.playbackCancel
	s.playback = nil
	s.playbackPath = ""
	s.playbackCancel = nil
	s.beginStoppingLocked()
	s.finishStoppingLocked(m.now())
	s.mu.Unlock()

	if connection != nil {
		connection.Close()
	}
	if playback != nil {
		playback.Stop()
	}
	if cancelPlayback != nil {
		cancelPlayback()
	}
	if connection != nil && m.voice != nil {
		_ = m.voice.Disconnect(m.ctx, guildID)
	}
}

func (m *Module) HandleChannelDeleted(ctx context.Context, event ChannelDeleted) {
	if ctx == nil {
		ctx = m.ctx
	}
	if strings.TrimSpace(event.GuildID) == "" || strings.TrimSpace(event.ChannelID) == "" {
		return
	}
	s := m.existingSession(event.GuildID)
	if s == nil {
		return
	}
	s.effectMu.Lock()
	defer s.effectMu.Unlock()
	if err := m.lockSettingsForUpdate(ctx, s); err != nil {
		return
	}
	s.mu.Lock()
	var connection VoiceConnection
	var playback PlaybackHandle
	var cancelPlayback context.CancelFunc
	if s.voiceChannelID == event.ChannelID {
		connection = s.voice
		playback = s.playback
		cancelPlayback = s.playbackCancel
		s.playback = nil
		s.playbackPath = ""
		s.playbackCancel = nil
		s.beginStoppingLocked()
		s.finishStoppingLocked(m.now())
	}
	textDeleted := s.textChannelID == event.ChannelID
	controllerDeleted := s.controllerChannelID == event.ChannelID
	switchedToVoiceText := false
	if textDeleted || controllerDeleted {
		s.removePendingForChannelLocked(event.ChannelID)
		s.controllerChannelID = ""
		s.controllerMessageID = ""
		if s.voiceChannelID != "" && s.voiceChannelID != event.ChannelID {
			s.textChannelID = s.voiceChannelID
			switchedToVoiceText = true
		} else if textDeleted {
			s.textChannelID = ""
		}
	}
	autojoinDeleted := false
	var deletedAutojoinChannel string
	if s.autojoinChannelID == event.ChannelID {
		deletedAutojoinChannel = s.autojoinChannelID
		s.autojoinChannelID = ""
		autojoinDeleted = true
	}
	hasControllerWork := len(s.pendingControllerCleanup) > 0
	restrictionMode := s.restrictionMode
	controllerWorkForDrain := hasControllerWork
	voiceWorkForDrain := connection != nil
	shouldDrainController := (controllerWorkForDrain || voiceWorkForDrain) && m.controller != nil
	shouldSyncController := switchedToVoiceText &&
		!s.stopping &&
		s.current != nil &&
		(s.phase == PhasePlaying || s.phase == PhasePaused) &&
		m.controller != nil
	epoch := s.sessionEpoch
	generation := s.playbackGeneration
	s.mu.Unlock()

	if autojoinDeleted && m.settings != nil {
		if err := m.settings.SaveRadioSettings(ctx, event.GuildID, RadioSettings{
			RestrictionMode:      restrictionMode,
			AutojoinChannelID:    "",
			AutojoinChannelIDSet: true,
		}); err != nil {
			s.mu.Lock()
			if s.autojoinChannelID == "" {
				s.autojoinChannelID = deletedAutojoinChannel
			}
			s.mu.Unlock()
			if m.warn != nil {
				m.warn(fmt.Errorf("failed to persist cleared autojoin channel %s for guild %s: %w", event.ChannelID, event.GuildID, err))
			}
		}
	}
	s.settingsMu.Unlock()

	if connection != nil {
		connection.Close()
	}
	if playback != nil {
		playback.Stop()
	}
	if cancelPlayback != nil {
		cancelPlayback()
	}
	if connection != nil && m.voice != nil {
		_ = m.voice.Disconnect(ctx, event.GuildID)
	}
	if shouldDrainController {
		s.controllerMu.Lock()
		_, _ = m.drainSessionControllersLocked(ctx, s, m.controller, false)
		s.controllerMu.Unlock()
	}
	if shouldSyncController {
		m.syncControllerAsync(ctx, event.GuildID, epoch, generation)
	}
}
