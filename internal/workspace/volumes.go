package workspace

import (
	"context"

	"github.com/deverity/cowflow/internal/platform"
)

// Volumes reports the volume and filesystem under a path (sprint-1.md §6.3).
// Capabilities and Inspect probe through it at call time, so a test can
// script the filesystem with workspacetest.FakeVolumes.
type Volumes interface {
	VolumeOf(ctx context.Context, path string) (platform.Volume, error)
	SameVolume(ctx context.Context, a, b string) (bool, error)
}

// PlatformVolumes is the production Volumes, a thin adapter over
// internal/platform.
type PlatformVolumes struct{}

// VolumeOf calls platform.VolumeOf.
func (PlatformVolumes) VolumeOf(ctx context.Context, path string) (platform.Volume, error) {
	return platform.VolumeOf(ctx, path)
}

// SameVolume calls platform.SameVolume.
func (PlatformVolumes) SameVolume(ctx context.Context, a, b string) (bool, error) {
	return platform.SameVolume(ctx, a, b)
}
