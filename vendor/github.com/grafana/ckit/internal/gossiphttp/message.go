package gossiphttp

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"sync"
)

const (
	// magic is the first byte sent with every message.
	magic      = 0xcc
	magic32    = 0xcd
	headerSize = 5 // 1 byte magic + 2 bytes length of the payload (16-bit) or 4 bytes length of the payload (32-bit)
)

var headerPool = &sync.Pool{
	New: func() any {
		return &header{
			data: make([]byte, headerSize),
		}
	},
}

type header struct {
	data []byte
}

// readMessage reads a message from an [io.Reader].
func readMessage(r io.Reader) ([]byte, error) {
	header := headerPool.Get().(*header)
	defer headerPool.Put(header)
	if _, err := io.ReadFull(r, header.data); err != nil {
		return nil, err
	}

	var (
		gotMagic   = header.data[0]
		dataLength uint32
	)

	switch gotMagic {
	case magic:
		dataLength = uint32(binary.BigEndian.Uint16(header.data[1:]))
	case magic32:
		dataLength = binary.BigEndian.Uint32(header.data[1:])
	default:
		return nil, fmt.Errorf("invalid magic (%x)", gotMagic)
	}

	data := make([]byte, dataLength)
	if _, err := io.ReadFull(r, data); err != nil {
		return nil, err
	}
	return data, nil
}

// writeMessage writes a message to an [io.Writer].
func writeMessage(w io.Writer, message []byte) error {
	// Note(salvacorts): We need to cast to uint to avoid overflows on 32-bit systems (e.g. arm docker images)
	if uint(len(message)) > math.MaxUint32 {
		return fmt.Errorf("message length %d exceeds size limit %d", len(message), uint32(math.MaxUint32))
	}

	header := headerPool.Get().(*header)
	defer headerPool.Put(header)

	if len(message) <= math.MaxUint16 {
		header.data[0] = magic
		binary.BigEndian.PutUint16(header.data[1:], uint16(len(message)))
	} else {
		header.data[0] = magic32
		binary.BigEndian.PutUint32(header.data[1:], uint32(len(message)))
	}

	if _, err := w.Write(header.data); err != nil {
		return err
	}

	_, err := w.Write(message)
	return err
}
