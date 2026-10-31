//go:build windows

package tsdb

import (
	"errors"
	"syscall"
)

// errSharingViolation is ERROR_SHARING_VIOLATION, which the syscall package does not define.
const errSharingViolation syscall.Errno = 32

// isTransientRenameErr reports whether err is a Windows error caused by another
// process briefly holding the file, which usually clears up on its own.
func isTransientRenameErr(err error) bool {
	return errors.Is(err, syscall.ERROR_ACCESS_DENIED) || errors.Is(err, errSharingViolation)
}
