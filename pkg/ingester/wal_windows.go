//go:build windows

package ingester

import (
	"syscall"
)

var (
	kernel32               = syscall.NewLazyDLL("kernel32.dll")
	procGetDiskFreeSpaceEx = kernel32.NewProc("GetDiskFreeSpaceExW")
)

// checkDiskUsage returns the disk usage percentage (0.0 to 1.0) for the WAL directory.
func (w *walWrapper) checkDiskUsage() (float64, error) {
	// Disable this for Windows for now
	return 0.0, nil
}
