package domain

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type frozenClock struct{ t time.Time }

func (c frozenClock) Now() time.Time                       { return c.t }
func (c frozenClock) After(time.Duration) <-chan time.Time { return nil }
func (c frozenClock) NewTicker(time.Duration) Ticker       { return nil }

func TestULIDGen_Monotonic(t *testing.T) {
	defer setTestPrefix(KindProject, "prj_")()
	g := NewULIDGen(frozenClock{t: time.Unix(1_700_000_000, 0)})
	prev := ""
	for i := 0; i < 10000; i++ {
		id, err := g.New(KindProject)
		require.NoError(t, err)
		require.True(t, strings.HasPrefix(id, "prj_"))
		require.Greater(t, id, prev)
		prev = id
	}
}

func TestULIDGen_Concurrent(t *testing.T) {
	defer setTestPrefix(KindProject, "prj_")()
	g := NewULIDGen(SystemClock{})
	const workers, per = 20, 1000
	out := make([][]string, workers)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < per; i++ {
				id, err := g.New(KindProject)
				if err != nil {
					t.Error(err)
					return
				}
				out[w] = append(out[w], id)
			}
		}()
	}
	wg.Wait()
	seen := make(map[string]struct{}, workers*per)
	for _, ids := range out {
		for _, id := range ids {
			_, dup := seen[id]
			require.False(t, dup, "duplicate %s", id)
			seen[id] = struct{}{}
		}
	}
	require.Len(t, seen, workers*per)
}

func TestULIDGen_UnknownKind(t *testing.T) {
	g := NewULIDGen(SystemClock{})
	_, err := g.New(Kind("nope"))
	require.Error(t, err)
}

func TestSystemClock(t *testing.T) {
	c := SystemClock{}
	require.WithinDuration(t, time.Now(), c.Now(), time.Second)
	select {
	case <-c.After(time.Millisecond):
	case <-time.After(time.Second):
		t.Fatal("After did not fire")
	}
	tk := c.NewTicker(time.Millisecond)
	defer tk.Stop()
	select {
	case <-tk.C():
	case <-time.After(time.Second):
		t.Fatal("ticker did not fire")
	}
}
