package codec

import (
	"encoding/binary"
	"fmt"
	"io"
)

// maxFrameSize is a sanity cap on incoming frames (100 MiB).
// Real Kafka brokers use message.max.bytes (default 1 MiB); we are generous
// to avoid spurious rejections during integration tests.
const maxFrameSize = 100 * 1024 * 1024

// ReadFrame reads one complete Kafka frame from r.
//
// A Kafka frame is a 4-byte big-endian length prefix followed by that many
// payload bytes.  ReadFrame returns only the payload (the 4-byte prefix is
// consumed but not included in the return value).
func ReadFrame(r io.Reader) ([]byte, error) {
	var hdr [4]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return nil, err
	}
	n := int(binary.BigEndian.Uint32(hdr[:]))
	if n < 0 || n > maxFrameSize {
		return nil, fmt.Errorf("codec: invalid frame length %d", n)
	}
	body := make([]byte, n)
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, err
	}
	return body, nil
}

// WriteFrame writes body to w as a length-prefixed Kafka frame.
//
// The 4-byte big-endian length prefix is written first, followed by body.
func WriteFrame(w io.Writer, body []byte) error {
	var hdr [4]byte
	binary.BigEndian.PutUint32(hdr[:], uint32(len(body)))
	if _, err := w.Write(hdr[:]); err != nil {
		return err
	}
	_, err := w.Write(body)
	return err
}
