package workspace

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// cfgRecorder records ConfigSetWorktree calls; every other GitOps method is
// unused and panics on the nil embedded interface.
type cfgRecorder struct {
	GitOps
	set [][2]string
}

func (c *cfgRecorder) ConfigSetWorktree(_ context.Context, _, key, value string) error {
	c.set = append(c.set, [2]string{key, value})
	return nil
}

func TestWorktreeConfigByOS(t *testing.T) {
	t.Parallel()
	for goos, want := range map[string][][2]string{
		"linux":   {{"core.autocrlf", "false"}},
		"windows": {{"core.autocrlf", "false"}, {"core.longpaths", "true"}},
	} {
		rec := &cfgRecorder{}
		p := NewWorktreeProvider(rec, nil)
		p.goos = goos
		require.NoError(t, p.configure(context.Background(), "/ws"))
		assert.Equal(t, want, rec.set, goos)
	}
}
