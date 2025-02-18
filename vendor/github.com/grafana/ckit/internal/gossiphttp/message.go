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
	headerSize = 3
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
		dataLength = binary.BigEndian.Uint16(header.data[1:])
	)

	if gotMagic != magic {
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
	header := headerPool.Get().(*header)
	defer headerPool.Put(header)

	if len(message) > math.MaxUint16 {
		return fmt.Errorf("message length %d exceeds size limit %d", len(message), math.MaxUint16)
	}

	header.data[0] = magic
	binary.BigEndian.PutUint16(header.data[1:], uint16(len(message)))

	if _, err := w.Write(header.data); err != nil {
		return err
	}

	_, err := w.Write(message)
	return err
}
