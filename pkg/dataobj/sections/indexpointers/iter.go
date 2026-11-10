package indexpointers

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"time"
	"unsafe"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/result"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/util/symbolizer"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/internal/columnar"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
)

// Iter iterates over indexpointers in the provided decoder. All indexpointers sections are
// iterated over in order.
func Iter(ctx context.Context, obj *dataobj.Object) result.Seq[TenantIndexPointer] {
	return result.Iter(func(yield func(pointer TenantIndexPointer) bool) error {
		for i, section := range obj.Sections().Filter(CheckSection) {
			pointersSection, err := Open(ctx, section)
			if err != nil {
				return fmt.Errorf("opening section %d: %w", i, err)
			}

			for result := range IterSection(ctx, pointersSection) {
				if result.Err() != nil || !yield(TenantIndexPointer{Tenant: section.Tenant, IndexPointer: result.MustValue()}) {
					return result.Err()
				}
			}
		}

		return nil
	})
}

func IterSection(ctx context.Context, section *Section) result.Seq[IndexPointer] {
	return result.Iter(func(yield func(IndexPointer) bool) error {
		dset, err := section.makeDataset()
		if err != nil {
			return fmt.Errorf("creating columns dataset: %w", err)
		}

		columns, err := result.Collect(dset.ListColumns(ctx))
		if err != nil {
			return err
		}

		r := dataset.NewRowReader(dataset.RowReaderOptions{
			Dataset:           dset,
			ProjectedColumns:  columns,
			PrefetchAllOnOpen: true,
		})
		defer r.Close()

		if err := r.Open(ctx); err != nil {
			return err
		}

		sym := symbolizer.New(128, 1024)

		var rows [1024]dataset.Row
		for {
			n, err := r.Read(ctx, rows[:])
			if err != nil && !errors.Is(err, io.EOF) {
				return err
			} else if n == 0 && errors.Is(err, io.EOF) {
				return nil
			}

			var pointer IndexPointer
			for _, row := range rows[:n] {
				if err := decodeRow(section.Columns(), row, &pointer, sym); err != nil {
					return err
				}

				if !yield(pointer) {
					return nil
				}
			}
		}
	})
}

// makeDataset builds a dataset from only the recognized columns, so rows stay
// aligned with Columns() and columns from a newer Loki are skipped, not decoded.
func (s *Section) makeDataset() (*columnar.Dataset, error) {
	recognized := s.Columns()
	inner := make([]*columnar.Column, len(recognized))
	for i, col := range recognized {
		inner[i] = col.inner
	}
	return columnar.MakeDataset(s.inner, inner)
}

// decodeRow decodes an indexpointer from a [dataset.Row], using the provided columns to
// determine the column type. The list of columns must match the columns used
// to create the row.
//
// The sym argument is used for reusing label values between calls to
// decodeRow. If sym is nil, label value strings are always allocated.
func decodeRow(columns []*Column, row dataset.Row, pointer *IndexPointer, sym *symbolizer.Symbolizer) error {
	*pointer = IndexPointer{}
	var minPostingKey, maxPostingKey []byte
	for columnIndex, columnValue := range row.Values {
		column := columns[columnIndex]
		switch column.Type {
		case ColumnTypePath:
			if ty := columnValue.Type(); ty != datasetmd.PHYSICAL_TYPE_BINARY {
				return fmt.Errorf("invalid type %s for %s", ty, column.Type)
			}

			if columnValue.IsNil() || columnValue.IsZero() {
				return fmt.Errorf("nil or zero value for %s", column.Type)
			}

			if sym != nil {
				pointer.Path = sym.Get(unsafeString(columnValue.Binary()))
			} else {
				pointer.Path = string(columnValue.Binary())
			}

		case ColumnTypeMinTimestamp:
			if ty := columnValue.Type(); ty != datasetmd.PHYSICAL_TYPE_INT64 {
				return fmt.Errorf("invalid type %s for %s", ty, column.Type)
			}

			if columnValue.IsNil() || columnValue.IsZero() {
				return fmt.Errorf("nil or zero value for %s", column.Type)
			}

			pointer.StartTs = time.Unix(0, columnValue.Int64())

		case ColumnTypeMaxTimestamp:
			if ty := columnValue.Type(); ty != datasetmd.PHYSICAL_TYPE_INT64 {
				return fmt.Errorf("invalid type %s for %s", ty, column.Type)
			}

			if columnValue.IsNil() || columnValue.IsZero() {
				return fmt.Errorf("nil or zero value for %s", column.Type)
			}

			pointer.EndTs = time.Unix(0, columnValue.Int64())

		case ColumnTypeRunID:
			if columnValue.IsNil() {
				continue
			}
			if ty := columnValue.Type(); ty != datasetmd.PHYSICAL_TYPE_INT64 {
				return fmt.Errorf("invalid type %s for %s", ty, column.Type)
			}
			runID, err := decodeRunID(columnValue.Int64())
			if err != nil {
				return err
			}
			pointer.RunID = runID

		case ColumnTypeMinPostingKey, ColumnTypeMaxPostingKey:
			if columnValue.IsNil() {
				continue
			}
			if ty := columnValue.Type(); ty != datasetmd.PHYSICAL_TYPE_BINARY {
				return fmt.Errorf("invalid type %s for %s", ty, column.Type)
			}
			if column.Type == ColumnTypeMinPostingKey {
				minPostingKey = columnValue.Binary()
			} else {
				maxPostingKey = columnValue.Binary()
			}

		case ColumnTypeShardBuckets:
			if columnValue.IsNil() {
				continue
			}
			if ty := columnValue.Type(); ty != datasetmd.PHYSICAL_TYPE_INT64 {
				return fmt.Errorf("invalid type %s for %s", ty, column.Type)
			}
			buckets, err := decodeShardBuckets(columnValue.Int64())
			if err != nil {
				return err
			}
			pointer.ShardBuckets = buckets

		default:
			// TODO(rfratto): We probably don't want to return an error on unexpected
			// columns because it breaks forward compatibility. Should we log
			// something here?
		}
	}

	keys, err := decodePostingKeys(minPostingKey, maxPostingKey)
	if err != nil {
		return err
	}
	pointer.PostingKeys = keys
	return nil
}

// decodePostingKeys returns the posting key range of a pointer from its
// encoded bounds. It returns the zero KeyRange if both bounds are nil, and an
// error if only one bound is nil.
func decodePostingKeys(minEncoded, maxEncoded []byte) (postings.KeyRange, error) {
	switch {
	case minEncoded == nil && maxEncoded == nil:
		return postings.KeyRange{}, nil
	case minEncoded == nil || maxEncoded == nil:
		return postings.KeyRange{}, fmt.Errorf("posting key range has only one bound: min=%t max=%t", minEncoded != nil, maxEncoded != nil)
	}

	keys, err := postings.DecodeKeyRange(minEncoded, maxEncoded)
	if err != nil {
		return postings.KeyRange{}, fmt.Errorf("decoding %s and %s: %w", ColumnTypeMinPostingKey, ColumnTypeMaxPostingKey, err)
	}
	return keys, nil
}

// decodeRunID returns the run ID with the given value. It returns an error if
// value is not positive, because writers store no value for the zero RunID.
func decodeRunID(value int64) (RunID, error) {
	if value <= 0 {
		return 0, fmt.Errorf("invalid %s value %d", ColumnTypeRunID, value)
	}
	return RunID(value), nil
}

// decodeShardBuckets returns the ShardBuckets with the given bucket count. It
// returns an error if buckets is not a valid bucket count.
func decodeShardBuckets(buckets int64) (ShardBuckets, error) {
	if buckets <= 0 || buckets > math.MaxUint32 {
		return ShardBuckets{}, fmt.Errorf("invalid %s value %d", ColumnTypeShardBuckets, buckets)
	}
	layout, err := streams.ShardLayoutFromBuckets(uint32(buckets))
	if err != nil {
		return ShardBuckets{}, fmt.Errorf("invalid %s value: %w", ColumnTypeShardBuckets, err)
	}
	return NewShardBuckets(layout), nil
}

func unsafeString(data []byte) string {
	return unsafe.String(unsafe.SliceData(data), len(data))
}
