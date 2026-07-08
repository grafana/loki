// Package sortmerge provides a k-way merge iterator over dataobj logs sections.
//
// It is a small primitive shared between the dataobj consumer (which uses it
// to merge sorted sections during a flush) and the dataobj-compactor executor
// (which uses it to merge sorted sections from multiple source data objects).
//
// The iterator emits records with their original (per-source) StreamIDs.
// StreamID rewriting is the caller's responsibility.
package sortmerge

import (
	"context"
	"fmt"
	"math"
	"slices"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/result"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/util/symbolizer"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/util/loser"
)

// Iterator returns an iterator that performs a k-way merge of records from
// multiple logs sections. It requires that the input sections are sorted
// according to sort.
func Iterator(ctx context.Context, sections []*dataobj.Section, sort logs.SortOrder) (result.Seq[logs.Record], error) {
	return iterator(ctx, sections, logs.CompareForSortOrder(sort))
}

// IteratorForSchema returns an iterator that performs a k-way merge of records
// from multiple schema-sorted logs sections. The input sections must be sorted
// by [schema sort key ASC, streamID ASC, timestamp DESC].
//
// It expects sortKeys to contain a mapping from StreamID to schema sort key.
func IteratorForSchema(ctx context.Context, sections []*dataobj.Section, sortKeys []string) (result.Seq[logs.Record], error) {
	return iterator(ctx, sections, logs.CompareForSortSchema(sortKeys))
}

// IteratorWithStreamRemap performs a k-way merge over logs sections drawn from
// multiple source objects. Each section's stream IDs are rewritten into a single global space
// via remaps[i] (the map for sections[i]) before records are compared, so one
// merge can order records across objects.
func IteratorWithStreamRemap(ctx context.Context, sections []*dataobj.Section, remaps []map[int64]int64, globalSortKeys []string, expectedSchema []string) (result.Seq[logs.Record], error) {
	if len(sections) != len(remaps) {
		return nil, fmt.Errorf("sortmerge: got %d sections but %d remaps", len(sections), len(remaps))
	}

	sequences := make([]*remapSectionSequence, 0, len(sections))
	bufferSize := max(128, 8192/max(1, len(sections)))

	for i, s := range sections {
		sec, err := logs.Open(ctx, s)
		if err != nil {
			return nil, fmt.Errorf("failed to open logs section: %w", err)
		}

		schema, err := sec.SchemaLabels()
		if err != nil {
			return nil, fmt.Errorf("reading section schema labels: %w", err)
		}
		if !slices.Equal(schema, expectedSchema) {
			return nil, fmt.Errorf("section schema %v does not match expected sort schema %v", schema, expectedSchema)
		}

		ds, err := logs.MakeColumnarDataset(sec)
		if err != nil {
			return nil, fmt.Errorf("creating columnar dataset: %w", err)
		}

		columns, err := result.Collect(ds.ListColumns(ctx))
		if err != nil {
			return nil, err
		}

		r := dataset.NewRowReader(dataset.RowReaderOptions{
			Dataset:  ds,
			Columns:  columns,
			Prefetch: true,
		})
		if err := r.Open(ctx); err != nil {
			return nil, fmt.Errorf("opening dataset row reader: %w", err)
		}

		sequences = append(sequences, &remapSectionSequence{
			section:         sec,
			remap:           remaps[i],
			DatasetSequence: logs.NewDatasetSequence(r, bufferSize),
		})
	}

	maxValue := result.Value(dataset.Row{
		Index: math.MaxInt,
		Values: []dataset.Value{
			dataset.Int64Value(math.MaxInt64), // StreamID
			dataset.Int64Value(math.MinInt64), // Timestamp
		},
	})

	tree := loser.New(sequences, maxValue, remapSectionSequenceAt, logs.CompareForSortSchema(globalSortKeys), remapSectionSequenceClose)
	sym := symbolizer.New(1024, 100_000)

	return result.Iter(
		func(yield func(logs.Record) bool) error {
			defer tree.Close()
			for tree.Next() {
				seq := tree.Winner()

				row, err := remapSectionSequenceAt(seq).Value()
				if err != nil {
					return err
				}

				var record logs.Record
				if err := logs.DecodeRow(seq.section.Columns(), row, &record, sym); err != nil {
					return err
				}
				// StreamID was rewritten to the global ID in the row; annotate the
				// sort key so downstream builders (SortSchemaASC) sort correctly.
				if record.StreamID >= 0 && int(record.StreamID) < len(globalSortKeys) {
					record.SortKey = globalSortKeys[record.StreamID]
				}
				if !yield(record) {
					return nil
				}
			}
			return nil
		}), nil
}

// remapSectionSequence wraps a section cursor and rewrites its local stream IDs
// into a global space as rows are produced.
type remapSectionSequence struct {
	logs.DatasetSequence
	section *logs.Section
	remap   map[int64]int64
}

var _ loser.Sequence = (*remapSectionSequence)(nil)

func (s *remapSectionSequence) Next() bool {
	if !s.DatasetSequence.Next() {
		return false
	}
	res := s.DatasetSequence.At()
	row, err := res.Value()
	if err != nil {
		return true // error is surfaced via At() to the consumer
	}
	if g, ok := s.remap[row.Values[0].Int64()]; ok {
		row.Values[0] = dataset.Int64Value(g)
	}
	return true
}

func remapSectionSequenceAt(seq *remapSectionSequence) result.Result[dataset.Row] { return seq.At() }
func remapSectionSequenceClose(seq *remapSectionSequence)                         { seq.Close() }

func iterator(
	ctx context.Context,
	sections []*dataobj.Section,
	less func(result.Result[dataset.Row], result.Result[dataset.Row]) bool,
) (result.Seq[logs.Record], error) {
	sequences := make([]*sectionSequence, 0, len(sections))

	// The buffer size is a trade-off between memory overhead and performance: Share a sensible batch size amongst the sections.
	bufferSize := max(128, 8192/max(1, len(sections)))

	for _, s := range sections {
		sec, err := logs.Open(ctx, s)
		if err != nil {
			return nil, fmt.Errorf("failed to open logs section: %w", err)
		}

		ds, err := logs.MakeColumnarDataset(sec)
		if err != nil {
			return nil, fmt.Errorf("creating columnar dataset: %w", err)
		}

		columns, err := result.Collect(ds.ListColumns(ctx))
		if err != nil {
			return nil, err
		}

		r := dataset.NewRowReader(dataset.RowReaderOptions{
			Dataset:  ds,
			Columns:  columns,
			Prefetch: true,
		})
		if err := r.Open(ctx); err != nil {
			return nil, fmt.Errorf("opening dataset row reader: %w", err)
		}

		sequences = append(sequences, &sectionSequence{
			section:         sec,
			DatasetSequence: logs.NewDatasetSequence(r, bufferSize),
		})
	}

	maxValue := result.Value(dataset.Row{
		Index: math.MaxInt,
		Values: []dataset.Value{
			dataset.Int64Value(math.MaxInt64), // StreamID
			dataset.Int64Value(math.MinInt64), // Timestamp
		},
	})

	tree := loser.New(sequences, maxValue, sectionSequenceAt, less, sectionSequenceClose)

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
				err = logs.DecodeRow(seq.section.Columns(), row, &record, nil)
				if err != nil || !yield(record) {
					return err
				}
			}
			return nil
		}), nil
}

type sectionSequence struct {
	logs.DatasetSequence
	section *logs.Section
}

var _ loser.Sequence = (*sectionSequence)(nil)

func sectionSequenceAt(seq *sectionSequence) result.Result[dataset.Row] { return seq.At() }
func sectionSequenceClose(seq *sectionSequence)                         { seq.Close() }
