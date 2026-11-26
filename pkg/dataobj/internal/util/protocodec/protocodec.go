// Package protocodec provides utilities for encoding and decoding protobuf
// messages into files.
//
// Protobuf messages are prepended with their size as a uvarint so they can be
// streamed.
package protocodec

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/gogo/protobuf/proto"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/streamio"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/util/bufpool"
)

// Size returns the number of bytes that would be required to call [Encode] for
// pb.
func Size(pb proto.Message) int {
	protoSize := proto.Size(pb)

	scratch := [binary.MaxVarintLen64]byte{}
	uvarintSize := binary.PutUvarint(scratch[:], uint64(protoSize))

	return uvarintSize + protoSize
}

// Encode encodes a protobuf message into w. Encoded messages can be decoded
// with [Decode] or [DecodeBytes].
func Encode(w streamio.Writer, pb proto.Message) error {
	buf := protoBufferPool.Get().(*proto.Buffer)
	buf.Reset()
	defer protoBufferPool.Put(buf)

	if err := buf.Marshal(pb); err != nil {
		return err
	}

	// Every protobuf message is always prepended with its size as a uvarint.
	messageSize := len(buf.Bytes())
	if err := streamio.WriteUvarint(w, uint64(messageSize)); err != nil {
		return err
	}

	sz, err := w.Write(buf.Bytes())
	if err != nil {
		return err
	} else if sz != messageSize {
		return io.ErrShortWrite
	}

	return nil
}

// maxDecodeSizeHint caps the buffer that [Decode] reserves for a message before it reads the
// message.
//
// The size of a message comes from the input. A corrupt size would force a huge allocation or a
// panic. A message larger than the cap still decodes, because its buffer grows while Decode reads
// it.
const maxDecodeSizeHint = 1 << 20

// Decode decodes a message encoded with protocodec from r and stores it in pb.
func Decode(r streamio.Reader, pb proto.Message) error {
	size, err := binary.ReadUvarint(r)
	if err != nil {
		return fmt.Errorf("read proto message size: %w", err)
	}

	buf := bufpool.Get(int(min(size, maxDecodeSizeHint)))
	defer bufpool.Put(buf)

	n, err := io.Copy(buf, io.LimitReader(r, int64(size)))
	if err != nil {
		return fmt.Errorf("read proto message: %w", err)
	} else if uint64(n) != size {
		return fmt.Errorf("read proto message: got=%d want=%d", n, size)
	}

	if err := proto.Unmarshal(buf.Bytes(), pb); err != nil {
		return fmt.Errorf("unmarshal proto message: %w", err)
	}
	return nil
}

// DecodeBytes decodes a message encoded with protocodec from the start of b and
// stores it in pb.
//
// The message in pb does not alias b, so the caller can reuse b after the call.
// DecodeBytes ignores the bytes after the message. It returns an error if b has
// no valid size, is shorter than the message, or holds a message that does not
// unmarshal.
func DecodeBytes(b []byte, pb proto.Message) error {
	size, n := binary.Uvarint(b)
	if n <= 0 {
		return errors.New("read proto message size: the size is truncated or too large")
	}
	if size > uint64(len(b)-n) {
		return fmt.Errorf("read proto message: got=%d want=%d", len(b)-n, size)
	}

	if err := proto.Unmarshal(b[n:n+int(size)], pb); err != nil {
		return fmt.Errorf("unmarshal proto message: %w", err)
	}
	return nil
}

var protoBufferPool = sync.Pool{
	New: func() any {
		return new(proto.Buffer)
	},
}
