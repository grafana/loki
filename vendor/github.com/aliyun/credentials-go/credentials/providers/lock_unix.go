// +build !windows

package providers

import (
	"syscall"
)

// lockFile acquires an exclusive lock on the file descriptor
func lockFile(fd int) error {
	return syscall.Flock(fd, syscall.LOCK_EX)
}

// unlockFile releases the lock on the file descriptor
func unlockFile(fd int) error {
	return syscall.Flock(fd, syscall.LOCK_UN)
}
