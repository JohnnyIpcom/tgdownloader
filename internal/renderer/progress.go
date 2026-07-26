package renderer

import (
	"context"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/term"
)

type Progress interface {
	EnablePS(ctx context.Context)
	Wait(ctx context.Context)
	WaitAndStop(ctx context.Context)
	Stop()

	UnitsTracker(message string, total int) Tracker
	BytesTracker(writer io.Writer, message string, total int64) BytesTracker
}

type Tracker interface {
	Increment(n int64)
	UpdateMessage(message string)
	Fail()
	Done()
}

type BytesTracker interface {
	io.Writer
	Tracker
}

func NewProgress() Progress {
	return NewTUIProgress(newTerminalProgressSink(os.Stdout))
}

func NewProgressWithoutValue() Progress {
	return NewProgress()
}

// NewProgressForContext uses structured events when the command context carries a sink.
func NewProgressForContext(ctx context.Context) Progress {
	return ProgressForContext(ctx, nil)
}

func ProgressForContext(ctx context.Context, fallback Progress) Progress {
	if sink, ok := eventSinkFromContext(ctx); ok {
		return NewTUIProgress(sink)
	}
	if fallback != nil {
		return fallback
	}
	return NewProgress()
}

type terminalProgressSink struct {
	mu       sync.Mutex
	writer   io.Writer
	terminal bool
	fd       uintptr
	active   *Event
	activeAt time.Time
	baseTime time.Duration
	frame    int
}

func newTerminalProgressSink(writer io.Writer) EventSink {
	sink := &terminalProgressSink{writer: outputWriter(writer)}
	if file, ok := writer.(interface{ Fd() uintptr }); ok {
		sink.fd = file.Fd()
		sink.terminal = term.IsTerminal(sink.fd)
	}
	return sink
}

func (s *terminalProgressSink) Emit(event Event) {
	s.mu.Lock()
	defer s.mu.Unlock()

	width := s.width()
	switch event.Kind {
	case EventProgressCreate, EventProgressUpdate, "":
		if !s.terminal {
			return
		}
		if s.active == nil {
			if event.Total > 0 {
				return
			}
		} else if s.active.ID != event.ID {
			return
		}
		startAnimation := s.active == nil
		active := event
		s.active = &active
		s.activeAt = time.Now()
		s.baseTime = event.Elapsed
		s.writeLive(event, width, s.frame)
		if startAnimation {
			go s.animate(event.ID)
		}
	case EventProgressDone, EventProgressFail:
		row := FormatProgress(event, width, 0)
		if !s.terminal {
			_, _ = fmt.Fprintln(s.writer, ansi.Strip(row))
			return
		}
		if s.active != nil {
			_, _ = fmt.Fprint(s.writer, "\r\x1b[K")
		}
		_, _ = fmt.Fprintln(s.writer, row)
		if s.active != nil && s.active.ID == event.ID {
			s.active = nil
			return
		}
		if s.active != nil {
			s.writeLive(s.activeEvent(), width, s.frame)
		}
	}
}

func (s *terminalProgressSink) width() int {
	if s.fd != 0 {
		if width, _, err := term.GetSize(s.fd); err == nil && width > 0 {
			return width
		}
	}
	return 120
}

func (s *terminalProgressSink) writeLive(event Event, width, frame int) {
	row := FormatProgress(event, width, frame)
	_, _ = fmt.Fprint(s.writer, "\r", lipgloss.NewStyle().MaxWidth(width).Render(row), "\x1b[K")
}

func (s *terminalProgressSink) animate(id string) {
	ticker := time.NewTicker(120 * time.Millisecond)
	defer ticker.Stop()
	for range ticker.C {
		s.mu.Lock()
		if s.active == nil || s.active.ID != id || s.active.Total > 0 {
			s.mu.Unlock()
			return
		}
		s.frame++
		s.writeLive(s.activeEvent(), s.width(), s.frame)
		s.mu.Unlock()
	}
}

func (s *terminalProgressSink) activeEvent() Event {
	event := *s.active
	event.Elapsed = s.baseTime + time.Since(s.activeAt)
	return event
}
