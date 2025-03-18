package querier

import (
	"container/heap"
	"context"
	"fmt"
	"io"
	"sort"
	"sync"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/iter"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/logql/log"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
)

var (
	recordsPool = sync.Pool{
		New: func() interface{} {
			records := make([]dataobj.Record, 1024)
			return &records
		},
	}
	samplesPool = sync.Pool{
		New: func() interface{} {
			samples := make([]logproto.Sample, 0, 1024)
			return &samples
		},
	}
	entryWithLabelsPool = sync.Pool{
		New: func() interface{} {
			entries := make([]entryWithLabels, 0, 1024)
			return &entries
		},
	}
)

type entryWithLabels struct {
	Labels     string
	StreamHash uint64
	Entry      logproto.Entry
}

// newEntryIterator creates a new EntryIterator for the given context, streams, and reader.
// It reads records from the reader and adds them to the topk heap based on the direction.
// The topk heap is used to maintain the top k entries based on the direction.
// The final result is returned as a slice of entries.
func newEntryIterator(ctx context.Context,
	streams map[int64]dataobj.Stream,
	reader *dataobj.LogsReader,
	req logql.SelectLogParams,
) (iter.EntryIterator, error) {
	bufPtr := recordsPool.Get().(*[]dataobj.Record)
	defer recordsPool.Put(bufPtr)
	buf := *bufPtr

	selector, err := req.LogSelector()
	if err != nil {
		return nil, err
	}
	pipeline, err := selector.Pipeline()
	if err != nil {
		return nil, err
	}

	var (
		prevStreamID    int64 = -1
		streamExtractor log.StreamPipeline
		streamHash      uint64
		top             = newTopK(int(req.Limit), req.Direction)
	)

	for {
		n, err := reader.Read(ctx, buf)
		if err != nil && err != io.EOF {
			return nil, fmt.Errorf("failed to read log records: %w", err)
		}

		if n == 0 && err == io.EOF {
			break
		}

		for _, record := range buf[:n] {
			stream, ok := streams[record.StreamID]
			if !ok {
				continue
			}
			if prevStreamID != record.StreamID {
				streamExtractor = pipeline.ForStream(stream.Labels)
				streamHash = streamExtractor.BaseLabels().Hash()
				prevStreamID = record.StreamID
			}

			timestamp := record.Timestamp.UnixNano()
			line, parsedLabels, ok := streamExtractor.Process(timestamp, record.Line, record.Metadata...)
			if !ok {
				continue
			}
			var metadata []logproto.LabelAdapter
			if len(record.Metadata) > 0 {
				metadata = logproto.FromLabelsToLabelAdapters(record.Metadata)
			}
			top.Add(entryWithLabels{
				Labels:     parsedLabels.String(),
				StreamHash: streamHash,
				Entry: logproto.Entry{
					Timestamp:          record.Timestamp,
					Line:               string(line),
					StructuredMetadata: metadata,
				},
			})
		}
	}
	return top.Iterator(), nil
}

// entryHeap implements a min-heap of entries based on a custom less function.
// The less function determines the ordering based on the direction (FORWARD/BACKWARD).
// For FORWARD direction:
//   - When comparing timestamps: entry.Timestamp.After(b) means 'a' is "less" than 'b'
//   - Example: [t3, t2, t1] where t3 is most recent, t3 will be at the root (index 0)
//
// For BACKWARD direction:
//   - When comparing timestamps: entry.Timestamp.Before(b) means 'a' is "less" than 'b'
//   - Example: [t1, t2, t3] where t1 is oldest, t1 will be at the root (index 0)
//
// In both cases:
//   - When timestamps are equal, we use labels as a tiebreaker
//   - The root of the heap (index 0) contains the entry we want to evict first
type entryHeap struct {
	less    func(a, b entryWithLabels) bool
	entries []entryWithLabels
}

func (h *entryHeap) Push(x any) {
	h.entries = append(h.entries, x.(entryWithLabels))
}

func (h *entryHeap) Pop() any {
	old := h.entries
	n := len(old)
	x := old[n-1]
	h.entries = old[:n-1]
	return x
}

func (h *entryHeap) Len() int {
	return len(h.entries)
}

func (h *entryHeap) Less(i, j int) bool {
	return h.less(h.entries[i], h.entries[j])
}

func (h *entryHeap) Swap(i, j int) {
	h.entries[i], h.entries[j] = h.entries[j], h.entries[i]
}

func lessFn(direction logproto.Direction) func(a, b entryWithLabels) bool {
	switch direction {
	case logproto.FORWARD:
		return func(a, b entryWithLabels) bool {
			if a.Entry.Timestamp.Equal(b.Entry.Timestamp) {
				return a.Labels < b.Labels
			}
			return a.Entry.Timestamp.After(b.Entry.Timestamp)
		}
	case logproto.BACKWARD:
		return func(a, b entryWithLabels) bool {
			if a.Entry.Timestamp.Equal(b.Entry.Timestamp) {
				return a.Labels < b.Labels
			}
			return a.Entry.Timestamp.Before(b.Entry.Timestamp)
		}
	default:
		panic("invalid direction")
	}
}

// topk maintains a min-heap of the k most relevant entries.
// The heap is ordered by timestamp (and labels as tiebreaker) based on the direction:
//   - For FORWARD: keeps k oldest entries by evicting newest entries first
//     Example with k=3: If entries arrive as [t1,t2,t3,t4,t5], heap will contain [t1,t2,t3]
//   - For BACKWARD: keeps k newest entries by evicting oldest entries first
//     Example with k=3: If entries arrive as [t1,t2,t3,t4,t5], heap will contain [t3,t4,t5]
type topk struct {
	k       int
	minHeap entryHeap
}

func newTopK(k int, direction logproto.Direction) *topk {
	if k <= 0 {
		panic("k must be greater than 0")
	}
	entries := entryWithLabelsPool.Get().(*[]entryWithLabels)
	return &topk{
		k: k,
		minHeap: entryHeap{
			less:    lessFn(direction),
			entries: *entries,
		},
	}
}

// Add adds a new entry to the topk heap.
// If the heap has less than k entries, the entry is added directly.
// Otherwise, if the new entry should be evicted before the root (index 0),
// it is discarded. If not, the root is popped (discarded) and the new entry is pushed.
//
// For FORWARD direction:
//   - Root contains newest entry (to be evicted first)
//   - New entries that are newer than root are discarded
//     Example: With k=3 and heap=[t1,t2,t3], a new entry t4 is discarded
//
// For BACKWARD direction:
//   - Root contains oldest entry (to be evicted first)
//   - New entries that are older than root are discarded
//     Example: With k=3 and heap=[t3,t4,t5], a new entry t2 is discarded
func (t *topk) Add(r entryWithLabels) {
	if t.minHeap.Len() < t.k {
		heap.Push(&t.minHeap, r)
		return
	}
	if t.minHeap.less(t.minHeap.entries[0], r) {
		_ = heap.Pop(&t.minHeap)
		heap.Push(&t.minHeap, r)
	}
}

type sliceIterator struct {
	entries []entryWithLabels
	curr    entryWithLabels
}

func (t *topk) Iterator() iter.EntryIterator {
	// We swap i and j in the less comparison to reverse the ordering from the minHeap.
	// The minHeap is ordered such that the entry to evict is at index 0.
	// For FORWARD: newest entries are evicted first, so we want oldest entries first in the final slice
	// For BACKWARD: oldest entries are evicted first, so we want newest entries first in the final slice
	// By swapping i and j, we effectively reverse the minHeap ordering to get the desired final ordering
	sort.Slice(t.minHeap.entries, func(i, j int) bool {
		return t.minHeap.less(t.minHeap.entries[j], t.minHeap.entries[i])
	})
	return &sliceIterator{entries: t.minHeap.entries}
}

func (s *sliceIterator) Next() bool {
	if len(s.entries) == 0 {
		return false
	}
	s.curr = s.entries[0]
	s.entries = s.entries[1:]
	return true
}

func (s *sliceIterator) At() logproto.Entry {
	return s.curr.Entry
}

func (s *sliceIterator) Err() error {
	return nil
}

func (s *sliceIterator) Labels() string {
	return s.curr.Labels
}

func (s *sliceIterator) StreamHash() uint64 {
	return s.curr.StreamHash
}

func (s *sliceIterator) Close() error {
	entryWithLabelsPool.Put(&s.entries)
	return nil
}

func newSampleIterator(ctx context.Context,
	streams map[int64]dataobj.Stream,
	extractors []syntax.SampleExtractor,
	reader *dataobj.LogsReader,
) (iter.SampleIterator, error) {
	bufPtr := recordsPool.Get().(*[]dataobj.Record)
	defer recordsPool.Put(bufPtr)
	buf := *bufPtr

	var (
		iterators       []iter.SampleIterator
		prevStreamID    int64 = -1
		streamExtractor log.StreamSampleExtractor
		series          = map[string]*logproto.Series{}
		streamHash      uint64
	)

	for {
		n, err := reader.Read(ctx, buf)
		if err != nil && err != io.EOF {
			return nil, fmt.Errorf("failed to read log records: %w", err)
		}

		// Handle end of stream or empty read
		if n == 0 && err == io.EOF {
			iterators = appendIteratorFromSeries(iterators, series)
			break
		}

		// Process records in the current batch
		for _, record := range buf[:n] {
			stream, ok := streams[record.StreamID]
			if !ok {
				continue
			}

			for _, extractor := range extractors {
				// Handle stream transition
				if prevStreamID != record.StreamID {
					iterators = appendIteratorFromSeries(iterators, series)
					clear(series)
					streamExtractor = extractor.ForStream(stream.Labels)
					streamHash = streamExtractor.BaseLabels().Hash()
					prevStreamID = record.StreamID
				}

				// Process the record
				timestamp := record.Timestamp.UnixNano()

				// TODO(twhitney): when iterating over multiple extractors, we need a way to pre-process as much of the line as possible
				// In the case of multi-variant expressions, the only difference between the multiple extractors should be the final value, with all
				// other filters and processing already done.
				value, parsedLabels, ok := streamExtractor.Process(timestamp, record.Line, record.Metadata...)
				if !ok {
					continue
				}

				// Get or create series for the parsed labels
				labelString := parsedLabels.String()
				s, exists := series[labelString]
				if !exists {
					s = createNewSeries(labelString, streamHash)
					series[labelString] = s
				}

				// Add sample to the series
				s.Samples = append(s.Samples, logproto.Sample{
					Timestamp: timestamp,
					Value:     value,
					Hash:      0,
				})
			}
		}
	}

	if len(iterators) == 0 {
		return iter.NoopSampleIterator, nil
	}

	return iter.NewSortSampleIterator(iterators), nil
}

// createNewSeries creates a new Series for the given labels and stream hash
func createNewSeries(labels string, streamHash uint64) *logproto.Series {
	samplesPtr := samplesPool.Get().(*[]logproto.Sample)
	samples := *samplesPtr
	return &logproto.Series{
		Labels:     labels,
		Samples:    samples[:0],
		StreamHash: streamHash,
	}
}

// appendIteratorFromSeries appends a new SampleIterator to the given list of iterators
func appendIteratorFromSeries(iterators []iter.SampleIterator, series map[string]*logproto.Series) []iter.SampleIterator {
	if len(series) == 0 {
		return iterators
	}

	seriesResult := make([]logproto.Series, 0, len(series))
	for _, s := range series {
		seriesResult = append(seriesResult, *s)
	}

	return append(iterators, iter.SampleIteratorWithClose(
		iter.NewMultiSeriesIterator(seriesResult),
		func() error {
			for _, s := range seriesResult {
				samplesPool.Put(&s.Samples)
			}
			return nil
		},
	))
}
