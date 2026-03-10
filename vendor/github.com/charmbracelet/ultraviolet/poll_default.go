//go:build !linux && !windows && !darwin && !freebsd && !netbsd && !openbsd && !dragonfly && !solaris
// +build !linux,!windows,!darwin,!freebsd,!netbsd,!openbsd,!dragonfly,!solaris

package uv

import "io"

// newPollReader creates a new pollReader for the given io.Reader.
// This is the default implementation for unsupported platforms.
func newPollReader(reader io.Reader) (pollReader, error) {
	return newFallbackReader(reader)
}
