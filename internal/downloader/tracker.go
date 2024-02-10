package downloader

import "io"

type TrackedWriter interface {
	io.Writer

	Fail()
	Done()
}

type Tracker interface {
	WrapWriter(w io.Writer, msg string, size int64) TrackedWriter
	Stop()
}

type nullTrackedWriter struct {
	w io.Writer
}

func (ntw *nullTrackedWriter) Write(p []byte) (int, error) {
	return ntw.w.Write(p)
}

func (ntw *nullTrackedWriter) Fail() {}

func (ntw *nullTrackedWriter) Done() {}

type nullTracker struct{}

func NewNullTracker() Tracker {
	return &nullTracker{}
}

func (nt *nullTracker) WrapWriter(w io.Writer, msg string, size int64) TrackedWriter {
	return &nullTrackedWriter{w: w}
}

func (nt *nullTracker) Stop() {}
