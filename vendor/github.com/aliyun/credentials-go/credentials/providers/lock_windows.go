// +build windows

package providers

import (
	"syscall"
	"unsafe"
)

var (
	modkernel32      = syscall.NewLazyDLL("kernel32.dll")
	procLockFileEx   = modkernel32.NewProc("LockFileEx")
	procUnlockFileEx = modkernel32.NewProc("UnlockFileEx")
)

const (
	// LOCKFILE_EXCLUSIVE_LOCK - request exclusive lock
	lockfileExclusiveLock = 0x00000002
)

// lockFile acquires an exclusive lock on the file using Windows LockFileEx
func lockFile(fd int) error {
	// LockFileEx parameters:
	// - hFile: file handle
	// - dwFlags: LOCKFILE_EXCLUSIVE_LOCK for exclusive lock
	// - dwReserved: must be 0
	// - nNumberOfBytesToLockLow: low-order 32 bits of lock range (1 byte is enough)
	// - nNumberOfBytesToLockHigh: high-order 32 bits of lock range
	// - lpOverlapped: pointer to OVERLAPPED structure
	var overlapped syscall.Overlapped
	r1, _, err := procLockFileEx.Call(
		uintptr(fd),
		uintptr(lockfileExclusiveLock),
		0,
		1,
		0,
		uintptr(unsafe.Pointer(&overlapped)),
	)
	if r1 == 0 {
		return err
	}
	return nil
}

// unlockFile releases the lock on the file using Windows UnlockFileEx
func unlockFile(fd int) error {
	var overlapped syscall.Overlapped
	r1, _, err := procUnlockFileEx.Call(
		uintptr(fd),
		0,
		1,
		0,
		uintptr(unsafe.Pointer(&overlapped)),
	)
	if r1 == 0 {
		return err
	}
	return nil
}
