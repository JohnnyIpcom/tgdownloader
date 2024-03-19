package telegram

import "fmt"

type Tracker interface {
	Fail()
	Done()
}

type ProgressRenderer interface {
	NewTracker(message string) Tracker
	Wait()
	Stop()
}

type progressRenderer struct{}

func (r *progressRenderer) NewTracker(message string) Tracker {
	fmt.Println(message)
	return &tracker{}
}

func (r *progressRenderer) Wait() {}

func (r *progressRenderer) Stop() {}

type tracker struct{}

func (t *tracker) Fail() {
	fmt.Println("Fail")
}

func (t *tracker) Done() {
	fmt.Println("Done")
}
