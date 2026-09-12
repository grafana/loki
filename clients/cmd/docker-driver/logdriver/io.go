package logdriver

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"github.com/gogo/protobuf/proto"
)

// ErrPayloadUnmarshal reports a frame whose payload could not be unmarshalled.
// The frame was read in full first, so the stream is still aligned on a frame
// boundary and decoding can continue with the next one. Every other Decode
// error leaves the offset unknown.
var ErrPayloadUnmarshal = errors.New("log entry payload could not be unmarshalled")

const binaryEncodeLen = 4

// maxFrameSize bounds the payload a single frame may declare. Decode sizes its
// buffer from the length prefix before any protobuf validation, so without this
// a corrupt frame forces an arbitrary allocation. Matches the limit the
// gogo/protobuf reader this replaced was constructed with.
const maxFrameSize uint32 = 1e6

// LogEntryEncoder encodes a LogEntry to a protobuf stream.
// The stream format is [uint32 big-endian size][protobuf message].
type LogEntryEncoder interface {
	Encode(*LogEntry) error
}

// NewLogEntryEncoder creates a protobuf stream encoder for log entries.
func NewLogEntryEncoder(w io.Writer) LogEntryEncoder {
	return &logEntryEncoder{
		w:   w,
		buf: make([]byte, 1024),
	}
}

type logEntryEncoder struct {
	buf []byte
	w   io.Writer
}

func (e *logEntryEncoder) Encode(l *LogEntry) error {
	payload, err := proto.Marshal(l)
	if err != nil {
		return err
	}
	total := len(payload) + binaryEncodeLen
	if total > len(e.buf) {
		e.buf = make([]byte, total)
	}
	binary.BigEndian.PutUint32(e.buf, uint32(len(payload)))
	copy(e.buf[binaryEncodeLen:], payload)
	_, err = e.w.Write(e.buf[:total])
	return err
}

// LogEntryDecoder decodes log entries from a stream encoded by LogEntryEncoder.
type LogEntryDecoder interface {
	Decode(*LogEntry) error
}

// NewLogEntryDecoder creates a new stream decoder for log entries.
func NewLogEntryDecoder(r io.Reader) LogEntryDecoder {
	return &logEntryDecoder{
		lenBuf: make([]byte, binaryEncodeLen),
		buf:    make([]byte, 1024),
		r:      r,
	}
}

type logEntryDecoder struct {
	r      io.Reader
	lenBuf []byte
	buf    []byte
}

func (d *logEntryDecoder) Decode(l *LogEntry) error {
	if _, err := io.ReadFull(d.r, d.lenBuf); err != nil {
		return err
	}

	// Compared as uint32: on a 32-bit build int(size) would wrap negative and
	// slip past the bound.
	frameSize := binary.BigEndian.Uint32(d.lenBuf)
	if frameSize > maxFrameSize {
		return fmt.Errorf("log entry frame size %d exceeds maximum %d", frameSize, maxFrameSize)
	}
	size := int(frameSize)
	if len(d.buf) < size {
		d.buf = make([]byte, size)
	}

	if _, err := io.ReadFull(d.r, d.buf[:size]); err != nil {
		return err
	}
	if err := proto.Unmarshal(d.buf[:size], l); err != nil {
		return fmt.Errorf("%w: %w", ErrPayloadUnmarshal, err)
	}
	return nil
}
