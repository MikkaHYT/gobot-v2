package ripper

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"gobot/internal/helpers"
	"gobot/internal/logger"
)

var safeIDRegex = regexp.MustCompile(`[^a-zA-Z0-9_-]`)

type Options struct {
	BaseDir                string
	MaxConcurrentDownloads int
	MaxUserSlots           int
	QueueCapacity          int
	DownloadTimeout        time.Duration
	PresentationTimeout    time.Duration
	ShutdownTimeout        time.Duration
	Downloader             Downloader
	MaxDownloadSizeMB      int64
}

type queuedTask struct {
	req       TaskRequest
	reqCtx    context.Context
	sink      ProgressSink
	onOutcome OutcomeCallback
}

type pipelineState int

const (
	stateStopped pipelineState = iota
	stateRunning
	stateStopping
)

type Pipeline struct {
	opts     Options
	slots    *slotManager
	taskCh   chan queuedTask
	workerCh chan struct{}
	baseDir  string
	taskSeq  uint64

	mu           sync.Mutex
	state        pipelineState
	ctx          context.Context
	cancel       context.CancelFunc
	stopCh       chan struct{}
	wg           sync.WaitGroup
	dispatcherWg sync.WaitGroup
}

func New(opts Options) *Pipeline {
	if opts.MaxConcurrentDownloads <= 0 {
		opts.MaxConcurrentDownloads = 3
	}
	if opts.MaxUserSlots <= 0 {
		opts.MaxUserSlots = 2
	}
	if opts.QueueCapacity <= 0 {
		opts.QueueCapacity = 100
	}
	if opts.DownloadTimeout <= 0 {
		opts.DownloadTimeout = 5 * time.Minute
	}
	if opts.PresentationTimeout <= 0 {
		opts.PresentationTimeout = 3 * time.Minute
	}
	if opts.ShutdownTimeout <= 0 {
		opts.ShutdownTimeout = 5 * time.Second
	}
	if opts.BaseDir == "" {
		opts.BaseDir = filepath.Join("data", "temp")
	}
	if opts.Downloader == nil {
		opts.Downloader = NewCompositeDownloader(NewYTDLPDownloader(), NewAPIFallbackDownloader())
	}

	return &Pipeline{
		opts:     opts,
		slots:    newSlotManager(),
		taskCh:   make(chan queuedTask, opts.QueueCapacity),
		workerCh: make(chan struct{}, opts.MaxConcurrentDownloads),
		baseDir:  opts.BaseDir,
		stopCh:   make(chan struct{}),
	}
}

func (p *Pipeline) Start(ctx context.Context) error {
	cleanBase := filepath.Clean(p.baseDir)
	if err := os.MkdirAll(cleanBase, 0700); err != nil {
		return fmt.Errorf("failed to create scratch root %s: %w", cleanBase, err)
	}

	p.mu.Lock()
	if p.state == stateRunning {
		p.mu.Unlock()
		return nil
	}
	if p.state == stateStopping {
		p.mu.Unlock()
		return ErrPipelineBusy
	}
	p.state = stateRunning
	p.stopCh = make(chan struct{})
	p.ctx, p.cancel = context.WithCancel(ctx)
	stopCh := p.stopCh
	runCtx := p.ctx
	p.mu.Unlock()

	p.wg.Add(1)
	p.dispatcherWg.Add(1)
	helpers.Spawn(func() {
		p.runDispatcher(runCtx, stopCh)
	})
	return nil
}

func (p *Pipeline) Stop() {
	p.StopWithTimeout(p.opts.ShutdownTimeout)
}

func (p *Pipeline) StopWithTimeout(timeout time.Duration) {
	p.mu.Lock()
	if p.state != stateRunning {
		p.mu.Unlock()
		return
	}
	p.state = stateStopping
	if p.cancel != nil {
		p.cancel()
	}
	close(p.stopCh)
	p.mu.Unlock()

	done := make(chan struct{})
	helpers.Spawn(func() {
		p.dispatcherWg.Wait()
		p.wg.Wait()
		p.mu.Lock()
		p.state = stateStopped
		p.mu.Unlock()
		close(done)
	})

	select {
	case <-done:
	case <-time.After(timeout):
		logger.Warnf("[RIPPER] Pipeline shutdown timed out after %v waiting for workers", timeout)
	}

	p.drainRemaining()
}

func (p *Pipeline) Submit(ctx context.Context, req TaskRequest, sink ProgressSink, onOutcome OutcomeCallback) error {
	if ctx != nil && ctx.Err() != nil {
		return ctx.Err()
	}

	p.mu.Lock()
	if p.state != stateRunning || p.ctx == nil || p.ctx.Err() != nil {
		p.mu.Unlock()
		return ErrServiceStopped
	}

	if !p.slots.tryAcquire(req.UserID, p.opts.MaxUserSlots) {
		p.mu.Unlock()
		return ErrUserSlotExhausted
	}

	task := queuedTask{
		req:       req,
		reqCtx:    ctx,
		sink:      sink,
		onOutcome: onOutcome,
	}

	select {
	case p.taskCh <- task:
		queuePos := len(p.taskCh)
		p.mu.Unlock()

		if sink != nil {
			sinkCtx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
			_ = func() (err error) {
				defer func() {
					if r := recover(); r != nil {
						logger.Errorf("[RIPPER] Panic in initial progress sink for task %s: %v", task.req.ID, r)
					}
				}()
				sink.OnProgress(sinkCtx, Progress{
					Stage:         StageQueued,
					QueuePosition: queuePos,
				})
				return nil
			}()
			cancel()
		}
		return nil
	default:
		p.slots.release(req.UserID)
		p.mu.Unlock()
		return ErrQueueFull
	}
}

func (p *Pipeline) runDispatcher(ctx context.Context, stopCh <-chan struct{}) {
	defer p.dispatcherWg.Done()
	defer p.wg.Done()
	for {
		select {
		case <-stopCh:
			return
		case <-ctx.Done():
			return
		case task := <-p.taskCh:
			if task.reqCtx != nil && task.reqCtx.Err() != nil {
				p.slots.release(task.req.UserID)
				p.cancelTaskWithErr(task, task.reqCtx.Err())
				continue
			}

			select {
			case p.workerCh <- struct{}{}:
				p.wg.Add(1)
				go p.runWorker(task)
			case <-stopCh:
				p.cancelQueuedTask(task)
				return
			case <-ctx.Done():
				p.cancelQueuedTask(task)
				return
			}
		}
	}
}

func (p *Pipeline) cancelQueuedTask(task queuedTask) {
	p.slots.release(task.req.UserID)
	p.cancelTaskWithErr(task, ErrServiceStopped)
}

func (p *Pipeline) cancelTaskWithErr(task queuedTask, cancelErr error) {
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	defer func() {
		if r := recover(); r != nil {
			logger.Errorf("[RIPPER] Panic during task %s cancellation: %v", task.req.ID, r)
		}
	}()

	if task.sink != nil {
		task.sink.OnProgress(shutdownCtx, Progress{Stage: StageFailed})
	}
	if task.onOutcome != nil {
		_ = task.onOutcome(shutdownCtx, TaskOutcome{
			ID:  task.req.ID,
			URL: task.req.URL,
			Err: cancelErr,
		})
	}
}

func (p *Pipeline) runWorker(task queuedTask) {
	defer func() {
		<-p.workerCh
		p.slots.release(task.req.UserID)
		p.wg.Done()
	}()

	defer func() {
		if r := recover(); r != nil {
			logger.Errorf("[RIPPER] Panic executing task %s: %v", task.req.ID, r)
			p.cancelTaskWithErr(task, fmt.Errorf("task panicked during execution: %v", r))
		}
	}()

	p.executeTask(task)
}

func (p *Pipeline) executeTask(task queuedTask) {
	startTime := time.Now()
	dispatcher := newProgressDispatcher(task.sink)
	defer dispatcher.close()

	if task.reqCtx != nil && task.reqCtx.Err() != nil {
		dispatcher.report(Progress{Stage: StageFailed})
		_, _ = p.deliverOutcome(task, TaskOutcome{
			ID:  task.req.ID,
			URL: task.req.URL,
			Err: task.reqCtx.Err(),
		})
		return
	}

	sanitizedID := safeIDRegex.ReplaceAllString(task.req.ID, "_")
	if sanitizedID == "" {
		sanitizedID = "task"
	}
	taskDirName := fmt.Sprintf("rip_%s_%d_%d", sanitizedID, time.Now().UnixNano(), atomic.AddUint64(&p.taskSeq, 1))

	cleanBase := filepath.Clean(p.baseDir)
	taskDir := filepath.Clean(filepath.Join(cleanBase, taskDirName))
	var outcomeDone <-chan struct{}

	rel, relErr := filepath.Rel(cleanBase, taskDir)
	if relErr != nil || strings.HasPrefix(rel, "..") || rel == "." {
		dispatcher.report(Progress{Stage: StageFailed})
		outcomeDone, _ = p.deliverOutcome(task, TaskOutcome{
			ID:  task.req.ID,
			URL: task.req.URL,
			Err: fmt.Errorf("task directory escapes base scratch root"),
		})
		return
	}

	if err := os.MkdirAll(taskDir, 0700); err != nil {
		dispatcher.report(Progress{Stage: StageFailed})
		outcomeDone, _ = p.deliverOutcome(task, TaskOutcome{
			ID:  task.req.ID,
			URL: task.req.URL,
			Err: fmt.Errorf("failed to create scratch directory: %w", err),
		})
		return
	}
	defer func() {
		cleanup := func() {
			if err := os.RemoveAll(taskDir); err != nil {
				logger.Warnf("[RIPPER] Failed to remove scratch directory %s: %v", taskDir, err)
				time.AfterFunc(5*time.Second, func() {
					_ = os.RemoveAll(taskDir)
				})
			}
		}
		if outcomeDone != nil {
			select {
			case <-outcomeDone:
				cleanup()
				return
			default:
				helpers.Spawn(func() {
					select {
					case <-outcomeDone:
					case <-time.After(10 * time.Second):
					}
					cleanup()
				})
				return
			}
		}
		cleanup()
	}()

	p.mu.Lock()
	baseCtx := p.ctx
	p.mu.Unlock()

	if task.reqCtx != nil {
		var cancelReq context.CancelFunc
		baseCtx, cancelReq = mergeContexts(baseCtx, task.reqCtx)
		defer cancelReq()
	}

	downloadCtx, cancel := context.WithTimeout(baseCtx, p.opts.DownloadTimeout)
	defer cancel()

	dispatcher.report(Progress{Stage: StageDownloading})

	progressBridge := make(chan Progress, 20)
	var bridgeWg sync.WaitGroup
	bridgeWg.Add(1)
	helpers.Spawn(func() {
		defer bridgeWg.Done()
		for prg := range progressBridge {
			dispatcher.report(prg)
		}
	})

	maxDlMB := p.opts.MaxDownloadSizeMB
	if maxDlMB <= 0 {
		maxDlMB = 200
	}
	maxDlBytes := maxDlMB * 1024 * 1024
	if task.req.MaxSizeBytes > maxDlBytes {
		maxDlBytes = task.req.MaxSizeBytes
	}

	downloadReq := DownloadRequest{
		TaskID:       sanitizedID,
		URL:          task.req.URL,
		AudioOnly:    task.req.AudioOnly,
		BestQuality:  task.req.BestQuality,
		MaxSizeBytes: maxDlBytes,
		OutputDir:    taskDir,
	}

	res, dlErr := p.opts.Downloader.Download(downloadCtx, downloadReq, progressBridge)
	close(progressBridge)
	bridgeWg.Wait()

	if dlErr != nil {
		dispatcher.report(Progress{Stage: StageFailed})
		outcomeDone, _ = p.deliverOutcome(task, TaskOutcome{
			ID:  task.req.ID,
			URL: task.req.URL,
			Err: dlErr,
		})
		return
	}

	if task.req.AudioOnly && len(res.Files) == 1 && res.Meta.Thumbnail != "" {
		dispatcher.report(Progress{Stage: StageTransforming})
		res.Files[0] = embedID3Artwork(downloadCtx, res.Files[0], res.Meta.Thumbnail)
	}

	dispatcher.report(Progress{Stage: StageValidating})
	validFiles, valErr := validateMediaFiles(res.Files, maxDlBytes)
	if valErr != nil {
		dispatcher.report(Progress{Stage: StageFailed})
		outcomeDone, _ = p.deliverOutcome(task, TaskOutcome{
			ID:  task.req.ID,
			URL: task.req.URL,
			Err: valErr,
		})
		return
	}

	dispatcher.report(Progress{Stage: StageUploading})
	outcomeDone, outcomeErr := p.deliverOutcome(task, TaskOutcome{
		ID:       task.req.ID,
		URL:      task.req.URL,
		Meta:     res.Meta,
		Files:    validFiles,
		Duration: time.Since(startTime),
	})
	if outcomeErr != nil {
		dispatcher.report(Progress{Stage: StageFailed})
	} else {
		dispatcher.report(Progress{Stage: StageCompleted})
	}
}

func (p *Pipeline) deliverOutcome(task queuedTask, outcome TaskOutcome) (<-chan struct{}, error) {
	done := make(chan struct{})
	if task.onOutcome == nil {
		close(done)
		return done, nil
	}

	p.mu.Lock()
	baseCtx := p.ctx
	p.mu.Unlock()

	callbackCtx, cancel := context.WithTimeout(baseCtx, p.opts.PresentationTimeout)

	var cbErr error
	go func() {
		defer cancel()
		defer func() {
			if r := recover(); r != nil {
				logger.Errorf("[RIPPER] Panic in outcome callback for task %s: %v", task.req.ID, r)
				cbErr = fmt.Errorf("panic in outcome callback: %v", r)
			}
			close(done)
		}()
		cbErr = task.onOutcome(callbackCtx, outcome)
	}()

	select {
	case <-done:
		return done, cbErr
	case <-callbackCtx.Done():
		logger.Warnf("[RIPPER] Outcome presentation timed out for task %s", task.req.ID)
		return done, callbackCtx.Err()
	}
}

func (p *Pipeline) drainRemaining() {
	for {
		select {
		case t := <-p.taskCh:
			p.slots.release(t.req.UserID)
			go func(task queuedTask) {
				defer func() {
					if r := recover(); r != nil {
						logger.Errorf("[RIPPER] Panic draining task %s: %v", task.req.ID, r)
					}
				}()
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				if task.sink != nil {
					task.sink.OnProgress(ctx, Progress{Stage: StageFailed})
				}
				if task.onOutcome != nil {
					_ = task.onOutcome(ctx, TaskOutcome{
						ID:  task.req.ID,
						URL: task.req.URL,
						Err: ErrServiceStopped,
					})
				}
			}(t)
		default:
			return
		}
	}
}

type progressDispatcher struct {
	sink     ProgressSink
	ch       chan Progress
	done     chan struct{}
	lastSend time.Time
	wg       sync.WaitGroup
}

func newProgressDispatcher(sink ProgressSink) *progressDispatcher {
	d := &progressDispatcher{
		sink: sink,
		ch:   make(chan Progress, 64),
		done: make(chan struct{}),
	}
	if sink == nil {
		return d
	}
	d.wg.Add(1)
	helpers.Spawn(d.loop)
	return d
}

func (d *progressDispatcher) report(p Progress) {
	if d.sink == nil {
		return
	}
	select {
	case <-d.done:
		return
	default:
	}

	select {
	case d.ch <- p:
	default:
		if p.Stage != "" {
			select {
			case <-d.ch:
			default:
			}
			select {
			case d.ch <- p:
			default:
			}
		}
	}
}

func (d *progressDispatcher) deliver(p Progress) {
	defer func() {
		if r := recover(); r != nil {
			logger.Errorf("[RIPPER] Panic in progress sink callback: %v", r)
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	d.sink.OnProgress(ctx, p)
}

func (d *progressDispatcher) loop() {
	defer d.wg.Done()
	const throttleInterval = 1500 * time.Millisecond

	for {
		select {
		case <-d.done:
			for {
				select {
				case p := <-d.ch:
					d.deliver(p)
				default:
					return
				}
			}
		case p := <-d.ch:
			now := time.Now()
			isStageChange := p.Stage != "" && p.Stage != StageDownloading
			if isStageChange || now.Sub(d.lastSend) >= throttleInterval {
				d.deliver(p)
				d.lastSend = now
			}
		}
	}
}

func (d *progressDispatcher) close() {
	if d.sink == nil {
		return
	}
	close(d.done)
	done := make(chan struct{})
	helpers.Spawn(func() {
		d.wg.Wait()
		close(done)
	})
	select {
	case <-done:
	case <-time.After(1 * time.Second):
	}
}

func mergeContexts(parent, child context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(parent)
	stop := context.AfterFunc(child, func() {
		cancel()
	})
	return ctx, func() {
		stop()
		cancel()
	}
}
