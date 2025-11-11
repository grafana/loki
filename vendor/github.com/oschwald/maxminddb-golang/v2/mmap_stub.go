//go:build appengine || plan9 || js || wasip1 || wasi

package maxminddb

import (
	"errors"
)

type mmapUnsupportedError struct{}

func (mmapUnsupportedError) Error() string {
	return "mmap is not supported on this platform"
}

func (mmapUnsupportedError) Is(target error) bool {
	return target == errors.ErrUnsupported
}

func mmap(_, _ int) (data []byte, err error) {
	return nil, mmapUnsupportedError{}
}

func munmap(_ []byte) (err error) {
	return mmapUnsupportedError{}
}
