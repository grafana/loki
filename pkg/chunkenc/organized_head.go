package chunkenc

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"math"

	"github.com/Workiva/go-datastructures/rangetree"
	"github.com/grafana/loki/v3/pkg/compression"
	"github.com/grafana/loki/v3/pkg/iter"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/log"
	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
	"github.com/pkg/errors"
	"github.com/prometheus/prometheus/model/labels"
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
	sm, err = b.serialiseStructuredMetadata(pool)
	if err != nil {
		return block{}, 0, err
	}
	ts, err = b.serialiseTimestamps(pool)
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

// todo (shantanu): rename these iterators to something meaningful

// newOrganizedSampleIterator iterates over new block format v5.
func newOrganizedSampleIterator(ctx context.Context, pool compression.ReaderPool, b []byte, format byte, extractor log.StreamSampleExtractor, symbolizer *symbolizer) iter.SampleIterator {
	return &sampleOrganizedBufferedIterator{
		organizedBufferedIterator: newOrganizedBufferedIterator(ctx, pool, b, format, symbolizer),
		extractor:                 extractor,
		stats:                     stats.FromContext(ctx),
	}
}

func newOrganizedBufferedIterator(ctx context.Context, pool compression.ReaderPool, b []byte, format byte, symbolizer *symbolizer) *organizedBufferedIterator {
	st := stats.FromContext(ctx)
	st.AddCompressedBytes(int64(len(b)))
	return &organizedBufferedIterator{
		origBytes: b,
		stats:     st,

		pool:       pool,
		symbolizer: symbolizer,

		format: format,
	}
}

type organizedBufferedIterator struct {
	origBytes []byte
	stats     *stats.Context

	reader     io.Reader
	pool       compression.ReaderPool
	symbolizer *symbolizer

	err error

	readBuf      [20]byte // Enough bytes to store two varints.
	readBufValid int      // How many bytes are left in readBuf from previous read.

	format   byte
	buf      []byte // The buffer for a single entry.
	currLine []byte // the current line, this is the same as the buffer but sliced the line size.
	currTs   int64

	symbolsBuf             []symbol      // The buffer for a single entry's symbols.
	currStructuredMetadata labels.Labels // The current labels.

	closed bool
}

func (e *organizedBufferedIterator) Next() bool {
	//TODO implement me
	panic("implement me")
}

func (e *organizedBufferedIterator) Err() error {
	//TODO implement me
	panic("implement me")
}

func (e *organizedBufferedIterator) Close() error {
	//TODO implement me
	panic("implement me")
}

type entryOrganizedBufferedIterator struct {
	*organizedBufferedIterator
	pipeline log.StreamPipeline
	stats    *stats.Context

	cur        logproto.Entry
	currLabels log.LabelsResult
}

func (e *entryOrganizedBufferedIterator) Labels() string {
	//TODO implement me
	panic("implement me")
}

func (e *entryOrganizedBufferedIterator) Err() error {
	//TODO implement me
	panic("implement me")
}

func (e *entryOrganizedBufferedIterator) At() logproto.Entry {
	//TODO implement me
	panic("implement me")
}

func (e *entryOrganizedBufferedIterator) StreamHash() uint64 {
	//TODO implement me
	panic("implement me")
}

func (e *entryOrganizedBufferedIterator) Next() bool {
	//TODO implement me
	panic("implement me")
}

func (e *entryOrganizedBufferedIterator) Close() error {
	//TODO implement me
	panic("implement me")
}

func newEntryOrganizedBufferedIterator(ctx context.Context, pool compression.ReaderPool, b []byte, pipeline log.StreamPipeline, format byte, symbolizer *symbolizer) iter.EntryIterator {
	return &entryOrganizedBufferedIterator{
		organizedBufferedIterator: newOrganizedBufferedIterator(ctx, pool, b, format, symbolizer),
		pipeline:                  pipeline,
		stats:                     stats.FromContext(ctx),
	}
}

type sampleOrganizedBufferedIterator struct {
	*organizedBufferedIterator

	extractor log.StreamSampleExtractor
	stats     *stats.Context

	cur        logproto.Sample
	currLabels log.LabelsResult
}

func (s *sampleOrganizedBufferedIterator) Err() error {
	//TODO implement me
	panic("implement me")
}

func (s *sampleOrganizedBufferedIterator) At() logproto.Sample {
	//TODO implement me
	panic("implement me")
}

func (s *sampleOrganizedBufferedIterator) StreamHash() uint64 {
	//TODO implement me
	panic("implement me")
}

func (s *sampleOrganizedBufferedIterator) Next() bool {
	for s.organizedBufferedIterator.Next() {
		// iterate over samples here
	}
	return false
}
func (s *sampleOrganizedBufferedIterator) Close() error {
	if s.extractor.ReferencedStructuredMetadata() {
		s.stats.SetQueryReferencedStructuredMetadata()
	}

	return s.organizedBufferedIterator.Close()
}

func (s *sampleOrganizedBufferedIterator) Labels() string {
	return s.currLabels.String()
}
