package telegram

import (
	"context"
	"fmt"
)

type Tracker interface {
	Fail()
	Done()
}

type Progress interface {
	Tracker(message string) Tracker
	Wait(ctx context.Context)
	WaitAndStop(ctx context.Context)
}

type ProgressProvider interface {
	NewProgress() Progress
}

type progress struct{}

var _ Progress = (*progress)(nil)

type tracker struct{}

func (t *tracker) Fail() {
	fmt.Println("Fail")
}

func (t *tracker) Done() {
	fmt.Println("Done")
}

func (r *progress) Tracker(message string) Tracker {
	fmt.Println(message)
	return &tracker{}
}

func (r *progress) Wait(ctx context.Context) {}

func (r *progress) WaitAndStop(ctx context.Context) {}

type progressProvider struct{}

var _ ProgressProvider = (*progressProvider)(nil)

func NewProgressProvider() ProgressProvider {
	return &progressProvider{}
}

func (p *progressProvider) NewProgress() Progress {
	return &progress{}
}
