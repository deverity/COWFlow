package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/deverity/cowflow/internal/domain"
)

// testIDs mints unique IDs without the production prefix table or
// the ULID generator. The tag keeps IDs unique across processes.
type testIDs struct {
	tag string
	n   *atomic.Int64
}

func newTestIDs(tag string) testIDs { return testIDs{tag: tag, n: new(atomic.Int64)} }

func (g testIDs) New(domain.Kind) (string, error) {
	return fmt.Sprintf("test_%s_%d", g.tag, g.n.Add(1)), nil
}

func openTest(t *testing.T, path string, ids domain.IDGen) *Store {
	t.Helper()
	s, err := Open(context.Background(), path, Options{IDs: ids})
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func countProjects(t *testing.T, s *Store) int {
	t.Helper()
	var n int
	require.NoError(t, s.db.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM projects").Scan(&n))
	return n
}

func TestWithTx_RollbackOnError(t *testing.T) {
	s := openTest(t, dbPath(t), newTestIDs("a"))
	boom := errors.New("boom")
	err := s.WithTx(context.Background(), "op", func(tx *Tx) error {
		_, err := tx.InsertProject("p", "/r")
		require.NoError(t, err)
		return boom
	})
	require.ErrorIs(t, err, boom)
	require.Equal(t, 0, countProjects(t, s))
}

func TestWithTx_Commit(t *testing.T) {
	s := openTest(t, dbPath(t), newTestIDs("a"))
	require.NoError(t, s.WithTx(context.Background(), "op", func(tx *Tx) error {
		_, err := tx.InsertProject("p", "/r")
		return err
	}))
	require.Equal(t, 1, countProjects(t, s))
}

func TestWithTx_ConcurrentWriters(t *testing.T) {
	const writers, per = 20, 50
	p := dbPath(t)
	ids := newTestIDs("c")
	stores := make([]*Store, writers)
	for i := range stores {
		stores[i] = openTest(t, p, ids) // each goroutine gets its own Store on the same file
	}
	var wg sync.WaitGroup
	errs := make([]error, writers)
	for w := range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range per {
				errs[w] = stores[w].WithTx(context.Background(), "", func(tx *Tx) error {
					_, err := tx.InsertProject("p", fmt.Sprintf("/r/%d/%d", w, i))
					return err
				})
				if errs[w] != nil {
					return
				}
			}
		}()
	}
	wg.Wait()
	for _, err := range errs {
		require.NoError(t, err)
	}
	require.Equal(t, writers*per, countProjects(t, stores[0]))
}

// TestHelperWriter is re-executed by TestWithTx_MultiProcess; it is a no-op
// in a normal test run.
func TestHelperWriter(t *testing.T) {
	path := os.Getenv("COWFLOW_TEST_DB")
	if path == "" {
		t.Skip("helper process only")
	}
	id := os.Getenv("COWFLOW_TEST_ID")
	s, err := Open(context.Background(), path, Options{IDs: newTestIDs("p" + id)})
	require.NoError(t, err)
	defer func() { _ = s.Close() }()
	for i := range 50 {
		require.NoError(t, s.WithTx(context.Background(), "", func(tx *Tx) error {
			_, err := tx.InsertProject("p", fmt.Sprintf("/proc/%s/%d", id, i))
			return err
		}))
	}
}

func TestWithTx_MultiProcess(t *testing.T) {
	if testing.Short() {
		t.Skip("multi-process test skipped under -short")
	}
	const procs = 5
	p := dbPath(t)
	s := openTest(t, p, newTestIDs("m"))
	cmds := make([]*exec.Cmd, procs)
	outs := make([]*lockedBuf, procs)
	for i := range procs {
		outs[i] = &lockedBuf{}
		cmds[i] = exec.Command(os.Args[0], "-test.run=^TestHelperWriter$") //nolint:gosec // re-executes the test binary
		cmds[i].Env = append(os.Environ(), "COWFLOW_TEST_DB="+p, "COWFLOW_TEST_ID="+strconv.Itoa(i))
		cmds[i].Stdout, cmds[i].Stderr = outs[i], outs[i]
		require.NoError(t, cmds[i].Start())
	}
	for i, c := range cmds {
		require.NoError(t, c.Wait(), outs[i].String())
	}
	require.Equal(t, procs*50, countProjects(t, s))
}

type lockedBuf struct {
	mu sync.Mutex
	b  []byte
}

func (l *lockedBuf) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.b = append(l.b, p...)
	return len(p), nil
}

func (l *lockedBuf) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return string(l.b)
}
