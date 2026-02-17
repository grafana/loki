//go:build !windows

package kgo

import (
	"syscall"
)

func isConnReset(errno syscall.Errno) bool {
	return errno == syscall.ECONNRESET
}
