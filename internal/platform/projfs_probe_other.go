//go:build !windows

package platform

import "context"

// ProjFSAvailable is always false off Windows: ProjFS is a Windows feature.
func (h *host) ProjFSAvailable(ctx context.Context) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	return false, nil
}
