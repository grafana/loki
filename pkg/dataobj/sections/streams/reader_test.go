package streams_test

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/apache/arrow-go/v18/arrow/scalar"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/util/arrowtest"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
)

func TestReader(t *testing.T) {
	alloc := memory.NewCheckedAllocator(memory.DefaultAllocator)
	defer alloc.AssertSize(t, 0)

	expect := arrowtest.Rows{
		{
			"stream_id.int64":         int64(1),
			"app.label.binary":        []byte("foo"),
			"cluster.label.binary":    []byte("test"),
			"min_timestamp.timestamp": arrowUnixTime(10),
			"max_timestamp.timestamp": arrowUnixTime(15),
			"rows.int64":              int64(2),
			"uncompressed_size.int64": int64(25),
		},
		{
			"stream_id.int64":         int64(2),
			"app.label.binary":        []byte("bar"),
			"cluster.label.binary":    []byte("test"),
			"min_timestamp.timestamp": arrowUnixTime(5),
			"max_timestamp.timestamp": arrowUnixTime(20),
			"rows.int64":              int64(2),
			"uncompressed_size.int64": int64(45),
		},
		{
			"stream_id.int64":         int64(3),
			"app.label.binary":        []byte("baz"),
			"cluster.label.binary":    []byte("test"),
			"min_timestamp.timestamp": arrowUnixTime(25),
			"max_timestamp.timestamp": arrowUnixTime(30),
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
			"app.label.binary":        []byte("bar"),
			"cluster.label.binary":    []byte("test"),
			"min_timestamp.timestamp": arrowUnixTime(5),
			"max_timestamp.timestamp": arrowUnixTime(20),
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

func TestReader_ColumnSubset(t *testing.T) {
	alloc := memory.NewCheckedAllocator(memory.DefaultAllocator)
	defer alloc.AssertSize(t, 0)

	expect := arrowtest.Rows{
		{
			"stream_id.int64":  int64(1),
			"app.label.binary": []byte("foo"),
		},
		{
			"stream_id.int64":  int64(2),
			"app.label.binary": []byte("bar"),
		},
		{
			"stream_id.int64":  int64(3),
			"app.label.binary": []byte("baz"),
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

func arrowUnixTime(sec int64) string {
	return arrowtest.Time(unixTime(sec).UTC())
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
