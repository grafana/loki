package sortmerge

import (
	"container/heap"
	"context"
	"fmt"
	"slices"
	"sort"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/result"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/util/symbolizer"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
)

// windowBufferSize is the per-open-section row buffer. Only the sections
// overlapping the current sort-key frontier are open at once, so this can stay
// generous without the memory blowing up with the total section count.
const windowBufferSize = 512

// onSectionOpen and onSectionClose are nil in production. Tests install them (via
// export_test.go) to observe how many section readers are open concurrently.
var (
	onSectionOpen  func()
	onSectionClose func()
)

// WindowSection is one source logs section to merge. MinKey is the section's
// smallest schema sort key in the ComputeSortKey encoding (label values joined
// by "\x00"); it is used to open the section lazily, only once the merge
// frontier reaches it. Remap rewrites the section's local stream IDs into the
// global stream-ID space before comparison.
type WindowSection struct {
	Section *dataobj.Section
	Remap   map[int64]int64
	MinKey  string
}

// WindowedIterator performs a schema-sorted k-way merge that keeps only the
// sections overlapping the current sort-key frontier open at once, rather than
// opening every section up front. Peak open readers equals the sort-key overlap
// depth, not the total section count, which bounds memory for merges spanning
// many sections. Prefetch stays on: with few readers open, batched reads are
// cheap.
//
// sortKeys maps a global stream ID to its precomputed schema sort key.
// expectedSchema, when non-nil, is validated against each section's schema
// labels as the section is opened.
//
// Correctness relies on each section's MinKey being a true lower bound on the
// rows it yields — i.e. every tuple in the section has a ref among the merged
// sections. That is the same section-ownership assumption the caller already
// makes by reading whole sections.
func WindowedIterator(ctx context.Context, sections []WindowSection, sortKeys []string, expectedSchema []string) (result.Seq[logs.Record], error) {
	pending := slices.Clone(sections)
	sort.SliceStable(pending, func(i, j int) bool { return pending[i].MinKey < pending[j].MinKey })

	less := logs.CompareForSortSchema(sortKeys)

	return result.Iter(func(yield func(logs.Record) bool) error {
		h := &activeHeap{less: less}
		defer h.closeAll()

		sym := symbolizer.New(1024, 100_000)

		// next is the index of the next pending section to open.
		var next int

		// openReady opens every pending section that could contribute the next
		// output row: while the smallest not-yet-open MinKey is <= the current
		// frontier (the smallest open row's sort key). When nothing is open, it
		// opens the smallest pending section to make progress.
		openReady := func() error {
			for next < len(pending) {
				if h.Len() > 0 {
					frontier, ok := h.minSortKey(sortKeys)
					if !ok || pending[next].MinKey > frontier {
						break
					}
				}
				ar, hasRow, err := openActiveReader(ctx, pending[next], expectedSchema)
				next++
				if err != nil {
					return err
				}
				if hasRow {
					heap.Push(h, ar)
				}
			}
			return nil
		}

		for {
			if err := openReady(); err != nil {
				return err
			}
			if h.Len() == 0 {
				if next >= len(pending) {
					return nil // fully drained
				}
				continue // nothing open yet; openReady will open the next section
			}

			top := h.readers[0]
			row, err := top.cur.Value()
			if err != nil {
				return err
			}

			var record logs.Record
			if err := logs.DecodeRow(top.sec.Columns(), row, &record, sym); err != nil {
				return err
			}
			if record.StreamID >= 0 && int(record.StreamID) < len(sortKeys) {
				record.SortKey = sortKeys[record.StreamID]
			}
			if !yield(record) {
				return nil
			}

			hasRow, err := top.advance()
			if err != nil {
				return err
			}
			if hasRow {
				heap.Fix(h, 0)
			} else {
				heap.Pop(h)
				top.close()
			}
		}
	}), nil
}

// activeReader is one currently-open source section within the merge window.
type activeReader struct {
	sec   *logs.Section
	seq   *logs.DatasetSequence
	remap map[int64]int64
	cur   result.Result[dataset.Row]
}

// advance reads the next row and rewrites its local stream ID into the global
// space. It returns false once the section is exhausted.
func (a *activeReader) advance() (bool, error) {
	if !a.seq.Next() {
		return false, nil
	}
	row, err := a.seq.At().Value()
	if err != nil {
		return false, err
	}
	if a.remap != nil {
		g, ok := a.remap[row.Values[0].Int64()]
		if !ok {
			return false, fmt.Errorf("sort merge: logs record references stream ID %d absent from stream remap", row.Values[0].Int64())
		}
		row.Values[0] = dataset.Int64Value(g)
	}
	a.cur = result.Value(row)
	return true, nil
}

func (a *activeReader) close() {
	if onSectionClose != nil {
		onSectionClose()
	}
	a.seq.Close()
}

// openActiveReader opens a section's row reader and loads its first row. It
// returns hasRow=false (and closes the reader) for an empty section.
func openActiveReader(ctx context.Context, ws WindowSection, expectedSchema []string) (*activeReader, bool, error) {
	sec, err := logs.Open(ctx, ws.Section)
	if err != nil {
		return nil, false, fmt.Errorf("failed to open logs section: %w", err)
	}
	if expectedSchema != nil {
		schema, err := sec.SchemaLabels()
		if err != nil {
			return nil, false, fmt.Errorf("reading section schema labels: %w", err)
		}
		if !slices.Equal(schema, expectedSchema) {
			return nil, false, fmt.Errorf("section schema %v does not match expected sort schema %v", schema, expectedSchema)
		}
	}
	ds, err := logs.MakeColumnarDataset(sec)
	if err != nil {
		return nil, false, fmt.Errorf("creating columnar dataset: %w", err)
	}
	columns, err := result.Collect(ds.ListColumns(ctx))
	if err != nil {
		return nil, false, err
	}
	r := dataset.NewRowReader(dataset.RowReaderOptions{Dataset: ds, Columns: columns, Prefetch: true})
	if err := r.Open(ctx); err != nil {
		return nil, false, fmt.Errorf("opening dataset row reader: %w", err)
	}
	if onSectionOpen != nil {
		onSectionOpen()
	}

	seq := logs.NewDatasetSequence(r, windowBufferSize)
	ar := &activeReader{sec: sec, seq: &seq, remap: ws.Remap}
	hasRow, err := ar.advance()
	if err != nil {
		ar.close()
		return nil, false, err
	}
	if !hasRow {
		ar.close()
		return nil, false, nil
	}
	return ar, true, nil
}

// activeHeap is a min-heap of open section readers ordered by the merge
// comparator on each reader's current row.
type activeHeap struct {
	readers []*activeReader
	less    func(result.Result[dataset.Row], result.Result[dataset.Row]) bool
}

func (h *activeHeap) Len() int           { return len(h.readers) }
func (h *activeHeap) Less(i, j int) bool { return h.less(h.readers[i].cur, h.readers[j].cur) }
func (h *activeHeap) Swap(i, j int)      { h.readers[i], h.readers[j] = h.readers[j], h.readers[i] }

func (h *activeHeap) Push(x any) { h.readers = append(h.readers, x.(*activeReader)) }

func (h *activeHeap) Pop() any {
	old := h.readers
	n := len(old)
	it := old[n-1]
	old[n-1] = nil
	h.readers = old[:n-1]
	return it
}

// minSortKey returns the sort key of the smallest open row (the heap root).
func (h *activeHeap) minSortKey(sortKeys []string) (string, bool) {
	if len(h.readers) == 0 {
		return "", false
	}
	row, err := h.readers[0].cur.Value()
	if err != nil {
		return "", false
	}
	sid := row.Values[0].Int64()
	if sid < 0 || int(sid) >= len(sortKeys) {
		return "", false
	}
	return sortKeys[sid], true
}

func (h *activeHeap) closeAll() {
	for _, ar := range h.readers {
		if ar != nil {
			ar.close()
		}
	}
}
