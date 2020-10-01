package ingester

import (
	"time"

	"github.com/grafana/loki/pkg/chunkenc"
	"github.com/grafana/loki/pkg/logproto"
)

// The passed wireChunks slice is for re-use.
func toWireChunks(descs []*chunkDesc, wireChunks []logproto.Chunk) ([]logproto.Chunk, error) {
	if cap(wireChunks) < len(descs) {
		wireChunks = make([]logproto.Chunk, len(descs))
	} else {
		wireChunks = wireChunks[:len(descs)]
	}
	for i, d := range descs {
		from, to := d.chunk.Bounds()
		wireChunk := logproto.Chunk{
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

func fromWireChunks(conf *Config, wireChunks []logproto.Chunk) ([]*chunkDesc, error) {
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
