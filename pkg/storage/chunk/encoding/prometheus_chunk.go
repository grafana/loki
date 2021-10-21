package encoding

import (
	"io"

	"github.com/pkg/errors"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/tsdb/chunkenc"
)

// Wrapper around Prometheus chunk.
type prometheusXorChunk struct {
	chunk chunkenc.Chunk
}

func newPrometheusXorChunk() *prometheusXorChunk {
	return &prometheusXorChunk{}
}

// Add adds another sample to the chunk. While Add works, it is only implemented
// to make tests work, and should not be used in production. In particular, it appends
// all samples to single chunk, and uses new Appender for each Add.
func (p *prometheusXorChunk) Add(m model.SamplePair) (Chunk, error) {
	if p.chunk == nil {
		p.chunk = chunkenc.NewXORChunk()
	}

	app, err := p.chunk.Appender()
	if err != nil {
		return nil, err
	}

	app.Append(int64(m.Timestamp), float64(m.Value))
	return nil, nil
}

func (p *prometheusXorChunk) NewIterator(iterator Iterator) Iterator {
	if p.chunk == nil {
		return errorIterator("Prometheus chunk is not set")
	}

	if pit, ok := iterator.(*prometheusChunkIterator); ok {
		pit.c = p.chunk
		pit.it = p.chunk.Iterator(pit.it)
		return pit
	}

	return &prometheusChunkIterator{c: p.chunk, it: p.chunk.Iterator(nil)}
}

func (p *prometheusXorChunk) Marshal(i io.Writer) error {
	if p.chunk == nil {
		return errors.New("chunk data not set")
	}
	_, err := i.Write(p.chunk.Bytes())
	return err
}

func (p *prometheusXorChunk) UnmarshalFromBuf(bytes []byte) error {
	c, err := chunkenc.FromData(chunkenc.EncXOR, bytes)
	if err != nil {
		return errors.Wrap(err, "failed to create Prometheus chunk from bytes")
	}

	p.chunk = c
	return nil
}

func (p *prometheusXorChunk) Encoding() Encoding {
	return PrometheusXorChunk
}

func (p *prometheusXorChunk) Utilization() float64 {
	// Used for reporting when chunk is used to store new data.
	return 0
}

func (p *prometheusXorChunk) Slice(_, _ model.Time) Chunk {
	return p
}

func (p *prometheusXorChunk) Rebound(from, to model.Time) (Chunk, error) {
	return nil, errors.New("Rebound not supported by PrometheusXorChunk")
}

func (p *prometheusXorChunk) Len() int {
	if p.chunk == nil {
		return 0
	}
	return p.chunk.NumSamples()
}

func (p *prometheusXorChunk) Size() int {
	if p.chunk == nil {
		return 0
	}
	return len(p.chunk.Bytes())
}

type prometheusChunkIterator struct {
	c  chunkenc.Chunk // we need chunk, because FindAtOrAfter needs to start with fresh iterator.
	it chunkenc.Iterator
}

func (p *prometheusChunkIterator) Scan() bool {
	return p.it.Next()
}

func (p *prometheusChunkIterator) FindAtOrAfter(time model.Time) bool {
	// FindAtOrAfter must return OLDEST value at given time. That means we need to start with a fresh iterator,
	// otherwise we cannot guarantee OLDEST.
	p.it = p.c.Iterator(p.it)
	return p.it.Seek(int64(time))
}

func (p *prometheusChunkIterator) Value() model.SamplePair {
	ts, val := p.it.At()
	return model.SamplePair{
		Timestamp: model.Time(ts),
		Value:     model.SampleValue(val),
	}
}

func (p *prometheusChunkIterator) Batch(size int) Batch {
	var batch Batch
	j := 0
	for j < size {
		t, v := p.it.At()
		batch.Timestamps[j] = t
		batch.Values[j] = v
		j++
		if j < size && !p.it.Next() {
			break
		}
	}
	batch.Index = 0
	batch.Length = j
	return batch
}

func (p *prometheusChunkIterator) Err() error {
	return p.it.Err()
}

type errorIterator string

func (e errorIterator) Scan() bool                         { return false }
func (e errorIterator) FindAtOrAfter(time model.Time) bool { return false }
func (e errorIterator) Value() model.SamplePair            { panic("no values") }
func (e errorIterator) Batch(size int) Batch               { panic("no values") }
func (e errorIterator) Err() error                         { return errors.New(string(e)) }
