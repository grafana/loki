package chunkenc

import (
	"io"

	"github.com/cortexproject/cortex/pkg/chunk/encoding"
)

// GzipLogChunk is a cortex encoding type for our chunks.
const GzipLogChunk = encoding.Encoding(128)

func init() {
	encoding.MustRegisterEncoding(GzipLogChunk, "GzipLogChunk", func() encoding.Chunk {
		return &Facade{
			c: NewMemChunk(EncGZIP),
		}
	})
}

// Facade for compatibility with cortex chunk type, so we can use it's chunk store.
type Facade struct {
	c Chunk
	encoding.Chunk
}

// NewFacade makes a new Facade.
func NewFacade(c Chunk) encoding.Chunk {
	return &Facade{
		c: c,
	}
}

// Marshal implements encoding.Chunk.
func (f Facade) Marshal(w io.Writer) error {
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
	f.c, err = NewByteChunk(buf)
	return err
}

// Encoding implements encoding.Chunk.
func (Facade) Encoding() encoding.Encoding {
	return GzipLogChunk
}

// LokiChunk returns the chunkenc.Chunk.
func (f Facade) LokiChunk() Chunk {
	return f.c
}
