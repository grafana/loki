//go:build windows

package ingester

import (
	"syscall"
	"unsafe"
)

var (
	kernel32               = syscall.NewLazyDLL("kernel32.dll")
	procGetDiskFreeSpaceEx = kernel32.NewProc("GetDiskFreeSpaceExW")
)

// checkDiskUsage returns the disk usage percentage (0.0 to 1.0) for the WAL directory.
func (w *walWrapper) checkDiskUsage() (float64, error) {
	dirPath, err := syscall.UTF16PtrFromString(w.cfg.Dir)
	if err != nil {
		return 0, err
	}

	var freeBytesAvailable, totalNumberOfBytes, totalNumberOfFreeBytes uint64

	ret, _, err := procGetDiskFreeSpaceEx.Call(
		uintptr(unsafe.Pointer(dirPath)),
		uintptr(unsafe.Pointer(&freeBytesAvailable)),
		uintptr(unsafe.Pointer(&totalNumberOfBytes)),
		uintptr(unsafe.Pointer(&totalNumberOfFreeBytes)),
	)

	if ret == 0 {
		return 0, err
	}

	// Calculate usage percentage
	used := totalNumberOfBytes - totalNumberOfFreeBytes
	usagePercent := float64(used) / float64(totalNumberOfBytes)

	return usagePercent, nil
}
