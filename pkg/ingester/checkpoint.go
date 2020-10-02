package ingester

import (
	"time"

	"github.com/grafana/loki/pkg/chunkenc"
)

// Chunk is a {de,}serializable intermediate type for chunkDesc which allows
// efficient loading/unloading to disk during WAL checkpoint recovery.
type Chunk struct {
	Data    []byte
	From    time.Time
	To      time.Time
	Flushed time.Time
	Closed  bool
}

// The passed wireChunks slice is for re-use.
func toWireChunks(descs []*chunkDesc, wireChunks []Chunk) ([]Chunk, error) {
	if cap(wireChunks) < len(descs) {
		wireChunks = make([]Chunk, len(descs))
	} else {
		wireChunks = wireChunks[:len(descs)]
	}
	for i, d := range descs {
		from, to := d.chunk.Bounds()
		wireChunk := Chunk{
			From:    from,
			To:      to,
			Closed:  d.closed,
			Flushed: d.flushed,
		}

		slice := wireChunks[i].Data[:0] // try to re-use the memory from last time
		if cap(slice) < d.chunk.CompressedSize() {
			slice = make([]byte, 0, d.chunk.CompressedSize())
		}

		out, err := d.chunk.BytesWith(slice)
		if err != nil {
			return nil, err
		}

		wireChunk.Data = out
		wireChunks[i] = wireChunk
	}
	return wireChunks, nil
}

func fromWireChunks(conf *Config, wireChunks []Chunk) ([]*chunkDesc, error) {
	descs := make([]*chunkDesc, 0, len(wireChunks))
	for _, c := range wireChunks {
		desc := &chunkDesc{
			closed:      c.Closed,
			flushed:     c.Flushed,
			lastUpdated: time.Now(),
		}

		mc, err := chunkenc.NewByteChunk(c.Data, conf.BlockSize, conf.TargetChunkSize)
		if err != nil {
			return nil, err
		}
		desc.chunk = mc

		descs = append(descs, desc)
	}
	return descs, nil
}
