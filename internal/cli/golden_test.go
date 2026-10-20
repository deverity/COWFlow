package cli

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/deverity/cowflow/internal/domain"
	"github.com/deverity/cowflow/internal/workspace"
)

func goldenInfo(sha string, size int64, files int, state domain.GoldenState, inUse int, used time.Time) workspace.GoldenInfo {
	return workspace.GoldenInfo{
		Golden: workspace.Golden{
			ProjectID: "prj_X", BaseSHA: sha, Dir: "/ws/golden/" + sha, SizeBytes: size, FileCount: files,
		},
		ID: "gld_" + sha, State: state, InUse: inUse, LastUsedAt: used,
	}
}

func TestGoldenListJSONGolden(t *testing.T) {
	e := newOpenEnv(t)
	e.golden.infos = []workspace.GoldenInfo{
		goldenInfo("aaaa1111", 1288490189, 60123, domain.GoldenReady, 0, time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)),
		goldenInfo("bbbb2222", 52428800, 2000, domain.GoldenBuilding, 1, time.Time{}),
	}
	r := e.run("golden", "list", "--json")
	require.Equal(t, ExitOK, r.code, r.stderr)
	checkGolden(t, "golden_list_json.golden", r.stdout)

	var docs []map[string]any
	require.NoError(t, json.Unmarshal([]byte(r.stdout), &docs))
	require.Len(t, docs, 2)
	assert.InDelta(t, 1288490189, docs[0]["size_bytes"], 0)
	assert.Equal(t, "/ws/golden/aaaa1111", docs[0]["path"])
	assert.Equal(t, "gld_aaaa1111", docs[0]["id"])
	assert.Equal(t, "prj_X", e.golden.gotProj)
}

func TestGoldenListHumanAndEmpty(t *testing.T) {
	e := newOpenEnv(t)
	r := e.run("golden", "list", "--json")
	require.Equal(t, ExitOK, r.code, r.stderr)
	assert.JSONEq(t, `[]`, r.stdout, "an empty cache is an empty array")

	e.golden.infos = []workspace.GoldenInfo{
		goldenInfo("aaaa1111", 1288490189, 60123, domain.GoldenReady, 0, time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)),
	}
	r = e.run("golden", "list")
	require.Equal(t, ExitOK, r.code, r.stderr)
	assert.Regexp(t, `ID\s+SHA\s+SIZE\s+FILES\s+STATE\s+IN_USE\s+LAST_USED`, r.stdout)
	assert.Regexp(t, `gld_aaaa1111\s+aaaa1111\s+1\.2 GB\s+60123\s+ready\s+0\s+2026-03-01T10:00:00Z`, r.stdout)
}

func TestGoldenGCRespectsBudgetAndInUse(t *testing.T) {
	e := newOpenEnv(t) // golden_cache_gb = 2
	e.golden.report = workspace.GCReport{
		Removed:      []string{"aaaa1111"},
		FreedBytes:   1 << 30,
		SkippedInUse: []string{"bbbb2222"},
		Warnings:     []string{"golden tree cccc3333: in_use reset to 0"},
	}
	r := e.run("golden", "gc", "--json")
	require.Equal(t, ExitOK, r.code, r.stderr)
	assert.Equal(t, int64(2)<<30, e.golden.gotBudget, "budget is [scheduler].golden_cache_gb")
	assert.Equal(t, "prj_X", e.golden.gotProj)
	assert.Contains(t, r.stderr, "warning: golden tree cccc3333")

	var doc gcDoc
	require.NoError(t, json.Unmarshal([]byte(r.stdout), &doc))
	assert.Equal(t, []string{"aaaa1111"}, doc.Removed)
	assert.Equal(t, []string{"bbbb2222"}, doc.SkippedInUse, "in-use trees are reported, not removed")
	assert.Equal(t, int64(1<<30), doc.FreedBytes)
	assert.Empty(t, doc.Pending)

	r = e.run("golden", "gc")
	require.Equal(t, ExitOK, r.code, r.stderr)
	assert.Contains(t, r.stdout, "removed 1 golden tree(s), freed 1.0 GB")
	assert.Contains(t, r.stdout, "skipped  bbbb2222 (in use)")

	assert.Equal(t, ExitUsage, e.run("golden", "gc", "--force").code, "gc takes no flags")
}
