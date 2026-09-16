package radio

import (
	"context"
	"os"
	"sync"

	"gobot/internal/helpers"
)

type PrebufferTask struct {
	mu         sync.Mutex
	workerMu   sync.Mutex
	wg         sync.WaitGroup
	closed     bool
	cancel     context.CancelFunc
	generation uint64
	revision   uint64
	token      uint64
	track      *Track
	opusPath   string
	pathOwned  bool
	inFlight   bool
	workers    map[uint64]struct{}
	sharedPath func(string) bool
}

func (t *PrebufferTask) SetSharedPathChecker(checker func(string) bool) {
	t.mu.Lock()
	t.sharedPath = checker
	t.mu.Unlock()
}

func NewPrebufferTask() *PrebufferTask {
	return &PrebufferTask{workers: make(map[uint64]struct{})}
}

func (t *PrebufferTask) Cancel() {
	t.mu.Lock()
	cancel := t.cancel
	path := t.opusPath
	owned := t.pathOwned
	t.cancel = nil
	t.token++
	t.inFlight = len(t.workers) > 0
	t.track = nil
	t.opusPath = ""
	t.pathOwned = false
	t.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	t.cleanupPath(path, owned)
}

func (t *PrebufferTask) Wait() {
	t.workerMu.Lock()
	defer t.workerMu.Unlock()
	t.wg.Wait()
}

func (t *PrebufferTask) finishWorker(token uint64) {
	t.mu.Lock()
	delete(t.workers, token)
	if t.token == token {
		t.cancel = nil
	}
	t.inFlight = len(t.workers) > 0
	t.mu.Unlock()
}

func (t *PrebufferTask) closeAndWait(ctx context.Context) error {
	t.workerMu.Lock()
	t.closed = true
	t.workerMu.Unlock()
	t.Cancel()
	done := make(chan struct{})
	helpers.Spawn(func() {
		t.Wait()
		close(done)
	})
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (t *PrebufferTask) cleanupPath(path string, owned bool) {
	if !owned || path == "" {
		return
	}
	t.mu.Lock()
	checker := t.sharedPath
	t.mu.Unlock()
	shared := checker != nil && checker(path)
	if !shared {
		_ = os.Remove(path)
	}
}

func (t *PrebufferTask) Start(parentCtx context.Context, gen uint64, track Track, preparer AudioPreparer) {
	t.StartForRevision(parentCtx, gen, 0, track, preparer)
}

func (t *PrebufferTask) StartForRevision(parentCtx context.Context, gen, revision uint64, track Track, preparer AudioPreparer) {
	t.startForRevision(parentCtx, gen, revision, track, preparer, nil)
}

func (t *PrebufferTask) StartForRevisionOwned(parentCtx context.Context, gen, revision uint64, track Track, preparer AudioPreparer, launch func(func()) bool) {
	t.startForRevision(parentCtx, gen, revision, track, preparer, launch)
}

func (t *PrebufferTask) startForRevision(parentCtx context.Context, gen, revision uint64, track Track, preparer AudioPreparer, launch func(func()) bool) {
	t.Cancel()

	if preparer == nil {
		preparer = (*LRUSongCache)(nil)
	}

	if parentCtx == nil {
		parentCtx = context.Background()
	}
	ctx, cancel := context.WithCancel(parentCtx)
	trackCopy := track.Clone()
	t.mu.Lock()
	t.token++
	token := t.token
	t.cancel = cancel
	t.generation = gen
	t.revision = revision
	t.track = &trackCopy
	t.inFlight = true
	t.mu.Unlock()

	worker := func() {
		var path string
		var owned bool
		defer func() {
			if recovered := recover(); recovered != nil {
				logRecoveredPanic("prebuffer worker", recovered)
				t.cleanupPath(path, owned)
				t.finishWorker(token)
			}
		}()
		path, owned, err := preparer.PrepareTrack(ctx, &trackCopy)
		if err != nil || path == "" {
			t.finishWorker(token)
			return
		}

		t.mu.Lock()
		valid := t.token == token && t.generation == gen && t.revision == revision && ctx.Err() == nil
		checker := t.sharedPath
		t.mu.Unlock()
		shared := checker != nil && checker(path)
		t.mu.Lock()
		valid = valid && t.token == token && t.generation == gen && t.revision == revision && ctx.Err() == nil
		if valid {
			t.opusPath = path
			t.pathOwned = owned
			if shared {
				t.pathOwned = false
			}
			t.mu.Unlock()
			t.finishWorker(token)
			return
		}
		t.mu.Unlock()
		if owned && !shared {
			_ = os.Remove(path)
		}
		t.finishWorker(token)
	}
	t.workerMu.Lock()
	if t.closed {
		t.workerMu.Unlock()
		cancel()
		t.mu.Lock()
		if t.token == token {
			t.inFlight = len(t.workers) > 0
			t.cancel = nil
			t.track = nil
			t.opusPath = ""
			t.pathOwned = false
		}
		t.mu.Unlock()
		return
	}
	t.wg.Add(1)
	t.mu.Lock()
	if t.workers == nil {
		t.workers = make(map[uint64]struct{})
	}
	t.workers[token] = struct{}{}
	t.mu.Unlock()
	trackedWorker := func() {
		defer t.wg.Done()
		worker()
	}
	if launch != nil {
		if launch(trackedWorker) {
			t.workerMu.Unlock()
			return
		}
		t.wg.Done()
		t.mu.Lock()
		delete(t.workers, token)
		t.inFlight = len(t.workers) > 0
		t.mu.Unlock()
		t.workerMu.Unlock()
	} else {
		helpers.Spawn(trackedWorker)
		t.workerMu.Unlock()
		return
	}
	cancel()
	t.mu.Lock()
	if t.token == token {
		t.cancel = nil
		t.track = nil
		t.opusPath = ""
		t.pathOwned = false
		t.inFlight = len(t.workers) > 0
	}
	t.mu.Unlock()
}

func (t *PrebufferTask) IsIdle() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return !t.inFlight && t.opusPath == ""
}

func (t *PrebufferTask) HasPath(path string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return path != "" && t.opusPath == path
}

func (t *PrebufferTask) TakeFor(gen, revision uint64, track Track) (string, bool) {
	t.mu.Lock()
	if t.generation != gen || t.revision != revision || t.opusPath == "" || t.track == nil || !sameTrack(*t.track, track) {
		t.mu.Unlock()
		return "", false
	}
	path := t.opusPath
	owned := t.pathOwned
	cancel := t.cancel
	t.opusPath = ""
	t.pathOwned = false
	t.track = nil
	t.cancel = nil
	t.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return path, owned
}

func sameTrack(a, b Track) bool {
	if a.ID != "" || b.ID != "" {
		return a.ID == b.ID && a.Title == b.Title && a.URL == b.URL && a.WebpageURL == b.WebpageURL
	}
	return a.Title == b.Title && a.URL == b.URL && a.WebpageURL == b.WebpageURL
}
