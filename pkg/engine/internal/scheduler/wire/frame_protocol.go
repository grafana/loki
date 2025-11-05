package wire

import (
	"io"
)

// FrameProtocol handles reading and writing frames over a connection.
type FrameProtocol interface {
	// WriteFrame writes a frame to the [io.Writer]
	WriteFrame(w io.Writer, frame Frame) error

	// ReadFrame reads a frame from the [io.Reader]
	ReadFrame(r io.Reader) (Frame, error)
}
