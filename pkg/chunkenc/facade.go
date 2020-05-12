package chunkenc

import (
	"io"

	"github.com/cortexproject/cortex/pkg/chunk/encoding"
)

// GzipLogChunk is a cortex encoding type for our chunks.
// Deprecated: the chunk encoding/compression format is inside the chunk data.
const GzipLogChunk = encoding.Encoding(128)

// LogChunk is a cortex encoding type for our chunks.
const LogChunk = encoding.Encoding(129)

func init() {
	encoding.MustRegisterEncoding(GzipLogChunk, "GzipLogChunk", func() encoding.Chunk {
		return &Facade{}
	})
	encoding.MustRegisterEncoding(LogChunk, "LogChunk", func() encoding.Chunk {
		return &Facade{}
	})
}

// Facade for compatibility with cortex chunk type, so we can use its chunk store.
type Facade struct {
	c          Chunk
	blockSize  int
	targetSize int
	encoding.Chunk
}

// NewFacade makes a new Facade.
func NewFacade(c Chunk, blockSize, targetSize int) encoding.Chunk {
	return &Facade{
		c:          c,
		blockSize:  blockSize,
		targetSize: targetSize,
	}
}

// Marshal implements encoding.Chunk.
func (f Facade) Marshal(w io.Writer) error {
	if f.c == nil {
		return nil
	}
	buf, err := f.c.Bytes()
	if err != nil {
		return err
	}
	_, err = w.Write(buf)
	return err
}

// UnmarshalFromBuf implements encoding.Chunk.
func (f *Facade) UnmarshalFromBuf(buf []byte) error {
	var err error
	f.c, err = NewByteChunk(buf, f.blockSize, f.targetSize)
	return err
}

// Encoding implements encoding.Chunk.
func (Facade) Encoding() encoding.Encoding {
	return LogChunk
}

// Utilization implements encoding.Chunk.
func (f Facade) Utilization() float64 {
	if f.c == nil {
		return 0
	}
	return f.c.Utilization()
}

// Size implements encoding.Chunk.
func (f Facade) Size() int {
	if f.c == nil {
		return 0
	}
	return f.c.Size()
}

// LokiChunk returns the chunkenc.Chunk.
func (f Facade) LokiChunk() Chunk {
	return f.c
}

// UncompressedSize is a helper function to hide the type assertion kludge when wanting the uncompressed size of the Cortex interface encoding.Chunk.
func UncompressedSize(c encoding.Chunk) (int, bool) {
	f, ok := c.(*Facade)

	if !ok || f.c == nil {
		return 0, false
	}

	return f.c.UncompressedSize(), true
}
