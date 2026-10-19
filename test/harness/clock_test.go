package harness_test

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/deverity/cowflow/test/harness"
)

var epoch = time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)

func TestClock_AdvanceFiresInOrder(t *testing.T) {
	c := harness.NewClock(epoch)
	third := c.After(30 * time.Second)
	first := c.After(10 * time.Second)
	second := c.After(20 * time.Second)
	never := c.After(time.Hour)

	assert.Equal(t, epoch, c.Now())
	c.Advance(15 * time.Second)
	assert.Equal(t, epoch.Add(15*time.Second), c.Now())
	require.Len(t, first, 1)
	assert.Empty(t, second)

	// Each waiter observes its own due time, in due order.
	assert.Equal(t, epoch.Add(10*time.Second), <-first)
	c.Advance(time.Minute)
	assert.Equal(t, epoch.Add(20*time.Second), <-second)
	assert.Equal(t, epoch.Add(30*time.Second), <-third)
	assert.Empty(t, never)
	assert.Equal(t, epoch.Add(75*time.Second), c.Now())
}

func TestClock_AfterNonPositiveFiresImmediately(t *testing.T) {
	c := harness.NewClock(epoch)
	assert.Equal(t, epoch, <-c.After(0))
	assert.Equal(t, epoch, <-c.After(-time.Second))
}

func TestClock_AdvanceNonPositiveIsNoop(t *testing.T) {
	c := harness.NewClock(epoch)
	c.Advance(0)
	c.Advance(-time.Hour)
	assert.Equal(t, epoch, c.Now())
}

func TestClock_Ticker(t *testing.T) {
	c := harness.NewClock(epoch)
	tk := c.NewTicker(10 * time.Second)

	c.Advance(9 * time.Second)
	assert.Empty(t, tk.C())
	c.Advance(time.Second)
	assert.Equal(t, epoch.Add(10*time.Second), <-tk.C())

	// A slow receiver loses ticks, like time.Ticker; it does not block Advance.
	c.Advance(35 * time.Second)
	assert.Len(t, tk.C(), 1)
	<-tk.C()

	tk.Stop()
	c.Advance(time.Hour)
	assert.Empty(t, tk.C())
}

func TestClock_TickerPanicsOnNonPositive(t *testing.T) {
	c := harness.NewClock(epoch)
	assert.Panics(t, func() { c.NewTicker(0) })
}

func TestClock_ReceiverMayCallNow(t *testing.T) {
	c := harness.NewClock(epoch)
	ch := c.After(time.Second)
	got := make(chan time.Time, 1)
	go func() {
		<-ch
		got <- c.Now() // must not deadlock against Advance
	}()
	c.Advance(time.Second)
	assert.Equal(t, epoch.Add(time.Second), <-got)
}

func TestClock_Concurrent(t *testing.T) {
	c := harness.NewClock(epoch)
	const workers = 8
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tk := c.NewTicker(time.Second)
			defer tk.Stop()
			for range 50 {
				_ = c.Now()
				_ = c.After(time.Second)
				select {
				case <-tk.C():
				default:
				}
			}
		}()
	}
	for range 50 {
		c.Advance(time.Second)
	}
	wg.Wait()
	assert.False(t, c.Now().Before(epoch.Add(50*time.Second)))
}
