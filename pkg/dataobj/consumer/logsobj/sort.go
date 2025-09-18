package logsobj

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/result"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/util/loser"
)

func sortMergeIterator(ctx context.Context, sections []*dataobj.Section, lessFunc compare[dataset.Row]) (result.Seq[logs.Record], error) {
	sequences := make([]*tableSequence, 0, len(sections))
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

		r := dataset.NewReader(dataset.ReaderOptions{
			Dataset:  ds,
			Columns:  columns,
			Prefetch: true,
		})

		sequences = append(sequences, &tableSequence{
			section: sec,
			columns: columns,
			r:       r,
			buf:     make([]dataset.Row, 8192),
		})
	}

	maxValue := result.Value(dataset.Row{
		Index: math.MaxInt,
		Values: []dataset.Value{
			dataset.Int64Value(math.MaxInt64), // StreamID
			dataset.Int64Value(math.MinInt64), // Timestamp
		},
	})

	tree := loser.New(sequences, maxValue, tableSequenceValue, rowResultLess(lessFunc), tableSequenceStop)
	defer tree.Close()

	return result.Iter(
		func(yield func(logs.Record) bool) error {
			for tree.Next() {
				seq := tree.Winner()

				row, err := tableSequenceValue(seq).Value()
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

type tableSequence struct {
	curValue result.Result[dataset.Row]

	section *logs.Section
	columns []dataset.Column

	r *dataset.Reader

	buf  []dataset.Row
	off  int // Offset into buf
	size int // Number of valid values in buf
}

var _ loser.Sequence = (*tableSequence)(nil)

func (seq *tableSequence) Next() bool {
	if seq.off < seq.size {
		seq.curValue = result.Value(seq.buf[seq.off])
		seq.off++
		return true
	}

ReadBatch:
	n, err := seq.r.Read(context.Background(), seq.buf)
	if err != nil && !errors.Is(err, io.EOF) {
		seq.curValue = result.Error[dataset.Row](err)
		return true
	} else if n == 0 && errors.Is(err, io.EOF) {
		return false
	} else if n == 0 {
		// Re-read if we got an empty batch without hitting EOF.
		goto ReadBatch
	}

	seq.curValue = result.Value(seq.buf[0])

	seq.off = 1
	seq.size = n
	return true
}

func tableSequenceValue(seq *tableSequence) result.Result[dataset.Row] { return seq.curValue }

func tableSequenceStop(seq *tableSequence) { _ = seq.r.Close() }

type compare[T any] func(T, T) int

func rowResultLess(cmp compare[dataset.Row]) func(a, b result.Result[dataset.Row]) bool {
	return func(a, b result.Result[dataset.Row]) bool {
		var (
			aRow, aErr = a.Value()
			bRow, bErr = b.Value()
		)

		// Put errors first so we return errors early.
		if aErr != nil {
			return true
		} else if bErr != nil {
			return false
		}

		return cmp(aRow, bRow) < 0
	}
}
