package util //nolint:revive

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
)

// EnsureDirectory makes sure directory is there, if not creates it if not
func EnsureDirectory(dir string) error {
	return EnsureDirectoryWithDefaultPermissions(dir, 0o777)
}

func EnsureDirectoryWithDefaultPermissions(dir string, mode fs.FileMode) error {
	info, err := os.Stat(dir)
	if os.IsNotExist(err) {
		return os.MkdirAll(dir, mode)
	} else if err == nil && !info.IsDir() {
		return fmt.Errorf("not a directory: %s", dir)
	}
	return err
}

func RequirePermissions(path string, required fs.FileMode) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}

	if mode := info.Mode(); mode&required != required {
		return fmt.Errorf("insufficient permissions for path %s: required %s but found %s", path, required.String(), mode.String())
	}
	return nil
}

// ReadCloserWithContextCancelFunc helps with cancelling the context when closing a ReadCloser.
// NOTE: The consumer of ReadCloserWithContextCancelFunc should always call the Close method when it is done reading which otherwise could cause a resource leak.
type ReadCloserWithContextCancelFunc struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func NewReadCloserWithContextCancelFunc(readCloser io.ReadCloser, cancel context.CancelFunc) io.ReadCloser {
	return ReadCloserWithContextCancelFunc{
		ReadCloser: readCloser,
		cancel:     cancel,
	}
}

func (r ReadCloserWithContextCancelFunc) Close() error {
	defer r.cancel()
	return r.ReadCloser.Close()
}
