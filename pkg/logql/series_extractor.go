package logql

import (
	"github.com/grafana/loki/pkg/iter"
	"github.com/grafana/loki/pkg/logproto"
)

var (
	extractBytes = bytesSampleExtractor{}
	extractCount = countSampleExtractor{}
)

// SeriesIterator is an iterator that iterate over a stream of logs and returns sample.
type SeriesIterator interface {
	Close() error
	Next() bool
	Peek() (Sample, bool)
	Error() error
}

// Sample is a series sample
type Sample struct {
	Labels        string
	Value         float64
	TimestampNano int64
}

type seriesIterator struct {
	iter    iter.PeekingEntryIterator
	sampler SampleExtractor

	updated bool
	cur     Sample
}

func newSeriesIterator(it iter.EntryIterator, sampler SampleExtractor) SeriesIterator {
	return &seriesIterator{
		iter:    iter.NewPeekingIterator(it),
		sampler: sampler,
	}
}

func (e *seriesIterator) Close() error {
	return e.iter.Close()
}

func (e *seriesIterator) Next() bool {
	e.updated = false
	return e.iter.Next()
}

func (e *seriesIterator) Peek() (Sample, bool) {
	if e.updated {
		return e.cur, true
	}

	for {
		lbs, entry, ok := e.iter.Peek()
		if !ok {
			return Sample{}, false
		}

		// transform
		e.cur, ok = e.sampler.From(lbs, entry)
		if ok {
			break
		}
		if !e.iter.Next() {
			return Sample{}, false
		}
	}
	e.updated = true
	return e.cur, true
}

func (e *seriesIterator) Error() error {
	return e.iter.Error()
}

// SampleExtractor transforms a log entry into a sample.
// In case of failure the second return value will be false.
type SampleExtractor interface {
	From(labels string, e logproto.Entry) (Sample, bool)
}

type countSampleExtractor struct{}

func (countSampleExtractor) From(lbs string, entry logproto.Entry) (Sample, bool) {
	return Sample{
		Labels:        lbs,
		TimestampNano: entry.Timestamp.UnixNano(),
		Value:         1.,
	}, true
}

type bytesSampleExtractor struct{}

func (bytesSampleExtractor) From(lbs string, entry logproto.Entry) (Sample, bool) {
	return Sample{
		Labels:        lbs,
		TimestampNano: entry.Timestamp.UnixNano(),
		Value:         float64(len(entry.Line)),
	}, true
}
