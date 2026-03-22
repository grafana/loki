// Package streamio defines interfaces shared by other packages for streaming
// binary data.
package streamio

import "io"

// Reader is an interface that combines an [io.Reader] and an [io.ByteReader].
type Reader interface {
	io.Reader
	io.ByteReader
}

// Writer is an interface that combines an [io.Writer] and an [io.ByteWriter].
type Writer interface {
	io.Writer
	io.ByteWriter
}

// Discard is a [Writer] for which all calls succeed without doing anything.
var Discard Writer = discard{}

type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }
func (discard) WriteByte(_ byte) error      { return nil }
