//go:build openbsd

package backend

import (
	"os"
	"syscall"
)

func createTimeFromFileInfo(input os.FileInfo) syscall.Timespec {
	if statT, ok := input.Sys().(*syscall.Stat_t); ok {
		return statT.Ctim
	}
	return syscall.Timespec{}
}
