//go:build solaris
// +build solaris

package uv

import "io"

// newPollReader creates a new pollReader for the given io.Reader.
// On Solaris, we use the select API.
func newPollReader(reader io.Reader) (pollReader, error) {
	return newSelectPollReader(reader)
}
