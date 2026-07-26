package renderer

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/johnnyipcom/tgdownloader/pkg/ps"
)

var tuiTrackerSequence atomic.Uint64

type tuiProgress struct {
	sink EventSink

	mu            sync.Mutex
	active        int
	changed       chan struct{}
	stopped       bool
	statusEnabled bool
	statusStop    chan struct{}
	statusWG      sync.WaitGroup
}

var _ Progress = (*tuiProgress)(nil)

func NewTUIProgress(sink EventSink) Progress {
	if sink == nil {
		sink = DiscardEvents()
	}
	return &tuiProgress{
		sink:       sink,
		changed:    make(chan struct{}),
		statusStop: make(chan struct{}),
	}
}

func (p *tuiProgress) EnablePS(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}

	p.mu.Lock()
	if p.stopped || p.statusEnabled {
		p.mu.Unlock()
		return
	}
	p.statusEnabled = true
	p.statusWG.Add(1)
	p.mu.Unlock()

	tracker := p.newTracker(strings.Join(ps.Humanize(ctx), " "), 0, false, false)
	go func() {
		defer p.statusWG.Done()
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				tracker.Done()
				return
			case <-p.statusStop:
				tracker.Done()
				return
			case <-ticker.C:
				tracker.UpdateMessage(strings.Join(ps.Humanize(ctx), " "))
			}
		}
	}()
}

func (p *tuiProgress) Wait(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}
	for {
		p.mu.Lock()
		if p.active == 0 || p.stopped {
			p.mu.Unlock()
			return
		}
		changed := p.changed
		p.mu.Unlock()

		select {
		case <-ctx.Done():
			return
		case <-changed:
		}
	}
}

func (p *tuiProgress) WaitAndStop(ctx context.Context) {
	p.Wait(ctx)
	p.Stop()
}

func (p *tuiProgress) Stop() {
	p.mu.Lock()
	if !p.stopped {
		p.stopped = true
		close(p.statusStop)
		p.signalChangedLocked()
	}
	p.mu.Unlock()

	p.statusWG.Wait()
}

func (p *tuiProgress) UnitsTracker(message string, total int) Tracker {
	return p.newTracker(message, int64(total), false, true)
}

func (p *tuiProgress) BytesTracker(writer io.Writer, message string, total int64) BytesTracker {
	if writer == nil {
		writer = io.Discard
	}
	return &tuiBytesTracker{tuiTracker: p.newTracker(message, total, true, true), writer: writer}
}

func (p *tuiProgress) newTracker(message string, total int64, bytes, counted bool) *tuiTracker {
	tracker := &tuiTracker{
		progress:  p,
		sink:      p.sink,
		id:        fmt.Sprintf("progress-%d", tuiTrackerSequence.Add(1)),
		message:   message,
		total:     total,
		bytes:     bytes,
		counted:   counted,
		startedAt: time.Now(),
	}
	if counted {
		p.mu.Lock()
		p.active++
		p.mu.Unlock()
	}
	tracker.emit(EventProgressCreate)
	return tracker
}

func (p *tuiProgress) trackerDone() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.active > 0 {
		p.active--
	}
	p.signalChangedLocked()
}

func (p *tuiProgress) signalChangedLocked() {
	close(p.changed)
	p.changed = make(chan struct{})
}

type tuiTracker struct {
	progress *tuiProgress
	sink     EventSink
	id       string
	bytes    bool
	counted  bool

	mu        sync.Mutex
	message   string
	current   int64
	total     int64
	terminal  bool
	startedAt time.Time
}

var _ Tracker = (*tuiTracker)(nil)

func (t *tuiTracker) Increment(n int64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.terminal {
		return
	}
	t.current += n
	t.emitLocked(EventProgressUpdate)
}

func (t *tuiTracker) UpdateMessage(message string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.terminal {
		return
	}
	t.message = message
	t.emitLocked(EventProgressUpdate)
}

func (t *tuiTracker) Fail() {
	t.finish(EventProgressFail)
}

func (t *tuiTracker) Done() {
	t.finish(EventProgressDone)
}

func (t *tuiTracker) finish(kind EventKind) {
	t.mu.Lock()
	if t.terminal {
		t.mu.Unlock()
		return
	}
	t.terminal = true
	t.emitLocked(kind)
	t.mu.Unlock()
	if t.counted {
		t.progress.trackerDone()
	}
}

func (t *tuiTracker) emit(kind EventKind) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.emitLocked(kind)
}

func (t *tuiTracker) emitLocked(kind EventKind) {
	unit := ProgressUnitCount
	if t.bytes {
		unit = ProgressUnitBytes
	}
	t.sink.Emit(Event{
		Kind:    kind,
		ID:      t.id,
		Label:   t.message,
		Current: t.current,
		Total:   t.total,
		Unit:    unit,
		Elapsed: time.Since(t.startedAt),
	})
}

type tuiBytesTracker struct {
	*tuiTracker
	writer io.Writer
}

var _ BytesTracker = (*tuiBytesTracker)(nil)

func (t *tuiBytesTracker) Write(data []byte) (int, error) {
	n, err := t.writer.Write(data)
	if err != nil {
		t.Fail()
		return n, err
	}
	t.Increment(int64(n))
	return n, nil
}
