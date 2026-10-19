package domain

import "time"

// Clock is injected everywhere time matters so the harness fake clock works (§18.1).
type Clock interface {
	Now() time.Time
	After(d time.Duration) <-chan time.Time
	NewTicker(d time.Duration) Ticker
}

// Ticker is the Clock-neutral counterpart of time.Ticker.
type Ticker interface {
	C() <-chan time.Time
	Stop()
}

// SystemClock is the real-time Clock. It is the only place in domain and
// service code allowed to call time.Now.
type SystemClock struct{}

var _ Clock = SystemClock{}

// Now returns the current wall-clock time.
func (SystemClock) Now() time.Time { return time.Now() }

// After waits for d to elapse and then sends the current time on the channel.
func (SystemClock) After(d time.Duration) <-chan time.Time { return time.After(d) }

// NewTicker returns a Ticker that fires every d.
func (SystemClock) NewTicker(d time.Duration) Ticker {
	return systemTicker{t: time.NewTicker(d)}
}

type systemTicker struct{ t *time.Ticker }

func (s systemTicker) C() <-chan time.Time { return s.t.C }
func (s systemTicker) Stop()               { s.t.Stop() }
