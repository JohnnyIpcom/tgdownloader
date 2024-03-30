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
