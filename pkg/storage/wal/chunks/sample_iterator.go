package chunks

import (
	"github.com/grafana/loki/v3/pkg/iter"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/log"
)

// NewSampleIterator creates an iterator for efficiently traversing samples in a chunk.
// It takes compressed chunk data, a processing pipeline, iteration direction, and a time range.
// The returned iterator filters samples based on the time range and applies the given pipeline.
// It handles both forward and backward iteration.
//
// Parameters:
//   - chunkData: Compressed chunk data containing samples
//   - pipeline: StreamSampleExtractor for processing and filtering samples
//   - from: Start timestamp (inclusive) for filtering samples
//   - through: End timestamp (exclusive) for filtering samples
//
// Returns a SampleIterator and an error if creation fails.
func NewSampleIterator(
	chunkData []byte,
	pipeline log.StreamSampleExtractor,
	from, through int64,
) (iter.SampleIterator, error) {
	chkReader, err := NewChunkReader(chunkData)
	if err != nil {
		return nil, err
	}
	it := &sampleBufferedIterator{
		reader:   chkReader,
		pipeline: pipeline,
		from:     from,
		through:  through,
	}
	return it, nil
}

// At implements iter.SampleIterator.
func (s *sampleBufferedIterator) At() logproto.Sample {
	return s.cur
}

// Close implements iter.SampleIterator.
func (s *sampleBufferedIterator) Close() error {
	return s.reader.Close()
}

// Err implements iter.SampleIterator.
func (s *sampleBufferedIterator) Err() error {
	return s.reader.Err()
}

// Labels implements iter.SampleIterator.
func (s *sampleBufferedIterator) Labels() string {
	return s.currLabels.String()
}

// Next implements iter.SampleIterator.
func (s *sampleBufferedIterator) Next() bool {
	for s.reader.Next() {
		// todo: Only use length columns for bytes_over_time without filter.
		ts, line := s.reader.At()
		// check if the timestamp is within the range before applying the pipeline.
		if ts < s.from {
			continue
		}
		if ts >= s.through {
			return false
		}
		// todo: structured metadata.
		val, lbs, matches := s.pipeline.Process(ts, line)
		if !matches {
			continue
		}
		s.currLabels = lbs
		s.cur.Value = val
		s.cur.Timestamp = ts
		return true
	}
	return false
}

// StreamHash implements iter.SampleIterator.
func (s *sampleBufferedIterator) StreamHash() uint64 {
	return s.pipeline.BaseLabels().Hash()
}
