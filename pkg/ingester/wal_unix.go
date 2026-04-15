//go:build !windows

package ingester

import (
	"syscall"
)

// checkDiskUsage returns the disk usage percentage (0.0 to 1.0) for the WAL directory.
func (w *walWrapper) checkDiskUsage() (float64, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(w.cfg.Dir, &stat); err != nil {
		return 0, err
	}

	// Calculate usage percentage
	total := stat.Blocks * uint64(stat.Bsize)
	free := stat.Bfree * uint64(stat.Bsize)
	used := total - free
	usagePercent := float64(used) / float64(total)

	return usagePercent, nil
}
