package ingester

import (
	"fmt"
	"sort"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/pkg/value"

	"github.com/cortexproject/cortex/pkg/chunk/encoding"
	"github.com/cortexproject/cortex/pkg/prom1/storage/metric"
)

const (
	sampleOutOfOrder     = "sample-out-of-order"
	newValueForTimestamp = "new-value-for-timestamp"
	sampleOutOfBounds    = "sample-out-of-bounds"
	duplicateSample      = "duplicate-sample"
	duplicateTimestamp   = "duplicate-timestamp"
)

type memorySeries struct {
	metric labels.Labels

	// Sorted by start time, overlapping chunk ranges are forbidden.
	chunkDescs []*desc

	// Whether the current head chunk has already been finished.  If true,
	// the current head chunk must not be modified anymore.
	headChunkClosed bool

	// The timestamp & value of the last sample in this series. Needed to
	// ensure timestamp monotonicity during ingestion.
	lastSampleValueSet bool
	lastTime           model.Time
	lastSampleValue    model.SampleValue

	// Prometheus metrics.
	createdChunks prometheus.Counter
}

// newMemorySeries returns a pointer to a newly allocated memorySeries for the
// given metric.
func newMemorySeries(m labels.Labels, createdChunks prometheus.Counter) *memorySeries {
	return &memorySeries{
		metric:        m,
		lastTime:      model.Earliest,
		createdChunks: createdChunks,
	}
}

// add adds a sample pair to the series, possibly creating a new chunk.
// The caller must have locked the fingerprint of the series.
func (s *memorySeries) add(v model.SamplePair) error {
	// If sender has repeated the same timestamp, check more closely and perhaps return error.
	if v.Timestamp == s.lastTime {
		// If we don't know what the last sample value is, silently discard.
		// This will mask some errors but better than complaining when we don't really know.
		if !s.lastSampleValueSet {
			return makeNoReportError(duplicateTimestamp)
		}
		// If both timestamp and sample value are the same as for the last append,
		// ignore as they are a common occurrence when using client-side timestamps
		// (e.g. Pushgateway or federation).
		if v.Value.Equal(s.lastSampleValue) {
			return makeNoReportError(duplicateSample)
		}
		return makeMetricValidationError(newValueForTimestamp, s.metric,
			fmt.Errorf("sample with repeated timestamp but different value; last value: %v, incoming value: %v", s.lastSampleValue, v.Value))
	}
	if v.Timestamp < s.lastTime {
		return makeMetricValidationError(sampleOutOfOrder, s.metric,
			fmt.Errorf("sample timestamp out of order; last timestamp: %v, incoming timestamp: %v", s.lastTime, v.Timestamp))
	}

	if len(s.chunkDescs) == 0 || s.headChunkClosed {
		newHead := newDesc(encoding.New(), v.Timestamp, v.Timestamp)
		s.chunkDescs = append(s.chunkDescs, newHead)
		s.headChunkClosed = false
		s.createdChunks.Inc()
	}

	newChunk, err := s.head().add(v)
	if err != nil {
		return err
	}

	// If we get a single chunk result, then just replace the head chunk with it
	// (no need to update first/last time).  Otherwise, we'll need to update first
	// and last time.
	if newChunk != nil {
		first, last, err := firstAndLastTimes(newChunk)
		if err != nil {
			return err
		}
		s.chunkDescs = append(s.chunkDescs, newDesc(newChunk, first, last))
		s.createdChunks.Inc()
	}

	s.lastTime = v.Timestamp
	s.lastSampleValue = v.Value
	s.lastSampleValueSet = true

	return nil
}

func firstAndLastTimes(c encoding.Chunk) (model.Time, model.Time, error) {
	var (
		first    model.Time
		last     model.Time
		firstSet bool
		iter     = c.NewIterator(nil)
	)
	for iter.Scan() {
		sample := iter.Value()
		if !firstSet {
			first = sample.Timestamp
			firstSet = true
		}
		last = sample.Timestamp
	}
	return first, last, iter.Err()
}

// closeHead marks the head chunk closed. The caller must have locked
// the fingerprint of the memorySeries. This method will panic if this
// series has no chunk descriptors.
func (s *memorySeries) closeHead(reason flushReason) {
	s.chunkDescs[0].flushReason = reason
	s.headChunkClosed = true
}

// firstTime returns the earliest known time for the series. The caller must have
// locked the fingerprint of the memorySeries. This method will panic if this
// series has no chunk descriptors.
func (s *memorySeries) firstTime() model.Time {
	return s.chunkDescs[0].FirstTime
}

// Returns time of oldest chunk in the series, that isn't flushed. If there are
// no chunks, or all chunks are flushed, returns 0.
// The caller must have locked the fingerprint of the memorySeries.
func (s *memorySeries) firstUnflushedChunkTime() model.Time {
	for _, c := range s.chunkDescs {
		if !c.flushed {
			return c.FirstTime
		}
	}

	return 0
}

// head returns a pointer to the head chunk descriptor. The caller must have
// locked the fingerprint of the memorySeries. This method will panic if this
// series has no chunk descriptors.
func (s *memorySeries) head() *desc {
	return s.chunkDescs[len(s.chunkDescs)-1]
}

func (s *memorySeries) samplesForRange(from, through model.Time) ([]model.SamplePair, error) {
	// Find first chunk with start time after "from".
	fromIdx := sort.Search(len(s.chunkDescs), func(i int) bool {
		return s.chunkDescs[i].FirstTime.After(from)
	})
	// Find first chunk with start time after "through".
	throughIdx := sort.Search(len(s.chunkDescs), func(i int) bool {
		return s.chunkDescs[i].FirstTime.After(through)
	})
	if fromIdx == len(s.chunkDescs) {
		// Even the last chunk starts before "from". Find out if the
		// series ends before "from" and we don't need to do anything.
		lt := s.chunkDescs[len(s.chunkDescs)-1].LastTime
		if lt.Before(from) {
			return nil, nil
		}
	}
	if fromIdx > 0 {
		fromIdx--
	}
	if throughIdx == len(s.chunkDescs) {
		throughIdx--
	}
	var values []model.SamplePair
	in := metric.Interval{
		OldestInclusive: from,
		NewestInclusive: through,
	}
	var reuseIter encoding.Iterator
	for idx := fromIdx; idx <= throughIdx; idx++ {
		cd := s.chunkDescs[idx]
		reuseIter = cd.C.NewIterator(reuseIter)
		chValues, err := encoding.RangeValues(reuseIter, in)
		if err != nil {
			return nil, err
		}
		values = append(values, chValues...)
	}
	return values, nil
}

func (s *memorySeries) setChunks(descs []*desc) error {
	if len(s.chunkDescs) != 0 {
		return fmt.Errorf("series already has chunks")
	}

	s.chunkDescs = descs
	if len(descs) > 0 {
		s.lastTime = descs[len(descs)-1].LastTime
	}
	return nil
}

func (s *memorySeries) isStale() bool {
	return s.lastSampleValueSet && value.IsStaleNaN(float64(s.lastSampleValue))
}

type desc struct {
	C           encoding.Chunk // nil if chunk is evicted.
	FirstTime   model.Time     // Timestamp of first sample. Populated at creation. Immutable.
	LastTime    model.Time     // Timestamp of last sample. Populated at creation & on append.
	LastUpdate  model.Time     // This server's local time on last change
	flushReason flushReason    // If chunk is closed, holds the reason why.
	flushed     bool           // set to true when flush succeeds
}

func newDesc(c encoding.Chunk, firstTime model.Time, lastTime model.Time) *desc {
	return &desc{
		C:          c,
		FirstTime:  firstTime,
		LastTime:   lastTime,
		LastUpdate: model.Now(),
	}
}

// Add adds a sample pair to the underlying chunk. For safe concurrent access,
// The chunk must be pinned, and the caller must have locked the fingerprint of
// the series.
func (d *desc) add(s model.SamplePair) (encoding.Chunk, error) {
	cs, err := d.C.Add(s)
	if err != nil {
		return nil, err
	}

	if cs == nil {
		d.LastTime = s.Timestamp // sample was added to this chunk
		d.LastUpdate = model.Now()
	}

	return cs, nil
}

func (d *desc) slice(start, end model.Time) *desc {
	return &desc{
		C:         d.C.Slice(start, end),
		FirstTime: start,
		LastTime:  end,
	}
}
