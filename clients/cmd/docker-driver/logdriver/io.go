package logdriver

import (
	"encoding/binary"
	"io"

	"github.com/gogo/protobuf/proto"
)

const binaryEncodeLen = 4

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

	size := int(binary.BigEndian.Uint32(d.lenBuf))
	if len(d.buf) < size {
		d.buf = make([]byte, size)
	}

	if _, err := io.ReadFull(d.r, d.buf[:size]); err != nil {
		return err
	}
	return proto.Unmarshal(d.buf[:size], l)
}
