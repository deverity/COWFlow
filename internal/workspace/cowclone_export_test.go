package workspace

import "context"

// readTreeSeeder is option A of U-PLAT-1, kept for tests and benchmarks only
// (SCOPE.md §23 decision 39): Git builds a fresh index from baseSHA and
// hashes every file once to refresh its stat data.
type readTreeSeeder struct{ git GitOps }

func (s readTreeSeeder) Seed(ctx context.Context, wt string, _ Golden, baseSHA string) error {
	if err := s.git.ReadTree(ctx, wt, baseSHA); err != nil {
		return err
	}
	// -C <wt> selects the worktree's own git directory and index.
	return s.git.RefreshIndex(ctx, wt, wt, "")
}

// ReadTreeSeeder returns the test-only read-tree seeder over g.
func ReadTreeSeeder(g GitOps) IndexSeeder { return readTreeSeeder{git: g} }

// SetIndexSeeder replaces the index seeder of p. Tests and benchmarks only.
func SetIndexSeeder(p *CowCloneProvider, s IndexSeeder) { p.seeder = s }
