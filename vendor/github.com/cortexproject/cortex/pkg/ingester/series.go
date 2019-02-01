package ingester

import (
	"fmt"
	"sort"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"

	"github.com/cortexproject/cortex/pkg/chunk/encoding"
	"github.com/cortexproject/cortex/pkg/prom1/storage/metric"
)

var (
	createdChunks = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "cortex_ingester_chunks_created_total",
		Help: "The total number of chunks the ingester has created.",
	})
)

func init() {
	prometheus.MustRegister(createdChunks)
}

type memorySeries struct {
	metric sortedLabelPairs

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
}

type memorySeriesError struct {
	message   string
	errorType string
}

func (error *memorySeriesError) Error() string {
	return error.message
}

// newMemorySeries returns a pointer to a newly allocated memorySeries for the
// given metric.
func newMemorySeries(m labelPairs) *memorySeries {
	return &memorySeries{
		metric:   m.copyValuesAndSort(),
		lastTime: model.Earliest,
	}
}

// helper to extract the not-necessarily-sorted type used elsewhere, without casting everywhere.
func (s *memorySeries) labels() labelPairs {
	return labelPairs(s.metric)
}

// add adds a sample pair to the series. It returns the number of newly
// completed chunks (which are now eligible for persistence).
//
// The caller must have locked the fingerprint of the series.
func (s *memorySeries) add(v model.SamplePair) error {
	// Don't report "no-op appends", i.e. where timestamp and sample
	// value are the same as for the last append, as they are a
	// common occurrence when using client-side timestamps
	// (e.g. Pushgateway or federation).
	if s.lastSampleValueSet &&
		v.Timestamp == s.lastTime &&
		v.Value.Equal(s.lastSampleValue) {
		return nil
	}
	if v.Timestamp == s.lastTime {
		return &memorySeriesError{
			message:   fmt.Sprintf("sample with repeated timestamp but different value for series %v; last value: %v, incoming value: %v", s.metric, s.lastSampleValue, v.Value),
			errorType: "new-value-for-timestamp",
		}
	}
	if v.Timestamp < s.lastTime {
		return &memorySeriesError{
			message:   fmt.Sprintf("sample timestamp out of order for series %v; last timestamp: %v, incoming timestamp: %v", s.metric, s.lastTime, v.Timestamp),
			errorType: "sample-out-of-order",
		}
	}

	if len(s.chunkDescs) == 0 || s.headChunkClosed {
		newHead := newDesc(encoding.New(), v.Timestamp, v.Timestamp)
		s.chunkDescs = append(s.chunkDescs, newHead)
		s.headChunkClosed = false
		createdChunks.Inc()
	}

	chunks, err := s.head().add(v)
	if err != nil {
		return err
	}

	// If we get a single chunk result, then just replace the head chunk with it
	// (no need to update first/last time).  Otherwise, we'll need to update first
	// and last time.
	if len(chunks) == 1 {
		s.head().C = chunks[0]
	} else {
		s.chunkDescs = s.chunkDescs[:len(s.chunkDescs)-1]
		for _, c := range chunks {
			first, last, err := firstAndLastTimes(c)
			if err != nil {
				return err
			}
			s.chunkDescs = append(s.chunkDescs, newDesc(c, first, last))
			createdChunks.Inc()
		}
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
		iter     = c.NewIterator()
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

func (s *memorySeries) closeHead() {
	s.headChunkClosed = true
}

// firstTime returns the earliest known time for the series. The caller must have
// locked the fingerprint of the memorySeries. This method will panic if this
// series has no chunk descriptors.
func (s *memorySeries) firstTime() model.Time {
	return s.chunkDescs[0].FirstTime
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
	for idx := fromIdx; idx <= throughIdx; idx++ {
		cd := s.chunkDescs[idx]
		chValues, err := encoding.RangeValues(cd.C.NewIterator(), in)
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

type desc struct {
	C          encoding.Chunk // nil if chunk is evicted.
	FirstTime  model.Time     // Timestamp of first sample. Populated at creation. Immutable.
	LastTime   model.Time     // Timestamp of last sample. Populated at creation & on append.
	LastUpdate model.Time     // This server's local time on last change
	flushed    bool           // set to true when flush succeeds
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
func (d *desc) add(s model.SamplePair) ([]encoding.Chunk, error) {
	cs, err := d.C.Add(s)
	if err != nil {
		return nil, err
	}

	if len(cs) == 1 {
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
