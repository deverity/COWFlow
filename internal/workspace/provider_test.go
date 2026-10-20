package workspace_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/deverity/cowflow/internal/git"
	"github.com/deverity/cowflow/internal/store"
	"github.com/deverity/cowflow/internal/workspace"
)

// The production adapters satisfy the consumer-side interfaces.
var (
	_ workspace.GitOps  = (*git.Client)(nil)
	_ workspace.Store   = (*store.Store)(nil)
	_ workspace.Volumes = workspace.PlatformVolumes{}
)

func TestCowKindString(t *testing.T) {
	tests := []struct {
		k    workspace.CowKind
		want string
	}{
		{workspace.CowNone, "none"},
		{workspace.CowBlock, "block"},
		{workspace.CowVirtual, "virtual"},
		{workspace.CowKind(99), "unknown(99)"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.k.String())
			assert.Equal(t, tt.want, fmt.Sprint(tt.k))
		})
	}
}

func TestCowKindMarshalJSON(t *testing.T) {
	type doc struct {
		CowKind workspace.CowKind `json:"cow_kind"`
	}
	for k, want := range map[workspace.CowKind]string{
		workspace.CowNone:    `{"cow_kind":"none"}`,
		workspace.CowBlock:   `{"cow_kind":"block"}`,
		workspace.CowVirtual: `{"cow_kind":"virtual"}`,
		255:                  `{"cow_kind":"unknown(255)"}`,
	} {
		b, err := json.Marshal(doc{CowKind: k})
		require.NoError(t, err)
		assert.JSONEq(t, want, string(b))
	}
	caps, err := json.Marshal(workspace.Capabilities{CopyOnWrite: workspace.CowBlock, Filesystem: "refs"})
	require.NoError(t, err)
	assert.Contains(t, string(caps), `"CopyOnWrite":"block"`)
}

func TestUnpreservedErrorMessageTruncates(t *testing.T) {
	var files []string
	for i := range 13 {
		files = append(files, fmt.Sprintf("src/f%02d.go", i))
	}
	err := error(&workspace.UnpreservedError{Report: workspace.DirtyReport{
		Uncommitted: files,
		Unpublished: []string{"0123456789abcdef0123456789abcdef01234567"},
	}})
	msg := err.Error()
	for _, f := range files[:10] {
		assert.Contains(t, msg, `"`+f+`"`)
	}
	for _, f := range files[10:] {
		assert.NotContains(t, msg, f)
	}
	assert.Contains(t, msg, "13 uncommitted files")
	assert.Contains(t, msg, "… and 3 more")
	assert.Contains(t, msg, `1 unpublished commit: "0123456789abcdef0123456789abcdef01234567"`)
	assert.Equal(t, 1, strings.Count(msg, "more"))

	var ue *workspace.UnpreservedError
	require.ErrorAs(t, fmt.Errorf("destroy: %w", err), &ue)
	assert.Len(t, ue.Report.Uncommitted, 13, "the report keeps every path")
}

func TestUnpreservedErrorQuotesOddPaths(t *testing.T) {
	err := &workspace.UnpreservedError{Report: workspace.DirtyReport{Uncommitted: []string{"a b, c\nd"}}}
	assert.Contains(t, err.Error(), `"a b, c\nd"`)
	assert.NotContains(t, err.Error(), "more")
}

func TestDirtyReportClean(t *testing.T) {
	assert.True(t, workspace.DirtyReport{}.Clean())
	assert.False(t, workspace.DirtyReport{Uncommitted: []string{"x"}}.Clean())
	assert.False(t, workspace.DirtyReport{Unpublished: []string{"abc"}}.Clean())
}

func TestSentinelsAreDistinct(t *testing.T) {
	all := []error{
		workspace.ErrNotFound, workspace.ErrPendingMount, workspace.ErrPendingCleanup,
		workspace.ErrProtectedBranch, workspace.ErrCloneUnsupported, workspace.ErrUnsupportedProvider,
	}
	for i, a := range all {
		assert.True(t, strings.HasPrefix(a.Error(), "workspace: "), a.Error())
		for j, b := range all {
			if i != j {
				assert.False(t, errors.Is(a, b), "%v is %v", a, b)
			}
		}
	}
}
