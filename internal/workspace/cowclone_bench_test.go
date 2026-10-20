package workspace_test

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/deverity/cowflow/internal/config"
	"github.com/deverity/cowflow/internal/domain"
	"github.com/deverity/cowflow/internal/git"
	"github.com/deverity/cowflow/internal/platform"
	"github.com/deverity/cowflow/internal/workspace"
	"github.com/deverity/cowflow/internal/workspace/workspacetest"
	"github.com/deverity/cowflow/test/fixtures/benchrepo"
)

// BenchmarkCowCloneCreate measures cow_clone Create on the S1-T19 small
// profile with a warm golden tree. With COWFLOW_TEST_COW_DIR or
// COWFLOW_TEST_REFS_DIR set it block-clones on that volume; otherwise it
// byte-copies through FakeCloner, which measures the walk and Git but not
// the filesystem. The seeder sub-benchmarks give S1-T21 both index options.
func BenchmarkCowCloneCreate(b *testing.B) {
	base, onRealFS := ccRealDir(b)
	var vols workspace.Volumes = workspace.PlatformVolumes{}
	var cloner workspace.Cloner // nil: the OS primitive
	if !onRealFS {
		var err error
		base, err = filepath.EvalSymlinks(b.TempDir())
		require.NoError(b, err)
		vols = workspacetest.NewFakeVolumes(platform.Volume{Filesystem: "btrfs", ID: "vol", MountPath: base})
		cloner = &workspacetest.FakeCloner{}
	}
	repo := filepath.Join(base, "repo")
	ctx := context.Background()
	rep, err := benchrepo.Generate(ctx, repo, benchrepo.Small, 1)
	require.NoError(b, err)
	sha := rep.Branches["main"]
	require.NotEmpty(b, sha)

	home := b.TempDir()
	gcfg := filepath.Join(home, "gitconfig")
	require.NoError(b, os.WriteFile(gcfg, nil, 0o600))
	runner, err := git.NewExecRunner(git.ExecOptions{Timeout: 5 * time.Minute, ExtraEnv: []string{
		"GIT_CONFIG_GLOBAL=" + gcfg, "GIT_CONFIG_NOSYSTEM=1", "HOME=" + home, "USERPROFILE=" + home,
	}})
	require.NoError(b, err)
	client := git.NewClient(runner)

	wsRoot := filepath.Join(base, "ws")
	require.NoError(b, os.Mkdir(wsRoot, 0o750))
	cfg := config.Default()
	cfg.Workspace.Root = wsRoot
	log := slog.New(slog.DiscardHandler)
	golden := workspace.NewGoldenCache(workspace.GoldenDeps{
		Config: cfg, Store: workspacetest.NewMemStore(domain.SystemClock{}), Git: client, Volumes: vols, Logger: log,
	})
	_, release, err := golden.Acquire(ctx, "prj_1", repo, sha) // warm the golden tree
	require.NoError(b, err)
	release()

	for _, seeder := range []string{"golden-index", "read-tree"} {
		b.Run(seeder, func(b *testing.B) {
			p := workspace.NewCowCloneProvider(workspace.CowCloneDeps{
				Config: cfg, Git: client, Golden: golden, Volumes: vols, Cloner: cloner, Logger: log,
			})
			if seeder == "read-tree" {
				workspace.SetIndexSeeder(p, workspace.ReadTreeSeeder(client))
			}
			n := 0
			for b.Loop() {
				n++
				slug := fmt.Sprintf("%s-%d", seeder, n)
				_, err := p.Create(ctx, workspace.WorkspaceSpec{
					ID: "wks_" + slug, ProjectID: "prj_1", RepoDir: repo,
					Path: filepath.Join(wsRoot, slug), Branch: "feature/" + slug, BaseSHA: sha,
				})
				require.NoError(b, err)
			}
		})
	}
}
