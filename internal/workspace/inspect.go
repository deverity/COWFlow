package workspace

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/deverity/cowflow/internal/config"
	"github.com/deverity/cowflow/internal/domain"
	"github.com/deverity/cowflow/internal/platform"
	"github.com/deverity/cowflow/internal/store"
)

// Inspector is the shared inspect and dirty-check logic every provider
// delegates to (sprint-1.md S1-T13). It reads the filesystem and Git at call
// time and trusts the stored row only for identity.
type Inspector struct {
	store     Store
	git       GitOps
	vols      Volumes
	namespace string // [git].shared_ref_namespace, read only from config
	stateDir  string // [daemon].state_dir, for the logs directory
}

// NewInspector returns an Inspector over s, g and v. A nil v means
// [PlatformVolumes]; a nil cfg leaves the ref namespace and state dir empty.
func NewInspector(s Store, g GitOps, v Volumes, cfg *config.Config) *Inspector {
	if v == nil {
		v = PlatformVolumes{}
	}
	in := &Inspector{store: s, git: g, vols: v}
	if cfg != nil {
		in.namespace = cfg.Git.SharedRefNamespace
		in.stateDir = cfg.Daemon.StateDir
	}
	return in
}

// Inspect reports workspace id as provider delivered it: the row's identity,
// the filesystem probed now from the path, cow the copy-on-write kind the
// provider delivers, HEAD and the dirty state read from Git now.
//
// A path whose volume is not mounted gives the state pending_mount and an
// error wrapping [ErrPendingMount]; the status is returned alongside it. A
// missing path, or a filesystem that differs from the recorded one, is a
// warning.
func (in *Inspector) Inspect(ctx context.Context, provider string, cow CowKind, id string) (*WorkspaceStatus, error) {
	if in == nil || in.store == nil || in.git == nil {
		return nil, fmt.Errorf("inspect workspace %s: %w: no inspector", id, errNotConfigured)
	}
	r, err := in.store.GetWorkspace(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return nil, fmt.Errorf("workspace %s: %w", id, ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("workspace %s: %w", id, err)
	}
	if r.Provider != provider {
		return nil, fmt.Errorf("inspect workspace %s: provider %q is not %q: %w", id, r.Provider, provider, ErrUnsupportedProvider)
	}
	w := recordToWorkspace(r)
	st := &WorkspaceStatus{Workspace: w, CowKind: cow, LogsDir: in.logsDir(id)}

	vol, missing, err := in.probe(ctx, w.Path)
	switch {
	case errors.Is(err, ErrPendingMount):
		st.State = domain.WorkspacePendingMount
		st.Warnings = append(st.Warnings, fmt.Sprintf("volume of %s is not mounted", w.Path))
		return st, fmt.Errorf("inspect workspace %s: %w", id, err)
	case err != nil:
		return nil, fmt.Errorf("inspect workspace %s: %w", id, err)
	}
	st.Filesystem = vol.Filesystem
	if r.Filesystem != "" && vol.Filesystem != r.Filesystem {
		st.Warnings = append(st.Warnings, fmt.Sprintf(
			"filesystem is %q now, recorded %q at create", vol.Filesystem, r.Filesystem))
	}
	if missing {
		st.Warnings = append(st.Warnings, fmt.Sprintf("workspace path %s is missing", w.Path))
		return st, nil
	}
	if st.Head, err = in.git.Head(ctx, w.Path); err != nil {
		return nil, fmt.Errorf("inspect workspace %s: head: %w", id, err)
	}
	if st.Dirty, err = in.Dirty(ctx, w); err != nil {
		return nil, fmt.Errorf("inspect workspace %s: %w", id, err)
	}
	return st, nil
}

// probe returns the volume under path. When the path does not exist it
// probes the parent directory and reports missing; a volume that cannot be
// read at all is [ErrPendingMount].
func (in *Inspector) probe(ctx context.Context, path string) (vol platform.Volume, missing bool, err error) {
	probed := path
	if _, serr := os.Lstat(path); errors.Is(serr, fs.ErrNotExist) {
		missing, probed = true, filepath.Dir(path)
	}
	vol, err = in.vols.VolumeOf(ctx, probed)
	switch {
	case err == nil:
		return vol, missing, nil
	case notMounted(err):
		return platform.Volume{}, false, fmt.Errorf("%w: %w", ErrPendingMount, err)
	}
	return platform.Volume{}, false, err
}

func notMounted(err error) bool {
	return errors.Is(err, platform.ErrNotMounted) || errors.Is(err, fs.ErrNotExist)
}

func (in *Inspector) logsDir(id string) string {
	if in.stateDir == "" {
		return ""
	}
	return LogsDir(in.stateDir, id)
}
