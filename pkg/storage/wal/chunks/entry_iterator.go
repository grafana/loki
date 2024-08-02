package chunks

import (
	"time"

	"github.com/grafana/loki/v3/pkg/iter"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/log"

	"github.com/grafana/loki/pkg/push"
)

type entryBufferedIterator struct {
	reader        *ChunkReader
	pipeline      log.StreamPipeline
	from, through int64

	cur        logproto.Entry
	currLabels log.LabelsResult
}

// NewEntryIterator creates an iterator for efficiently traversing log entries in a chunk.
// It takes compressed chunk data, a processing pipeline, iteration direction, and a time range.
// The returned iterator filters entries based on the time range and applies the given pipeline.
// It handles both forward and backward iteration.
//
// Parameters:
//   - chunkData: Compressed chunk data containing log entries
//   - pipeline: StreamPipeline for processing and filtering entries
//   - direction: Direction of iteration (FORWARD or BACKWARD)
//   - from: Start timestamp (inclusive) for filtering entries
//   - through: End timestamp (exclusive) for filtering entries
//
// Returns an EntryIterator and an error if creation fails.
func NewEntryIterator(
	chunkData []byte,
	pipeline log.StreamPipeline,
	direction logproto.Direction,
	from, through int64,
) (iter.EntryIterator, error) {
	chkReader, err := NewChunkReader(chunkData)
	if err != nil {
		return nil, err
	}
	it := &entryBufferedIterator{
		reader:   chkReader,
		pipeline: pipeline,
		from:     from,
		through:  through,
	}
	if direction == logproto.FORWARD {
		return it, nil
	}
	return iter.NewEntryReversedIter(it)
}

// At implements iter.EntryIterator.
func (e *entryBufferedIterator) At() push.Entry {
	return e.cur
}

// Close implements iter.EntryIterator.
func (e *entryBufferedIterator) Close() error {
	return e.reader.Close()
}

// Err implements iter.EntryIterator.
func (e *entryBufferedIterator) Err() error {
	return e.reader.Err()
}

// Labels implements iter.EntryIterator.
func (e *entryBufferedIterator) Labels() string {
	return e.currLabels.String()
}

// Next implements iter.EntryIterator.
func (e *entryBufferedIterator) Next() bool {
	for e.reader.Next() {
		ts, line := e.reader.At()
		// check if the timestamp is within the range before applying the pipeline.
		if ts < e.from {
			continue
		}
		if ts >= e.through {
			return false
		}
		// todo: structured metadata.
		newLine, lbs, matches := e.pipeline.Process(ts, line)
		if !matches {
			continue
		}
		e.currLabels = lbs
		e.cur.Timestamp = time.Unix(0, ts)
		e.cur.Line = string(newLine)
		e.cur.StructuredMetadata = logproto.FromLabelsToLabelAdapters(lbs.StructuredMetadata())
		e.cur.Parsed = logproto.FromLabelsToLabelAdapters(lbs.Parsed())
		return true
	}
	return false
}

// StreamHash implements iter.EntryIterator.
func (e *entryBufferedIterator) StreamHash() uint64 {
	return e.pipeline.BaseLabels().Hash()
}

type sampleBufferedIterator struct {
	reader        *ChunkReader
	pipeline      log.StreamSampleExtractor
	from, through int64

	cur        logproto.Sample
	currLabels log.LabelsResult
}
