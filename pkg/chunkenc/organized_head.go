package chunkenc

import (
	"bytes"
	"context"
	"encoding/binary"
	"math"

	"github.com/Workiva/go-datastructures/rangetree"
	"github.com/grafana/loki/v3/pkg/compression"
	"github.com/grafana/loki/v3/pkg/iter"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/log"
	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
	"github.com/pkg/errors"
)

type organisedHeadBlock struct {
	unorderedHeadBlock
}

func newOrganisedHeadBlock(fmt HeadBlockFmt, symbolizer *symbolizer) *organisedHeadBlock {
	return &organisedHeadBlock{
		unorderedHeadBlock: unorderedHeadBlock{
			format:     fmt,
			symbolizer: symbolizer,
			rt:         rangetree.New(1),
		},
	}
}

// Serialise is used in creating an ordered, compressed block from an organisedHeadBlock
func (b *organisedHeadBlock) Serialise(pool compression.WriterPool) ([]byte, error) {
	inBuf := serializeBytesBufferPool.Get().(*bytes.Buffer)
	defer func() {
		inBuf.Reset()
		serializeBytesBufferPool.Put(inBuf)
	}()

	outBuf := &bytes.Buffer{}
	compressedWriter := pool.GetWriter(outBuf)
	defer pool.PutWriter(compressedWriter)
	encBuf := make([]byte, binary.MaxVarintLen64)

	_ = b.forEntries(context.Background(), logproto.FORWARD, 0, math.MaxInt64,
		func(_ *stats.Context, _ int64, line string, _ symbols) error {
			n := binary.PutUvarint(encBuf, uint64(len(line)))
			inBuf.Write(encBuf[:n])
			inBuf.WriteString(line)
			return nil
		},
	)

	if _, err := compressedWriter.Write(inBuf.Bytes()); err != nil {
		return nil, errors.Wrap(err, "appending entry")
	}
	if err := compressedWriter.Close(); err != nil {
		return nil, errors.Wrap(err, "flushing pending compress buffer")
	}

	return outBuf.Bytes(), nil
}

func (b *organisedHeadBlock) Iterator(
	_ context.Context,
	_ logproto.Direction,
	_, _ int64,
	_ log.StreamPipeline,
) iter.EntryIterator {
	return nil
}

func (b *organisedHeadBlock) SampleIterator(
	_ context.Context,
	_, _ int64,
	_ log.StreamSampleExtractor,
) iter.SampleIterator {
	return nil
}

func (b *organisedHeadBlock) CompressedBlock(pool compression.WriterPool) (block, int, error) {
	var sm []byte
	var ts []byte

	bl, err := b.Serialise(pool)
	if err != nil {
		return block{}, 0, err
	}
	sm, err = b.serialiseStructuredMetadata()
	if err != nil {
		return block{}, 0, err
	}
	ts, err = b.serialiseTimestamps()
	if err != nil {
		return block{}, 0, err
	}

	mint, maxt := b.Bounds()

	return block{
		b:          bl,
		numEntries: b.Entries(),
		mint:       mint,
		maxt:       maxt,
		sm:         sm,
		ts:         ts,
	}, len(bl), nil
}

func (b *organisedHeadBlock) serialiseStructuredMetadata(pool compression.WriterPool) ([]byte, error) {
	inBuf := serializeBytesBufferPool.Get().(*bytes.Buffer)
	defer func() {
		inBuf.Reset()
		serializeBytesBufferPool.Put(inBuf)
	}()

	symbolsSectionBuf := serializeBytesBufferPool.Get().(*bytes.Buffer)
	defer func() {
		symbolsSectionBuf.Reset()
		serializeBytesBufferPool.Put(symbolsSectionBuf)
	}()

	outBuf := &bytes.Buffer{}
	compressedWriter := pool.GetWriter(outBuf)
	defer pool.PutWriter(compressedWriter)
	encBuf := make([]byte, binary.MaxVarintLen64)

	_ = b.forEntries(context.Background(), logproto.FORWARD, 0, math.MaxInt64,
		func(_ *stats.Context, ts int64, _ string, symbols symbols) error {
			symbolsSectionBuf.Reset()
			n := binary.PutUvarint(encBuf, uint64(len(symbols)))
			symbolsSectionBuf.Write(encBuf[:n])

			for _, l := range symbols {
				n = binary.PutUvarint(encBuf, uint64(l.Name))
				symbolsSectionBuf.Write(encBuf[:n])

				n = binary.PutUvarint(encBuf, uint64(l.Value))
				symbolsSectionBuf.Write(encBuf[:n])
			}

			// write the length of symbols section
			n = binary.PutUvarint(encBuf, uint64(symbolsSectionBuf.Len()))
			inBuf.Write(encBuf[:n])

			inBuf.Write(symbolsSectionBuf.Bytes())

			return nil
		},
	)

	if _, err := compressedWriter.Write(inBuf.Bytes()); err != nil {
		return nil, errors.Wrap(err, "appending entry")
	}
	if err := compressedWriter.Close(); err != nil {
		return nil, errors.Wrap(err, "flushing pending compress buffer")
	}

	return outBuf.Bytes(), nil
}

func (b *organisedHeadBlock) serialiseTimestamps(pool compression.WriterPool) ([]byte, error) {
	inBuf := serializeBytesBufferPool.Get().(*bytes.Buffer)
	defer func() {
		inBuf.Reset()
		serializeBytesBufferPool.Put(inBuf)
	}()

	outBuf := &bytes.Buffer{}
	compressedWriter := pool.GetWriter(outBuf)
	defer pool.PutWriter(compressedWriter)
	encBuf := make([]byte, binary.MaxVarintLen64)

	_ = b.forEntries(context.Background(), logproto.FORWARD, 0, math.MaxInt64,
		func(_ *stats.Context, ts int64, line string, _ symbols) error {
			n := binary.PutVarint(encBuf, ts)
			inBuf.Write(encBuf[:n])
			return nil
		},
	)

	if _, err := compressedWriter.Write(inBuf.Bytes()); err != nil {
		return nil, errors.Wrap(err, "appending entry")
	}
	if err := compressedWriter.Close(); err != nil {
		return nil, errors.Wrap(err, "flushing pending compress buffer")
	}

	return outBuf.Bytes(), nil
}
