package streams_test

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/apache/arrow-go/v18/arrow/scalar"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
	"github.com/grafana/loki/v3/pkg/util/arrowtest"
)

func TestReader(t *testing.T) {
	alloc := memory.NewCheckedAllocator(memory.DefaultAllocator)
	defer alloc.AssertSize(t, 0)

	expect := arrowtest.Rows{
		{
			"stream_id.int64":         int64(1),
			"app.label.utf8":          "foo",
			"cluster.label.utf8":      "test",
			"min_timestamp.timestamp": time.Unix(10, 0).UTC(),
			"max_timestamp.timestamp": time.Unix(15, 0).UTC(),
			"rows.int64":              int64(2),
			"uncompressed_size.int64": int64(25),
		},
		{
			"stream_id.int64":         int64(2),
			"app.label.utf8":          "bar",
			"cluster.label.utf8":      "test",
			"min_timestamp.timestamp": time.Unix(5, 0).UTC(),
			"max_timestamp.timestamp": time.Unix(20, 0).UTC(),
			"rows.int64":              int64(2),
			"uncompressed_size.int64": int64(45),
		},
		{
			"stream_id.int64":         int64(3),
			"app.label.utf8":          "baz",
			"cluster.label.utf8":      "test",
			"min_timestamp.timestamp": time.Unix(25, 0).UTC(),
			"max_timestamp.timestamp": time.Unix(30, 0).UTC(),
			"rows.int64":              int64(2),
			"uncompressed_size.int64": int64(35),
		},
	}

	sec := buildStreamsSection(t, 1)

	r := streams.NewReader(streams.ReaderOptions{
		Columns:    sec.Columns(),
		Predicates: nil,
		Allocator:  alloc,
	})

	actualTable, err := readTable(context.Background(), r)
	if actualTable != nil {
		defer actualTable.Release()
	}
	require.NoError(t, err)

	actual, err := arrowtest.TableRows(alloc, actualTable)
	require.NoError(t, err, "failed to get rows from table")
	require.Equal(t, expect, actual)
}

func TestReader_Predicate(t *testing.T) {
	alloc := memory.NewCheckedAllocator(memory.DefaultAllocator)
	defer alloc.AssertSize(t, 0)

	expect := arrowtest.Rows{
		{
			"stream_id.int64":         int64(2),
			"app.label.utf8":          "bar",
			"cluster.label.utf8":      "test",
			"min_timestamp.timestamp": time.Unix(5, 0).UTC(),
			"max_timestamp.timestamp": time.Unix(20, 0).UTC(),
			"rows.int64":              int64(2),
			"uncompressed_size.int64": int64(45),
		},
	}

	sec := buildStreamsSection(t, 1)

	appLabel := sec.Columns()[5]
	require.Equal(t, "app", appLabel.Name)
	require.Equal(t, streams.ColumnTypeLabel, appLabel.Type)

	r := streams.NewReader(streams.ReaderOptions{
		Columns: sec.Columns(),
		Predicates: []streams.Predicate{
			streams.EqualPredicate{
				Column: appLabel,
				Value:  scalar.NewBinaryScalar(memory.NewBufferBytes([]byte("bar")), arrow.BinaryTypes.Binary),
			},
		},
		Allocator: alloc,
	})

	actualTable, err := readTable(context.Background(), r)
	if actualTable != nil {
		defer actualTable.Release()
	}
	require.NoError(t, err)

	actual, err := arrowtest.TableRows(alloc, actualTable)
	require.NoError(t, err, "failed to get rows from table")
	require.Equal(t, expect, actual)
}

func TestReader_InPredicate(t *testing.T) {
	alloc := memory.NewCheckedAllocator(memory.DefaultAllocator)
	defer alloc.AssertSize(t, 0)

	expect := arrowtest.Rows{
		{
			"stream_id.int64":         int64(2),
			"app.label.utf8":          "bar",
			"cluster.label.utf8":      "test",
			"min_timestamp.timestamp": time.Unix(5, 0).UTC(),
			"max_timestamp.timestamp": time.Unix(20, 0).UTC(),
			"rows.int64":              int64(2),
			"uncompressed_size.int64": int64(45),
		},
	}

	sec := buildStreamsSection(t, 1)

	streamID := sec.Columns()[0]
	require.Equal(t, "", streamID.Name)
	require.Equal(t, streams.ColumnTypeStreamID, streamID.Type)

	r := streams.NewReader(streams.ReaderOptions{
		Columns: sec.Columns(),
		Predicates: []streams.Predicate{
			streams.InPredicate{
				Column: streamID,
				Values: []scalar.Scalar{
					scalar.NewInt64Scalar(2),
				},
			},
		},
		Allocator: alloc,
	})

	actualTable, err := readTable(context.Background(), r)
	if actualTable != nil {
		defer actualTable.Release()
	}
	require.NoError(t, err)

	actual, err := arrowtest.TableRows(alloc, actualTable)
	require.NoError(t, err, "failed to get rows from table")
	require.Equal(t, expect, actual)
}

func TestReader_ColumnSubset(t *testing.T) {
	alloc := memory.NewCheckedAllocator(memory.DefaultAllocator)
	defer alloc.AssertSize(t, 0)

	expect := arrowtest.Rows{
		{
			"stream_id.int64": int64(1),
			"app.label.utf8":  "foo",
		},
		{
			"stream_id.int64": int64(2),
			"app.label.utf8":  "bar",
		},
		{
			"stream_id.int64": int64(3),
			"app.label.utf8":  "baz",
		},
	}

	sec := buildStreamsSection(t, 1)

	var (
		streamID = sec.Columns()[0]
		appLabel = sec.Columns()[5]
	)

	require.Equal(t, "", streamID.Name)
	require.Equal(t, streams.ColumnTypeStreamID, streamID.Type)
	require.Equal(t, "app", appLabel.Name)
	require.Equal(t, streams.ColumnTypeLabel, appLabel.Type)

	r := streams.NewReader(streams.ReaderOptions{
		Columns:    []*streams.Column{streamID, appLabel},
		Predicates: nil,
		Allocator:  alloc,
	})

	actualTable, err := readTable(context.Background(), r)
	if actualTable != nil {
		defer actualTable.Release()
	}
	require.NoError(t, err)

	actual, err := arrowtest.TableRows(alloc, actualTable)
	require.NoError(t, err, "failed to get rows from table")
	require.Equal(t, expect, actual)
}

func readTable(ctx context.Context, r *streams.Reader) (arrow.Table, error) {
	var recs []arrow.Record

	for {
		rec, err := r.Read(ctx, 128)
		if rec != nil {
			if rec.NumRows() > 0 {
				recs = append(recs, rec)
			}
			defer rec.Release()
		}

		if err != nil && errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			return nil, err
		}
	}

	if len(recs) == 0 {
		return nil, io.EOF
	}

	return array.NewTableFromRecords(recs[0].Schema(), recs), nil
}
