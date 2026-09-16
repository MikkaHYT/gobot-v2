package radio

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
)

const (
	controllerBackoffBase = time.Second
	controllerBackoffMax  = time.Minute
)

var controllerRetryAfterPattern = regexp.MustCompile(`(?i)retry[-_ ]?after\s*"?\s*[:=]?\s*"?([0-9]+(?:\.[0-9]+)?)`)

var (
	ErrControllerRateLimited          = errors.New("controller writes are temporarily backed off")
	ErrControllerPermissionSuppressed = errors.New("controller writes are suppressed for this channel")
)

type ControllerRef struct {
	GuildID            string
	ChannelID          string
	MessageID          string
	SessionEpoch       uint64
	PlaybackGeneration uint64
}

type controllerKey struct {
	channelID string
	messageID string
}

func (r ControllerRef) Valid() bool {
	return r.GuildID != "" && r.ChannelID != "" && r.MessageID != ""
}

type ControllerPort interface {
	CreateOrUpdate(ctx context.Context, ref ControllerRef, snap Snapshot) (string, error)
	Delete(ctx context.Context, ref ControllerRef) error
	CheckControllerPermissions(ctx context.Context, ref ControllerRef) error
}

func IsDiscordNotFound(err error) bool {
	if err == nil {
		return false
	}
	errStr := strings.ToLower(err.Error())
	return strings.Contains(errStr, "10008") ||
		strings.Contains(errStr, "10003") ||
		strings.Contains(errStr, "unknown message") ||
		strings.Contains(errStr, "unknown channel") ||
		strings.Contains(errStr, "404")
}

func IsDiscordPermissionError(err error) bool {
	if err == nil {
		return false
	}
	errStr := strings.ToLower(err.Error())
	return strings.Contains(errStr, "50013") ||
		strings.Contains(errStr, "50001") ||
		strings.Contains(errStr, "missing permissions") ||
		strings.Contains(errStr, "missing access")
}

func controllerRateLimitRetryAfter(err error) (time.Duration, bool) {
	if err == nil {
		return 0, false
	}

	var rateLimitErr *discordgo.RateLimitError
	if errors.As(err, &rateLimitErr) && rateLimitErr != nil && rateLimitErr.RateLimit != nil && rateLimitErr.TooManyRequests != nil {
		return rateLimitErr.RetryAfter, true
	}
	var rateLimitValue discordgo.RateLimitError
	if errors.As(err, &rateLimitValue) && rateLimitValue.RateLimit != nil && rateLimitValue.TooManyRequests != nil {
		return rateLimitValue.RetryAfter, true
	}

	errStr := strings.ToLower(err.Error())
	if !strings.Contains(errStr, "429") &&
		!strings.Contains(errStr, "rate limit") &&
		!strings.Contains(errStr, "rate-limit") &&
		!strings.Contains(errStr, "too many requests") {
		return 0, false
	}

	match := controllerRetryAfterPattern.FindStringSubmatch(err.Error())
	if len(match) != 2 {
		return 0, true
	}
	seconds, parseErr := strconv.ParseFloat(match[1], 64)
	if parseErr != nil || seconds <= 0 {
		return 0, true
	}
	return time.Duration(seconds * float64(time.Second)), true
}

func controllerFallbackBackoff(failures int) time.Duration {
	backoff := controllerBackoffBase
	for i := 1; i < failures && backoff < controllerBackoffMax; i++ {
		backoff *= 2
	}
	if backoff > controllerBackoffMax {
		return controllerBackoffMax
	}
	return backoff
}

func (s *session) controllerWriteBlockLocked(channelID string, now time.Time) error {
	if s.controllerBackoffChannel != "" && s.controllerBackoffChannel != channelID {
		s.controllerBackoffUntil = time.Time{}
		s.controllerBackoffChannel = ""
		s.controllerBackoffFailures = 0
	}
	if s.controllerSuppressedChannel != "" && s.controllerSuppressedChannel != channelID {
		s.controllerSuppressedChannel = ""
	}
	if s.controllerSuppressedChannel == channelID {
		return ErrControllerPermissionSuppressed
	}
	if !s.controllerBackoffUntil.IsZero() && now.Before(s.controllerBackoffUntil) {
		return ErrControllerRateLimited
	}
	if !s.controllerBackoffUntil.IsZero() {
		s.controllerBackoffUntil = time.Time{}
	}
	return nil
}

func (s *session) recordControllerWriteErrorLocked(channelID string, err error, now time.Time) bool {
	if retryAfter, rateLimited := controllerRateLimitRetryAfter(err); rateLimited {
		if s.controllerBackoffChannel != channelID {
			s.controllerBackoffChannel = channelID
			s.controllerBackoffFailures = 0
		}
		s.controllerBackoffFailures++
		backoff := controllerFallbackBackoff(s.controllerBackoffFailures)
		if retryAfter > 0 {
			backoff = retryAfter
		}
		if backoff > controllerBackoffMax {
			backoff = controllerBackoffMax
		}
		s.controllerBackoffUntil = now.Add(backoff)
		return true
	}
	if IsDiscordPermissionError(err) {
		if s.controllerSuppressedChannel == channelID {
			return false
		}
		s.controllerSuppressedChannel = channelID
	}
	return true
}

func (s *session) controllerWriteSucceededLocked(channelID string) {
	if s.controllerBackoffChannel != channelID {
		return
	}
	s.controllerBackoffUntil = time.Time{}
	s.controllerBackoffChannel = ""
	s.controllerBackoffFailures = 0
}

func (m *Module) RecordControllerPermissionCheck(guildID, channelID string, checkErr error) {
	if checkErr != nil || guildID == "" || channelID == "" {
		return
	}
	m.mu.Lock()
	s := m.sessions[guildID]
	m.mu.Unlock()
	if s == nil {
		return
	}
	s.controllerMu.Lock()
	defer s.controllerMu.Unlock()
	s.mu.Lock()
	if s.controllerSuppressedChannel == channelID {
		s.controllerSuppressedChannel = ""
	}
	s.mu.Unlock()
}

func (s *session) retainControllerLocked() {
	if s.controllerMessageID == "" || s.controllerChannelID == "" {
		return
	}
	if s.pendingControllerCleanup == nil {
		s.pendingControllerCleanup = make(map[controllerKey]ControllerRef)
	}
	ref := ControllerRef{
		GuildID:            s.guildID,
		ChannelID:          s.controllerChannelID,
		MessageID:          s.controllerMessageID,
		SessionEpoch:       s.sessionEpoch,
		PlaybackGeneration: s.playbackGeneration,
	}
	if ref.Valid() {
		s.pendingControllerCleanup[controllerKey{channelID: ref.ChannelID, messageID: ref.MessageID}] = ref
	}
	s.controllerChannelID = ""
	s.controllerMessageID = ""
	s.controllerCleanup = len(s.pendingControllerCleanup) > 0
}

func (s *session) addPendingCleanupLocked(ref ControllerRef) {
	if !ref.Valid() {
		return
	}
	if s.pendingControllerCleanup == nil {
		s.pendingControllerCleanup = make(map[controllerKey]ControllerRef)
	}
	s.pendingControllerCleanup[controllerKey{channelID: ref.ChannelID, messageID: ref.MessageID}] = ref
	s.controllerCleanup = true
}

func (s *session) pendingControllersLocked() []ControllerRef {
	if len(s.pendingControllerCleanup) == 0 {
		return nil
	}
	refs := make([]ControllerRef, 0, len(s.pendingControllerCleanup))
	for _, ref := range s.pendingControllerCleanup {
		refs = append(refs, ref)
	}
	return refs
}

func (s *session) completeControllerCleanupLocked(ref ControllerRef) {
	delete(s.pendingControllerCleanup, controllerKey{channelID: ref.ChannelID, messageID: ref.MessageID})
	s.controllerCleanup = len(s.pendingControllerCleanup) > 0
}

func (s *session) removePendingForChannelLocked(channelID string) {
	for k, ref := range s.pendingControllerCleanup {
		if ref.ChannelID == channelID || k.channelID == channelID {
			delete(s.pendingControllerCleanup, k)
		}
	}
	s.controllerCleanup = len(s.pendingControllerCleanup) > 0
}

func (m *Module) syncControllerAsync(ctx context.Context, guildID string, epoch, generation uint64) {
	if m.controller == nil {
		return
	}
	m.startWorker(func() {
		m.syncController(ctx, guildID, epoch, generation)
	})
}

func (m *Module) syncController(ctx context.Context, guildID string, epoch, generation uint64) {
	if m.controller == nil {
		return
	}
	s, err := m.getSession(ctx, guildID)
	if err != nil || s == nil {
		return
	}

	s.controllerMu.Lock()
	defer s.controllerMu.Unlock()

	s.mu.Lock()
	if s.playerChannelID != "" {
		s.textChannelID = s.playerChannelID
	}
	if s.stopping || s.sessionEpoch != epoch || s.textChannelID == "" {
		s.mu.Unlock()
		return
	}
	currentGen := s.playbackGeneration
	snap := s.snapshotLocked()
	oldRef := ControllerRef{
		GuildID:            guildID,
		ChannelID:          s.controllerChannelID,
		MessageID:          s.controllerMessageID,
		SessionEpoch:       epoch,
		PlaybackGeneration: currentGen,
	}
	ref := ControllerRef{
		GuildID:            guildID,
		ChannelID:          s.textChannelID,
		MessageID:          s.controllerMessageID,
		SessionEpoch:       epoch,
		PlaybackGeneration: currentGen,
	}
	if oldRef.Valid() && oldRef.ChannelID != ref.ChannelID {
		ref.MessageID = ""
	}
	if blockErr := s.controllerWriteBlockLocked(ref.ChannelID, m.now()); blockErr != nil {
		s.mu.Unlock()
		if errors.Is(blockErr, ErrControllerPermissionSuppressed) {
			if m.controller != nil {
				if checkErr := m.controller.CheckControllerPermissions(ctx, ref); checkErr == nil && ctx.Err() == nil {
					s.mu.Lock()
					if !s.stopping && s.sessionEpoch == epoch && s.textChannelID == ref.ChannelID && s.controllerSuppressedChannel == ref.ChannelID {
						s.controllerSuppressedChannel = ""
					}
					s.mu.Unlock()
				} else if checkErr != nil && IsDiscordNotFound(checkErr) {
					s.mu.Lock()
					if !s.stopping && s.sessionEpoch == epoch && s.textChannelID == ref.ChannelID && s.voiceChannelID != "" && s.voiceChannelID != ref.ChannelID {
						s.removePendingForChannelLocked(ref.ChannelID)
						if s.controllerChannelID == ref.ChannelID {
							s.controllerChannelID = ""
							s.controllerMessageID = ""
						}
						s.controllerSuppressedChannel = ""
						s.textChannelID = s.voiceChannelID
						resyncEpoch := s.sessionEpoch
						resyncGen := s.playbackGeneration
						s.mu.Unlock()
						m.syncControllerAsync(ctx, guildID, resyncEpoch, resyncGen)
						return
					}
					s.mu.Unlock()
				}
			}
		}
		return
	}
	s.mu.Unlock()

	newMsgID, errCreate := m.controller.CreateOrUpdate(ctx, ref, snap)

	s.mu.Lock()
	if errCreate != nil {
		if ctx.Err() != nil {
			s.mu.Unlock()
			return
		}
		if !s.stopping && s.sessionEpoch == epoch && s.textChannelID == ref.ChannelID {
			if IsDiscordNotFound(errCreate) && s.voiceChannelID != "" && s.voiceChannelID != ref.ChannelID {
				s.removePendingForChannelLocked(ref.ChannelID)
				if s.controllerChannelID == ref.ChannelID {
					s.controllerChannelID = ""
					s.controllerMessageID = ""
				}
				s.controllerSuppressedChannel = ""
				s.textChannelID = s.voiceChannelID
				resyncEpoch := s.sessionEpoch
				resyncGen := s.playbackGeneration
				s.mu.Unlock()
				m.syncControllerAsync(ctx, guildID, resyncEpoch, resyncGen)
				return
			}
			shouldWarn := s.recordControllerWriteErrorLocked(ref.ChannelID, errCreate, m.now())
			s.mu.Unlock()
			if shouldWarn && m.warn != nil {
				m.warn(errCreate)
			}
			return
		}
		s.mu.Unlock()
		return
	}
	s.controllerWriteSucceededLocked(ref.ChannelID)

	current := !s.stopping && s.sessionEpoch == epoch && s.textChannelID == ref.ChannelID
	if current {
		if s.playbackGeneration == generation {
			if newMsgID != "" && newMsgID != ref.MessageID {
				if oldRef.Valid() {
					s.addPendingCleanupLocked(oldRef)
				}
				s.controllerMessageID = newMsgID
				s.controllerChannelID = ref.ChannelID
			} else if newMsgID != "" && s.controllerMessageID == "" {
				s.controllerMessageID = newMsgID
				s.controllerChannelID = ref.ChannelID
			}
			s.mu.Unlock()
			_, _ = m.drainSessionControllersLocked(m.workerCtx, s, m.controller, false)
			return
		}

		if newMsgID == ref.MessageID {
			if s.controllerMessageID == "" {
				s.controllerMessageID = newMsgID
				s.controllerChannelID = ref.ChannelID
			}
			s.mu.Unlock()
			_, _ = m.drainSessionControllersLocked(m.workerCtx, s, m.controller, false)
			return
		}

		if newMsgID != "" {
			if s.controllerMessageID == "" {
				s.controllerMessageID = newMsgID
				s.controllerChannelID = ref.ChannelID
			} else {
				s.addPendingCleanupLocked(ControllerRef{
					GuildID:            guildID,
					ChannelID:          ref.ChannelID,
					MessageID:          newMsgID,
					SessionEpoch:       epoch,
					PlaybackGeneration: generation,
				})
			}
		}
		s.mu.Unlock()
		_, _ = m.drainSessionControllersLocked(m.workerCtx, s, m.controller, false)
		return
	}

	if newMsgID == "" {
		s.mu.Unlock()
		return
	}
	s.addPendingCleanupLocked(ControllerRef{
		GuildID:            guildID,
		ChannelID:          ref.ChannelID,
		MessageID:          newMsgID,
		SessionEpoch:       epoch,
		PlaybackGeneration: generation,
	})
	s.mu.Unlock()
	_, _ = m.drainSessionControllersLocked(m.workerCtx, s, m.controller, false)
}

func (m *Module) startControllerUpdater(guildID string, epoch, generation uint64) {
	if m.controller == nil || m.controllerRefreshInterval <= 0 {
		return
	}
	m.startWorker(func() {
		ticker := time.NewTicker(m.controllerRefreshInterval)
		defer ticker.Stop()
		for {
			select {
			case <-m.workerCtx.Done():
				return
			case <-ticker.C:
			}
			m.mu.Lock()
			s := m.sessions[guildID]
			m.mu.Unlock()
			if s == nil {
				return
			}
			s.mu.Lock()
			active := !s.stopping && s.sessionEpoch == epoch && s.playbackGeneration == generation && s.phase == PhasePlaying && s.current != nil
			if active && s.controllerView == ControllerViewQueue {
				if time.Since(s.controllerViewActivity) > 90*time.Second {
					s.controllerView = ControllerViewPlayer
					s.controllerRevision++
				} else {
					s.mu.Unlock()
					continue
				}
			}
			s.mu.Unlock()
			if !active {
				return
			}
			m.syncController(m.workerCtx, guildID, epoch, generation)
		}
	})
}

func (m *Module) ShowController(ctx context.Context, guildID, channelID string) (Snapshot, error) {
	if ctx == nil {
		ctx = m.ctx
	}
	if m.controller == nil || strings.TrimSpace(channelID) == "" {
		return Snapshot{}, ErrMissingContext
	}
	s, err := m.getSession(ctx, guildID)
	if err != nil {
		return Snapshot{}, err
	}
	s.controllerMu.Lock()
	defer s.controllerMu.Unlock()

	s.mu.Lock()
	if s.stopping || s.current == nil {
		s.mu.Unlock()
		return Snapshot{}, ErrInvalidPhase
	}
	s.lastActivity = m.now()
	epoch := s.sessionEpoch
	generation := s.playbackGeneration
	snap := s.snapshotLocked()
	oldRef := ControllerRef{
		GuildID:            guildID,
		ChannelID:          s.controllerChannelID,
		MessageID:          s.controllerMessageID,
		SessionEpoch:       epoch,
		PlaybackGeneration: generation,
	}
	if blockErr := s.controllerWriteBlockLocked(channelID, m.now()); blockErr != nil {
		s.mu.Unlock()
		return Snapshot{}, blockErr
	}
	s.mu.Unlock()

	newID, err := m.controller.CreateOrUpdate(ctx, ControllerRef{
		GuildID:            guildID,
		ChannelID:          channelID,
		SessionEpoch:       epoch,
		PlaybackGeneration: generation,
	}, snap)
	if err != nil {
		s.mu.Lock()
		if !s.stopping && s.sessionEpoch == epoch && s.textChannelID == channelID {
			s.recordControllerWriteErrorLocked(channelID, err, m.now())
		}
		s.mu.Unlock()
		return Snapshot{}, err
	}
	if newID == "" {
		return Snapshot{}, fmt.Errorf("controller creation returned an empty message ID")
	}

	s.mu.Lock()
	s.controllerWriteSucceededLocked(channelID)
	current := !s.stopping && s.sessionEpoch == epoch
	newRef := ControllerRef{
		GuildID:            guildID,
		ChannelID:          channelID,
		MessageID:          newID,
		SessionEpoch:       epoch,
		PlaybackGeneration: s.playbackGeneration,
	}
	needsResync := false
	latestGen := s.playbackGeneration
	if current {
		if oldRef.Valid() && (oldRef.ChannelID != channelID || oldRef.MessageID != newID) {
			s.addPendingCleanupLocked(oldRef)
		}
		s.textChannelID = channelID
		s.controllerChannelID = channelID
		s.controllerMessageID = newID
		needsResync = s.playbackGeneration != generation
		snap = s.snapshotLocked()
	} else {
		s.addPendingCleanupLocked(newRef)
	}
	s.mu.Unlock()
	_, cleanupErrs := m.drainSessionControllersLocked(ctx, s, m.controller, false)
	if !current {
		return Snapshot{}, ErrStaleOperation
	}
	if needsResync {
		m.syncControllerAsync(m.workerCtx, guildID, epoch, latestGen)
	}
	if len(cleanupErrs) > 0 {
		return snap, errors.Join(cleanupErrs...)
	}
	return snap, nil
}

func (m *Module) drainPendingControllers(ctx context.Context, port ControllerPort, reportErrors bool) error {
	if port == nil {
		return nil
	}

	var allRefs []ControllerRef
	m.mu.Lock()
	for _, s := range m.sessions {
		s.mu.Lock()
		allRefs = append(allRefs, s.pendingControllersLocked()...)
		s.mu.Unlock()
	}
	m.mu.Unlock()

	var errs []error
	for _, s := range m.sessionsForRefs(allRefs) {
		s.controllerMu.Lock()
		_, sessionErrs := m.drainSessionControllersLocked(ctx, s, port, reportErrors)
		s.controllerMu.Unlock()
		errs = append(errs, sessionErrs...)
	}
	if !reportErrors {
		return nil
	}
	return errors.Join(errs...)
}

func (m *Module) sessionsForRefs(refs []ControllerRef) []*session {
	seen := make(map[string]bool)
	var sessions []*session
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, ref := range refs {
		if seen[ref.GuildID] {
			continue
		}
		if s := m.sessions[ref.GuildID]; s != nil {
			seen[ref.GuildID] = true
			sessions = append(sessions, s)
		}
	}
	return sessions
}

func (m *Module) drainSessionControllersLocked(ctx context.Context, s *session, port ControllerPort, reportErrors bool) ([]ControllerRef, []error) {
	s.mu.Lock()
	refs := s.pendingControllersLocked()
	s.mu.Unlock()
	var errs []error
	for _, ref := range refs {
		err := port.Delete(ctx, ref)
		if err == nil || IsDiscordNotFound(err) {
			s.mu.Lock()
			s.completeControllerCleanupLocked(ref)
			s.mu.Unlock()
			continue
		}
		if m.warn != nil {
			m.warn(err)
		}
		if reportErrors {
			errs = append(errs, fmt.Errorf("delete Controller %s/%s: %w", ref.ChannelID, ref.MessageID, err))
		}
	}
	return refs, errs
}

func (m *Module) SetControllerView(ctx context.Context, guildID string, view ControllerViewType, page int) (Snapshot, error) {
	if ctx == nil {
		ctx = m.ctx
	}
	s, err := m.getSession(ctx, guildID)
	if err != nil {
		return Snapshot{}, err
	}
	s.controllerMu.Lock()
	defer s.controllerMu.Unlock()

	s.mu.Lock()
	s.controllerView = view
	s.controllerQueuePage = page
	s.controllerViewActivity = m.now()
	s.controllerRevision++
	epoch := s.sessionEpoch
	generation := s.playbackGeneration
	snap := s.snapshotLocked()
	ref := ControllerRef{
		GuildID:            guildID,
		ChannelID:          s.textChannelID,
		MessageID:          s.controllerMessageID,
		SessionEpoch:       epoch,
		PlaybackGeneration: generation,
	}
	s.mu.Unlock()

	if m.controller != nil && ref.Valid() {
		newMsgID, errCreate := m.controller.CreateOrUpdate(ctx, ref, snap)
		s.mu.Lock()
		if errCreate == nil && newMsgID != "" {
			s.controllerMessageID = newMsgID
			s.controllerChannelID = ref.ChannelID
		}
		s.mu.Unlock()
	}
	return snap, nil
}

func (m *Module) GetControllerView(guildID string) (ControllerViewType, int) {
	m.mu.Lock()
	s := m.sessions[guildID]
	m.mu.Unlock()
	if s == nil {
		return ControllerViewPlayer, 1
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.controllerView, s.controllerQueuePage
}

func (m *Module) ResetControllerQueuePage(guildID string) {
	m.mu.Lock()
	s := m.sessions[guildID]
	m.mu.Unlock()
	if s == nil {
		return
	}
	s.mu.Lock()
	s.controllerQueuePage = 1
	s.controllerViewActivity = m.now()
	s.controllerRevision++
	s.mu.Unlock()
}

