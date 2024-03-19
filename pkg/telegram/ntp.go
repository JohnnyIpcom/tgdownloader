package telegram

import (
	"time"

	"github.com/beevik/ntp"
	"github.com/gotd/td/clock"
)

type ntpClock struct {
	offset time.Duration
}

func (n *ntpClock) Now() time.Time {
	return time.Now().Add(n.offset)
}

func (n *ntpClock) Timer(d time.Duration) clock.Timer {
	return clock.System.Timer(d)
}

func (n *ntpClock) Ticker(d time.Duration) clock.Ticker {
	return clock.System.Ticker(d)
}

func NewNTPClock(ntpHost string) (clock.Clock, error) {
	resp, err := ntp.Query(ntpHost)
	if err != nil {
		return nil, err
	}

	if err := resp.Validate(); err != nil {
		return nil, err
	}

	return &ntpClock{
		offset: resp.ClockOffset,
	}, nil
}
