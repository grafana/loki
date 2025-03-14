package chunkenc

import (
	"io"
	"time"

	"github.com/prometheus/common/model"

	"github.com/grafana/loki/v3/pkg/storage/chunk"
	"github.com/grafana/loki/v3/pkg/util/filter"
)

// GzipLogChunk is a cortex encoding type for our chunks.
// Deprecated: the chunk encoding/compression format is inside the chunk data.
const GzipLogChunk = chunk.Encoding(128)

// LogChunk is a cortex encoding type for our chunks.
const LogChunk = chunk.Encoding(129)

func init() {
	chunk.MustRegisterEncoding(GzipLogChunk, "GzipLogChunk", func() chunk.Data {
		return &Facade{}
	})
	chunk.MustRegisterEncoding(LogChunk, "LogChunk", func() chunk.Data {
		return &Facade{}
	})
}

// Facade for compatibility with cortex chunk type, so we can use its chunk store.
type Facade struct {
	c          Chunk
	blockSize  int
	targetSize int
	chunk.Data
}

// NewFacade makes a new Facade.
func NewFacade(c Chunk, blockSize, targetSize int) chunk.Data {
	return &Facade{
		c:          c,
		blockSize:  blockSize,
		targetSize: targetSize,
	}
}

func (f Facade) Bounds() (time.Time, time.Time) {
	return f.c.Bounds()
}

// Marshal implements chunk.Chunk.
func (f Facade) Marshal(w io.Writer) error {
	if f.c == nil {
		return nil
	}
	if _, err := f.c.WriteTo(w); err != nil {
		return err
	}
	return nil
}

// UnmarshalFromBuf implements chunk.Chunk.
func (f *Facade) UnmarshalFromBuf(buf []byte) error {
	var err error
	f.c, err = NewByteChunk(buf, f.blockSize, f.targetSize)
	return err
}

// Encoding implements chunk.Chunk.
func (Facade) Encoding() chunk.Encoding {
	return LogChunk
}

// Utilization implements encoding.Chunk.
func (f Facade) Utilization() float64 {
	if f.c == nil {
		return 0
	}
	return f.c.Utilization()
}

// Size implements encoding.Chunk, which unfortunately uses
// the Size method to refer to the byte size and not the entry count
// like chunkenc.Chunk does.
func (f Facade) Size() int {
	if f.c == nil {
		return 0
	}
	// Note this is an estimation (which is OK)
	return f.c.CompressedSize()
}

func (f Facade) UncompressedSize() int {
	if f.c == nil {
		return 0
	}
	return f.c.UncompressedSize()
}

func (f Facade) Entries() int {
	if f.c == nil {
		return 0
	}
	return f.c.Size()
}

// LokiChunk returns the chunkenc.Chunk.
func (f Facade) LokiChunk() Chunk {
	return f.c
}

func (f Facade) Rebound(start, end model.Time, filter filter.Func) (chunk.Data, error) {
	newChunk, err := f.c.Rebound(start.Time(), end.Time(), filter)
	if err != nil {
		return nil, err
	}
	return &Facade{
		c: newChunk,
	}, nil
}

// UncompressedSize is a helper function to hide the type assertion kludge when wanting the uncompressed size of the Cortex interface encoding.Chunk.
func UncompressedSize(c chunk.Data) (int, bool) {
	f, ok := c.(*Facade)

	if !ok || f.c == nil {
		return 0, false
	}

	return f.c.UncompressedSize(), true
}
