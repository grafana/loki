//go:build !windows && !appengine && !plan9 && !js && !wasip1 && !wasi

package maxminddb

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

type mmapENODEVError struct{}

func (mmapENODEVError) Error() string {
	return "mmap: the underlying filesystem of the specified file does not support memory mapping"
}

func (mmapENODEVError) Is(target error) bool {
	return target == errors.ErrUnsupported
}

func mmap(fd, length int) (data []byte, err error) {
	data, err = unix.Mmap(fd, 0, length, unix.PROT_READ, unix.MAP_SHARED)
	if err != nil {
		if err == unix.ENODEV {
			return nil, mmapENODEVError{}
		}
		return nil, os.NewSyscallError("mmap", err)
	}
	return data, nil
}

func munmap(b []byte) (err error) {
	if err = unix.Munmap(b); err != nil {
		return os.NewSyscallError("munmap", err)
	}
	return nil
}
