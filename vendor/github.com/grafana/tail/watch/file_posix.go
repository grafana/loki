//go:build linux || darwin || freebsd || netbsd || openbsd
// +build linux darwin freebsd netbsd openbsd

package watch

import "os"

func IsDeletePending(_ *os.File) (bool, error) {
	return false, nil
}
