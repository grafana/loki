// Package sortmerge provides a k-way merge iterator over dataobj logs sections.
//
// It is a small primitive shared between the dataobj builder (which uses it
// to merge sorted sections during a flush) and the dataobj-compactor executor
// (which uses it to merge sorted sections from multiple source data objects).
package sortmerge

import (
	"context"
	"fmt"
	"math"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/result"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
	"github.com/grafana/loki/v3/pkg/util/loser"
)

// SchemaSortedIterator returns an iterator that performs a k-way merge of records
// from multiple schema-sorted logs sections.
// The input sections must belong to the same object and must therefore have the same StreamID key space.
// The input sections must be sorted by the schema method: [shard ASC, schema key ASC, hash ASC, streamID ASC, timestamp DESC].
//
// An additional ordering input is required to map StreamID to the corresponding external sort tuple ([0] unused).
func SchemaSortedIterator(ctx context.Context, sections []*dataobj.Section, ordering []streams.SortKey) (result.Seq[logs.Record], error) {
	return schemaSortedIterator(ctx, sections, ordering, nil)
}

// SchemaSortedStreamsIterator is like [SchemaSortedIterator], but it merges
// only the records of the streams in streamIDs. Each section reader skips the
// pages that hold no record of those streams, except the stream ID pages. An
// empty streamIDs merges no records.
func SchemaSortedStreamsIterator(ctx context.Context, sections []*dataobj.Section, ordering []streams.SortKey, streamIDs []int64) (result.Seq[logs.Record], error) {
	if len(streamIDs) == 0 {
		return result.Iter(func(func(logs.Record) bool) error { return nil }), nil
	}
	return schemaSortedIterator(ctx, sections, ordering, streamIDs)
}

// schemaSortedIterator merges the records of sections. A nil streamIDs
// merges every record.
func schemaSortedIterator(ctx context.Context, sections []*dataobj.Section, ordering []streams.SortKey, streamIDs []int64) (result.Seq[logs.Record], error) {
	sequences := make([]*sectionSequence, 0, len(sections))
	ready := false
	defer func() {
		if !ready {
			for _, seq := range sequences {
				seq.Close()
			}
		}
	}()
	bufferSize := max(128, 8192/max(1, len(sections)))

	for _, s := range sections {
		sec, err := logs.Open(ctx, s)
		if err != nil {
			return nil, fmt.Errorf("failed to open logs section: %w", err)
		}

		seq, err := openSectionSequence(ctx, sec, bufferSize, streamIDs)
		if err != nil {
			return nil, err
		}
		sequences = append(sequences, seq)
	}

	maxValue := result.Value(dataset.Row{
		Index: math.MaxInt,
		Values: []dataset.Value{
			dataset.Int64Value(math.MaxInt64), // StreamID
			dataset.Int64Value(math.MinInt64), // Timestamp
		},
	})

	tree := loser.New(sequences, maxValue, sectionSequenceAt, logs.CompareByStreamSchema(ordering), sectionSequenceClose)
	ready = true

	return result.Iter(
		func(yield func(logs.Record) bool) error {
			defer tree.Close()
			for tree.Next() {
				seq := tree.Winner()

				row, err := sectionSequenceAt(seq).Value()
				if err != nil {
					return err
				}

				var record logs.Record
				if err := logs.DecodeRow(seq.section.Columns(), row, &record, nil); err != nil {
					return err
				}
				if !yield(record) {
					return nil
				}
			}
			return nil
		}), nil
}

// openSectionSequence opens a cursor over the records of sec. A nil streamIDs
// reads every record.
func openSectionSequence(ctx context.Context, sec *logs.Section, bufferSize int, streamIDs []int64) (*sectionSequence, error) {
	ds, err := logs.MakeColumnarDataset(sec)
	if err != nil {
		return nil, fmt.Errorf("creating columnar dataset: %w", err)
	}
	columns, err := result.Collect(ds.ListColumns(ctx))
	if err != nil {
		return nil, err
	}

	opts := dataset.RowReaderOptions{Dataset: ds, ProjectedColumns: columns, PrefetchAllOnOpen: false}
	if streamIDs != nil {
		p, err := streamIDPredicate(sec, columns, streamIDs)
		if err != nil {
			return nil, err
		}
		opts.Predicates = []dataset.Predicate{p}
	}
	r := dataset.NewRowReader(opts)
	if err := r.Open(ctx); err != nil {
		_ = r.Close()
		return nil, fmt.Errorf("opening dataset row reader: %w", err)
	}
	return &sectionSequence{section: sec, DatasetSequence: logs.NewDatasetSequence(r, bufferSize)}, nil
}

// streamIDPredicate returns a predicate that keeps the records of the streams
// in streamIDs. columns must be the dataset columns of sec, in the order of
// sec.Columns().
func streamIDPredicate(sec *logs.Section, columns []dataset.Column, streamIDs []int64) (dataset.Predicate, error) {
	for i, col := range sec.Columns() {
		if col.Type == logs.ColumnTypeStreamID {
			// A section keeps the records of a stream together, so consecutive
			// rows usually share a stream ID, and the memoized set skips most
			// lookups. The set is not safe for concurrent use; each reader
			// gets its own.
			return dataset.InPredicate{
				Column: columns[i],
				Values: dataset.NewMemoizedInt64ValueSetOf(streamIDs...),
			}, nil
		}
	}
	return nil, fmt.Errorf("logs section has no stream ID column")
}

// sectionSequence wraps a section cursor. When remap is non-nil it rewrites the
// section's local stream IDs into a global space as rows are produced.
type sectionSequence struct {
	logs.DatasetSequence
	section *logs.Section
	remap   map[int64]int64
	err     error
}

var _ loser.Sequence = (*sectionSequence)(nil)

func (s *sectionSequence) Next() bool {
	if !s.DatasetSequence.Next() {
		return false
	}
	if s.remap == nil {
		return true
	}
	row, err := s.DatasetSequence.At().Value()
	if err != nil {
		return true // error is surfaced via At() to the consumer
	}
	if g, ok := s.remap[row.Values[0].Int64()]; ok {
		row.Values[0] = dataset.Int64Value(g)
	} else {
		s.err = fmt.Errorf("sort merge: logs record references stream ID %d absent from stream remap", row.Values[0].Int64())
	}
	return true
}

func (s *sectionSequence) At() result.Result[dataset.Row] {
	if s.err != nil {
		return result.Error[dataset.Row](s.err)
	}
	return s.DatasetSequence.At()
}

func sectionSequenceAt(seq *sectionSequence) result.Result[dataset.Row] { return seq.At() }
func sectionSequenceClose(seq *sectionSequence)                         { seq.Close() }
