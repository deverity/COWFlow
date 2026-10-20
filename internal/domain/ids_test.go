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

func TestPrefixes_Table(t *testing.T) {
	seen := map[string]Kind{}
	for k, p := range prefixes {
		require.Regexp(t, `^[a-z]{2,3}$`, p, "kind %s", k)
		other, dup := seen[p]
		require.False(t, dup, "prefix %s used by %s and %s", p, k, other)
		seen[p] = k
	}
	require.Len(t, prefixes, 19)
}

func TestParseID_RoundTrip(t *testing.T) {
	g := NewULIDGen(SystemClock{})
	for k := range prefixes {
		id, err := g.New(k)
		require.NoError(t, err)
		got, err := ParseID(id)
		require.NoError(t, err)
		require.Equal(t, k, got)
	}
}

func TestParseID_Invalid(t *testing.T) {
	const u = "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	for _, id := range []string{
		"", u, "prj" + u, "prj_" + u[:25], "prj_" + u + "0",
		"PRJ_" + u, "p_" + u, "prjx_" + u, "prj_" + "01ARZ3NDEKTSV4RRFFQ69G5FAI",
		"prj_" + "01arz3ndektsv4rrffq69g5fav", "zzz_" + u,
	} {
		_, err := ParseID(id)
		require.Error(t, err, "id %q", id)
	}
	k, err := ParseID("prj_" + u)
	require.NoError(t, err)
	require.Equal(t, KindProject, k)
}
