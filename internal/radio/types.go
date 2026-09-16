package radio

import (
	"context"
	"errors"
	"strings"
	"time"
)

var (
	ErrInvalidGuildID       = errors.New("invalid guild ID")
	ErrInvalidRequest       = errors.New("invalid radio request")
	ErrInvalidPhase         = errors.New("invalid radio phase")
	ErrInvalidControlAction = errors.New("invalid radio control action")
	ErrMissingContext       = errors.New("radio module requires a context")
	ErrShuttingDown         = errors.New("radio module is shutting down")
	ErrStaleOperation       = errors.New("radio operation became stale")
	ErrVoiceUnavailable     = errors.New("voice connection unavailable")
	ErrVoiceTransport       = errors.New("voice transport failed")
	ErrPlaybackSource       = errors.New("playback source failed")
	ErrPlaybackPanic        = errors.New("playback worker panicked")
	ErrDAVEFrameHeld        = errors.New("dave frame held pending negotiation")
)

type Track struct {
	ID            string `json:"id,omitempty"`
	Title         string `json:"title"`
	URL           string `json:"url"`
	WebpageURL    string `json:"webpage_url"`
	Uploader      string `json:"uploader"`
	Thumbnail     string `json:"thumbnail"`
	CoverURL      string `json:"cover_url,omitempty"`
	CoverData     []byte `json:"-"`
	CoverFilename string `json:"-"`
	Era           string `json:"era,omitempty"`
	Category      string `json:"category,omitempty"`
	Duration      int    `json:"duration"`
	IsAutoplay    bool   `json:"is_autoplay,omitempty"`
	RequesterID   string `json:"requester_id,omitempty"`
}

func (t Track) Clone() Track {
	clone := t
	clone.CoverData = append([]byte(nil), t.CoverData...)
	return clone
}

func cloneTracks(tracks []Track) []Track {
	if len(tracks) == 0 {
		return nil
	}
	clones := make([]Track, len(tracks))
	for i := range tracks {
		clones[i] = tracks[i].Clone()
	}
	return clones
}

func cloneTrack(t *Track) *Track {
	if t == nil {
		return nil
	}
	c := t.Clone()
	return &c
}

func cloneTrackPointers(tracks []*Track) []*Track {
	if len(tracks) == 0 {
		return nil
	}
	clones := make([]*Track, 0, len(tracks))
	for _, track := range tracks {
		clones = append(clones, cloneTrack(track))
	}
	return clones
}

type Phase string

const (
	PhaseIdle       Phase = "idle"
	PhaseConnecting Phase = "connecting"
	PhaseLoading    Phase = "loading"
	PhasePlaying    Phase = "playing"
	PhasePaused     Phase = "paused"
	PhaseStopping   Phase = "stopping"
	PhaseRecovering Phase = "recovering"
)

func (p Phase) Valid() bool {
	switch p {
	case PhaseIdle, PhaseConnecting, PhaseLoading, PhasePlaying, PhasePaused, PhaseStopping, PhaseRecovering:
		return true
	default:
		return false
	}
}

type ConnectRequest struct {
	GuildID          string
	VoiceChannelID   string
	TextChannelID    string
	SuppressAutoplay bool
}

type EnqueuePlacement uint8

const (
	EnqueueBack EnqueuePlacement = iota
	EnqueueNext
)

type EnqueueRequest struct {
	GuildID        string
	VoiceChannelID string
	TextChannelID  string
	Tracks         []Track
	Placement      EnqueuePlacement
}

type ControlAction uint8

const (
	ControlPause ControlAction = iota
	ControlResume
	ControlPauseForEmptyChannel
	ControlResumeForHumanReturn
	ControlSkip
	ControlPrevious
	ControlLoop
	ControlLoopOff
	ControlLoopTrack
	ControlLoopQueue
	ControlShuffle
	ControlClearQueue
	ControlStop
	ControlLeave
)

func (a ControlAction) Valid() bool {
	switch a {
	case ControlPause, ControlResume, ControlSkip, ControlPrevious, ControlLoop, ControlShuffle, ControlClearQueue, ControlStop, ControlLeave:
		return true
	case ControlPauseForEmptyChannel, ControlResumeForHumanReturn, ControlLoopOff, ControlLoopTrack, ControlLoopQueue:
		return true
	default:
		return false
	}
}

type LoopMode string

const (
	LoopOff   LoopMode = "off"
	LoopTrack LoopMode = "track"
	LoopQueue LoopMode = "queue"
)

func (m LoopMode) Valid() bool {
	return m == LoopOff || m == LoopTrack || m == LoopQueue
}

func nextLoopMode(current LoopMode) LoopMode {
	switch current {
	case LoopOff:
		return LoopTrack
	case LoopTrack:
		return LoopQueue
	default:
		return LoopOff
	}
}

type PauseCause string

const (
	PauseCauseNone         PauseCause = ""
	PauseCauseManual       PauseCause = "manual"
	PauseCauseEmptyChannel PauseCause = "empty_channel"
)

func (m LoopMode) String() string {
	return strings.TrimSpace(string(m))
}

type ControlRequest struct {
	GuildID string
	Action  ControlAction
}

type SettingsUpdate struct {
	GuildID              string
	RestrictionMode      string
	AutojoinChannelID    string
	PlayerChannelID      string
	RestrictionModeSet   bool
	AutojoinChannelIDSet bool
	PlayerChannelIDSet   bool
	Is247                bool
	Is247Set             bool
	Mode                 string
	ModeSet              bool
	Volume               float64
	VolumeSet            bool
}

type ControllerViewType int

const (
	ControllerViewPlayer ControllerViewType = iota
	ControllerViewQueue
)

type Snapshot struct {
	GuildID             string
	VoiceChannelID      string
	TextChannelID       string
	Phase               Phase
	PauseCause          PauseCause
	RestrictionMode     string
	AutojoinChannelID   string
	PlayerChannelID     string
	Is247               bool
	LoopMode            LoopMode
	Mode                string
	Volume              float64
	Queue               []Track
	History             []Track
	Current             *Track
	PlaybackGeneration  uint64
	SessionEpoch        uint64
	LoadingSince        time.Time
	TrackStartTime      time.Time
	PausedDuration      time.Duration
	PauseStartTime      time.Time
	ControllerChannelID string
	ControllerMessageID string
	ControllerCleanup   bool
	PendingControllers  []ControllerRef
	ControllerView      ControllerViewType
	QueuePage           int
	ControllerRevision  uint64
}

func (s Snapshot) Clone() Snapshot {
	clone := s
	clone.Queue = cloneTracks(s.Queue)
	clone.History = cloneTracks(s.History)
	if s.Current != nil {
		current := s.Current.Clone()
		clone.Current = &current
	}
	clone.PendingControllers = append([]ControllerRef(nil), s.PendingControllers...)
	return clone
}

type VoiceConnection interface {
	SendOpus(frame []byte) error
	SetSpeaking(speaking bool) error
	Close()
	IsClosed() bool
}

type VoicePort interface {
	Join(context.Context, string, string) (VoiceConnection, error)
	Disconnect(context.Context, string) error
}

type TrackProvider interface {
	NextTrack(ctx context.Context, guildID, mode string, recentTitles []string) (*Track, error)
}

type VoiceStateEvent struct {
	GuildID           string
	UserID            string
	BotUserID         string
	ChannelID         string
	PreviousChannelID string
	HumanCount        int
	HumanCountSet     bool
}

type ChannelDeleted struct {
	GuildID   string
	ChannelID string
}
