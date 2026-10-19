package harness

import (
	"slices"
	"sync"
	"time"

	"github.com/deverity/cowflow/internal/domain"
)

// Clock is a deterministic domain.Clock. Time only moves when a test calls
// Advance. It is safe for concurrent use.
type Clock struct {
	mu      sync.Mutex
	now     time.Time
	seq     uint64
	waiters []*waiter // fire when now >= at
}

var _ domain.Clock = (*Clock)(nil)

// waiter is a one-shot timer (After) or a periodic one (ticker, period > 0).
type waiter struct {
	at     time.Time
	seq    uint64 // tie-break: registration order
	period time.Duration
	ch     chan time.Time // capacity 1; sends never block
}

// NewClock returns a Clock reading start.
func NewClock(start time.Time) *Clock { return &Clock{now: start} }

// Now returns the fake current time.
func (c *Clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// After returns a channel that receives the fake time once d has elapsed. A
// non-positive d fires immediately.
func (c *Clock) After(d time.Duration) <-chan time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	ch := make(chan time.Time, 1)
	if d <= 0 {
		ch <- c.now
		return ch
	}
	c.add(&waiter{at: c.now.Add(d), ch: ch})
	return ch
}

// NewTicker returns a Ticker that fires every d of fake time. Like
// time.NewTicker it panics when d is not positive, and it drops ticks a slow
// receiver has not consumed.
func (c *Clock) NewTicker(d time.Duration) domain.Ticker {
	if d <= 0 {
		panic("harness: non-positive interval for NewTicker")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	w := &waiter{at: c.now.Add(d), period: d, ch: make(chan time.Time, 1)}
	c.add(w)
	return &ticker{c: c, w: w}
}

// Advance moves the clock forward by d and fires every waiter that becomes
// due, in order of due time (registration order on ties). Each waiter sees
// the clock set to its own due time. Delivery happens without the lock held,
// so receivers may call Now. A non-positive d does nothing.
func (c *Clock) Advance(d time.Duration) {
	if d <= 0 {
		return
	}
	c.mu.Lock()
	target := c.now.Add(d)
	c.mu.Unlock()
	for {
		c.mu.Lock()
		w := c.popDue(target)
		if w == nil {
			c.now = target
			c.mu.Unlock()
			return
		}
		c.now = w.at
		fireAt := w.at
		if w.period > 0 {
			w.at = w.at.Add(w.period)
			c.add(w)
		}
		c.mu.Unlock()
		select {
		case w.ch <- fireAt:
		default: // slow ticker receiver: drop the tick
		}
	}
}

// add registers w. The caller holds c.mu.
func (c *Clock) add(w *waiter) {
	c.seq++
	w.seq = c.seq
	c.waiters = append(c.waiters, w)
}

// popDue removes and returns the earliest waiter due at or before target, or
// nil. The caller holds c.mu.
func (c *Clock) popDue(target time.Time) *waiter {
	best := -1
	for i, w := range c.waiters {
		if w.at.After(target) {
			continue
		}
		if best < 0 || w.at.Before(c.waiters[best].at) ||
			(w.at.Equal(c.waiters[best].at) && w.seq < c.waiters[best].seq) {
			best = i
		}
	}
	if best < 0 {
		return nil
	}
	w := c.waiters[best]
	c.waiters = slices.Delete(c.waiters, best, best+1)
	return w
}

type ticker struct {
	c *Clock
	w *waiter
}

func (t *ticker) C() <-chan time.Time { return t.w.ch }

func (t *ticker) Stop() {
	t.c.mu.Lock()
	defer t.c.mu.Unlock()
	t.c.waiters = slices.DeleteFunc(t.c.waiters, func(w *waiter) bool { return w == t.w })
}
