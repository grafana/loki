package wire

import (
	"io"
)

// FrameProtocol handles reading and writing frames over a connection.
// FrameProtocol is stateful and bound to specific reader/writer during initialization.
// It is not safe for concurrent use.
type FrameProtocol interface {
	// BindWriter binds the protocol to a writer. Must be called before WriteFrame.
	BindWriter(w io.Writer)

	// BindReader binds the protocol to a reader. Must be called before ReadFrame.
	BindReader(r io.Reader)

	// WriteFrame writes a frame to the bound writer.
	WriteFrame(frame Frame) error

	// ReadFrame reads a frame from the bound reader.
	ReadFrame() (Frame, error)
}
