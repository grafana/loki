package ingester

import (
	"time"

	"github.com/gogo/protobuf/proto"
	"github.com/pkg/errors"

	"github.com/grafana/loki/pkg/chunkenc"
)

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
			From:      from,
			To:        to,
			Closed:    d.closed,
			FlushedAt: d.flushed,
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
			flushed:     c.FlushedAt,
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

func decodeCheckpointRecord(rec []byte, s *Series) error {
	switch RecordType(rec[0]) {
	case CheckpointRecord:
		return proto.Unmarshal(rec[1:], s)
	default:
		return errors.Errorf("unexpected record type: %d", rec[0])
	}
}

func encodeWithTypeHeader(m proto.Message, typ RecordType, b []byte) ([]byte, error) {
	buf, err := proto.Marshal(m)
	if err != nil {
		return b, err
	}

	b = append(b[:0], byte(typ))
	b = append(b, buf...)
	return b, nil
}

type SeriesIter interface {
	Num() int
	Iter() <-chan Series
	Stop()
}

type CheckpointWriter interface {
	Log(Series) error
	Close()
}

type Checkpointer struct {
	dur    time.Duration
	iter   SeriesIter
	writer CheckpointWriter

	quit chan struct{}
}

func NewCheckpointer(dur time.Duration, iter SeriesIter, writer CheckpointWriter) *Checkpointer {
	return &Checkpointer{
		dur:    dur,
		iter:   iter,
		writer: writer,
		quit:   make(chan struct{}),
	}
}

func (c *Checkpointer) PerformCheckpoint() error {
	// signal whether checkpoint writes should be amortized or burst
	var immediate bool
	n := c.iter.Num()
	if n < 1 {
		return nil
	}

	perSeriesDuration := (95 * c.dur) / (100 * time.Duration(n))

	ticker := time.NewTicker(perSeriesDuration)
	defer ticker.Stop()
	start := time.Now()
	for s := range c.iter.Iter() {
		if err := c.writer.Log(s); err != nil {
			return err
		}

		if !immediate {
			if time.Since(start) > c.dur {
				// This indicates the checkpoint is taking too long; stop waiting
				// and flush the remaining series as fast as possible.
				immediate = true
				continue
			}
		}

		select {
		case <-c.quit:
		case <-ticker.C:
		}

	}

	return nil
}
