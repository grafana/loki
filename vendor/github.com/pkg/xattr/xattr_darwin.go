//go:build darwin
// +build darwin

package xattr

import (
	"os"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

// See https://opensource.apple.com/source/xnu/xnu-1504.15.3/bsd/sys/xattr.h.auto.html
const (
	// XATTR_SUPPORTED will be true if the current platform is supported
	XATTR_SUPPORTED = true

	XATTR_NOFOLLOW        = 0x0001
	XATTR_CREATE          = 0x0002
	XATTR_REPLACE         = 0x0004
	XATTR_NOSECURITY      = 0x0008
	XATTR_NODEFAULT       = 0x0010
	XATTR_SHOWCOMPRESSION = 0x0020

	// ENOATTR is not exported by the syscall package on Linux, because it is
	// an alias for ENODATA. We export it here so it is available on all
	// our supported platforms.
	ENOATTR = syscall.ENOATTR
)

func getxattr(path string, name string, data []byte) (int, error) {
	return unix.Getxattr(path, name, data)
}

func lgetxattr(path string, name string, data []byte) (int, error) {
	return unix.Lgetxattr(path, name, data)
}

func fgetxattr(f *os.File, name string, data []byte) (int, error) {
	path, err := getPath(f)
	if err != nil {
		return 0, err
	}
	return getxattr(path, name, data)
}

func setxattr(path string, name string, data []byte, flags int) error {
	return unix.Setxattr(path, name, data, flags)
}

func lsetxattr(path string, name string, data []byte, flags int) error {
	return unix.Lsetxattr(path, name, data, flags)
}

func fsetxattr(f *os.File, name string, data []byte, flags int) error {
	path, err := getPath(f)
	if err != nil {
		return err
	}
	return setxattr(path, name, data, flags)
}

func removexattr(path string, name string) error {
	return unix.Removexattr(path, name)
}

func lremovexattr(path string, name string) error {
	return unix.Lremovexattr(path, name)
}

func fremovexattr(f *os.File, name string) error {
	path, err := getPath(f)
	if err != nil {
		return err
	}
	return removexattr(path, name)
}

func listxattr(path string, data []byte) (int, error) {
	return unix.Listxattr(path, data)
}

func llistxattr(path string, data []byte) (int, error) {
	return unix.Llistxattr(path, data)
}

func flistxattr(f *os.File, data []byte) (int, error) {
	path, err := getPath(f)
	if err != nil {
		return 0, err
	}
	return listxattr(path, data)
}

// getPath returns the full path to the specified file.
func getPath(f *os.File) (string, error) {
	var buf [unix.PathMax]byte
	_, _, err := unix.Syscall(unix.SYS_FCNTL,
		uintptr(int(f.Fd())),
		uintptr(unix.F_GETPATH),
		uintptr(unsafe.Pointer(&buf[0])))
	if err != 0 {
		return "", err
	}
	n := 0
	for n < len(buf) && buf[n] != 0 {
		n++
	}
	return string(buf[:n]), nil
}

// stringsFromByteSlice converts a sequence of attributes to a []string.
// On Darwin and Linux, each entry is a NULL-terminated string.
func stringsFromByteSlice(buf []byte) (result []string) {
	offset := 0
	for index, b := range buf {
		if b == 0 {
			result = append(result, string(buf[offset:index]))
			offset = index + 1
		}
	}
	return
}
