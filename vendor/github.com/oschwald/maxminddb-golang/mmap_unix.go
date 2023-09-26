//go:build !windows && !appengine && !plan9 && !js && !wasip1
// +build !windows,!appengine,!plan9,!js,!wasip1

package maxminddb

import (
	"golang.org/x/sys/unix"
)

func mmap(fd, length int) (data []byte, err error) {
	return unix.Mmap(fd, 0, length, unix.PROT_READ, unix.MAP_SHARED)
}

func munmap(b []byte) (err error) {
	return unix.Munmap(b)
}
