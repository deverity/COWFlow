package workspace

import "context"

// Cloner is the block-clone seam used by the cow_clone provider (sprint-1.md
// §6.2). The production adapter calls the per-OS cloneFile primitive in
// cow_<goos>.go (owned by AG-PLAT); tests use workspacetest.FakeCloner.
//
// Contract:
//  1. dst must not exist; CloneFile creates it exclusively and never
//     overwrites.
//  2. The caller guarantees src and dst are on the same volume.
//  3. On success dst has src's bytes, size, permission bits and modification
//     time (the index stat check depends on the mtime).
//  4. It never falls back to a byte copy: an unsupported filesystem, volume
//     or flag is an error wrapping ErrCloneUnsupported.
//  5. On any error dst is removed; no partial file is left behind.
//  6. ctx is checked between chunks; a single clone call is not
//     interruptible.
type Cloner interface {
	CloneFile(ctx context.Context, src, dst string) error
}
