package ripper

import (
	"context"
	"errors"
	"net/url"
	"time"
)

var (
	ErrMediaTooLarge        = errors.New("media file exceeds maximum allowed size")
	ErrExternalUploadFailed = errors.New("external upload providers failed")
	ErrUserSlotExhausted    = errors.New("user has reached maximum active download slots")
	ErrQueueFull            = errors.New("download queue is at maximum capacity")
	ErrServiceStopped       = errors.New("media pipeline service is not running")
	ErrNoValidMedia         = errors.New("no valid media files found after download")
	ErrUnsupportedDomain    = errors.New("this domain is not supported")
	ErrPipelineBusy         = errors.New("media pipeline is busy or shutting down")
)

type Stage string

const (
	StageQueued       Stage = "queued"
	StageDownloading  Stage = "downloading"
	StageTransforming Stage = "transforming"
	StageValidating   Stage = "validating"
	StageUploading    Stage = "uploading"
	StageCompleted    Stage = "completed"
	StageFailed       Stage = "failed"
)

type Progress struct {
	Stage         Stage
	QueuePosition int
	Percent       float64
	Speed         string
	ETA           string
}

type ProgressSink interface {
	OnProgress(ctx context.Context, p Progress)
}

type TaskRequest struct {
	ID           string
	URL          string
	AudioOnly    bool
	BestQuality  bool
	UserID       string
	GuildID      string
	MaxSizeBytes int64
}

type MediaMeta struct {
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Uploader    string   `json:"uploader"`
	UploaderID  string   `json:"uploader_id"`
	WebpageURL  string   `json:"webpage_url"`
	Thumbnail   string   `json:"thumbnail"`
	Tags        []string `json:"tags"`
}

type TaskOutcome struct {
	ID       string
	URL      string
	Meta     MediaMeta
	Files    []string
	Err      error
	Duration time.Duration
}

type OutcomeCallback func(ctx context.Context, outcome TaskOutcome) error

type DownloadRequest struct {
	TaskID       string
	URL          string
	AudioOnly    bool
	BestQuality  bool
	MaxSizeBytes int64
	OutputDir    string
}

type DownloadResult struct {
	Meta  MediaMeta
	Files []string
}

type Downloader interface {
	Download(ctx context.Context, req DownloadRequest, progress chan<- Progress) (DownloadResult, error)
	Supports(targetURL *url.URL) bool
}
