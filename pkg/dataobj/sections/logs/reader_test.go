package logs_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/apache/arrow-go/v18/arrow/scalar"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/util/arrowtest"
)

// TestReader does a basic end-to-end test over a reader with a predicate applied.
func TestReader(t *testing.T) {
	sec := buildSection(t, []logs.Record{
		{StreamID: 2, Timestamp: unixTime(40), Metadata: labels.FromStrings("trace_id", "789012"), Line: []byte("baz qux")},
		{StreamID: 2, Timestamp: unixTime(30), Metadata: labels.FromStrings("trace_id", "123456"), Line: []byte("foo bar")},
		{StreamID: 1, Timestamp: unixTime(20), Metadata: labels.FromStrings("trace_id", "abcdef"), Line: []byte("goodbye, world!")},
		{StreamID: 1, Timestamp: unixTime(10), Metadata: labels.EmptyLabels(), Line: []byte("hello, world!")},
		{StreamID: 1, Timestamp: unixTime(5), Metadata: labels.FromStrings("trace_id", "abcdef", "foo", ""), Line: []byte("")},
	})

	var (
		streamID = sec.Columns()[0]
		foo      = sec.Columns()[2]
		traceID  = sec.Columns()[3]
		message  = sec.Columns()[4]
	)

	require.Equal(t, "", streamID.Name)
	require.Equal(t, logs.ColumnTypeStreamID, streamID.Type)
	require.Equal(t, "trace_id", traceID.Name)
	require.Equal(t, logs.ColumnTypeMetadata, traceID.Type)
	require.Equal(t, "foo", foo.Name)
	require.Equal(t, logs.ColumnTypeMetadata, foo.Type)
	require.Equal(t, "", message.Name)
	require.Equal(t, logs.ColumnTypeMessage, message.Type)

	for _, tt := range []struct {
		name     string
		columns  []*logs.Column
		expected arrowtest.Rows
	}{
		{
			name:    "basic reads with predicate",
			columns: []*logs.Column{streamID, traceID, foo, message},
			expected: arrowtest.Rows{
				{"stream_id.int64": int64(1), "foo.metadata.utf8": nil, "trace_id.metadata.utf8": "abcdef", "message.utf8": "goodbye, world!"},
				{"stream_id.int64": int64(1), "foo.metadata.utf8": "", "trace_id.metadata.utf8": "abcdef", "message.utf8": ""},
				{"stream_id.int64": int64(2), "foo.metadata.utf8": nil, "trace_id.metadata.utf8": "123456", "message.utf8": "foo bar"},
			},
		},
		// tests that the reader evaluates predicates correctly even when predicate columns are not projected.
		{
			name:    "reads with predicate columns that are not projected",
			columns: []*logs.Column{streamID, message},
			expected: arrowtest.Rows{
				{"stream_id.int64": int64(1), "message.utf8": "goodbye, world!"},
				{"stream_id.int64": int64(1), "message.utf8": ""},
				{"stream_id.int64": int64(2), "message.utf8": "foo bar"},
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := logs.NewReader(logs.ReaderOptions{
				Columns:   tt.columns,
				Allocator: memory.DefaultAllocator,
				Predicates: []logs.Predicate{
					logs.FuncPredicate{
						Column: traceID,
						Keep: func(_ *logs.Column, value scalar.Scalar) bool {
							if !value.IsValid() {
								return false
							}

							bb := value.(*scalar.String).Value.Bytes()
							return bytes.Equal(bb, []byte("abcdef")) || bytes.Equal(bb, []byte("123456"))
						},
					},
					logs.InPredicate{
						Column: streamID,
						Values: []scalar.Scalar{
							scalar.NewInt64Scalar(1),
							scalar.NewInt64Scalar(2),
						},
					},
				},
			})

			actualTable, err := readTable(context.Background(), r)
			require.NoError(t, err)

			actual, err := arrowtest.TableRows(memory.DefaultAllocator, actualTable)
			require.NoError(t, err, "failed to get rows from table")
			require.Equal(t, tt.expected, actual)
		})
	}
}

func TestReader_ReadBeforeOpen(t *testing.T) {
	sec := buildSection(t, []logs.Record{
		{StreamID: 1, Timestamp: unixTime(10), Line: []byte("hello")},
	})

	r := logs.NewReader(logs.ReaderOptions{
		Columns:   sec.Columns(),
		Allocator: memory.DefaultAllocator,
	})

	rec, err := r.Read(context.Background(), 128)
	require.Nil(t, rec)
	require.ErrorContains(t, err, "reader not opened")
}

func buildSection(t *testing.T, recs []logs.Record) *logs.Section {
	t.Helper()

	sectionBuilder := logs.NewBuilder(nil, logs.BuilderOptions{
		PageSizeHint:     8192,
		BufferSize:       4192,
		StripeMergeLimit: 2,
		SortOrder:        logs.SortStreamASC,
	})

	for _, rec := range recs {
		sectionBuilder.Append(rec)
	}

	objectBuilder := dataobj.NewBuilder(nil)
	require.NoError(t, objectBuilder.Append(sectionBuilder))

	obj, closer, err := objectBuilder.Flush()
	require.NoError(t, err)
	t.Cleanup(func() { closer.Close() })

	sec, err := logs.Open(t.Context(), obj.Sections()[0])
	require.NoError(t, err)
	return sec
}

func unixTime(sec int64) time.Time { return time.Unix(sec, 0) }

func readTable(ctx context.Context, r *logs.Reader) (arrow.Table, error) {
	var recs []arrow.RecordBatch
	if err := r.Open(ctx); err != nil {
		return nil, err
	}

	for {
		rec, err := r.Read(ctx, 128)
		if rec != nil {
			if rec.NumRows() > 0 {
				recs = append(recs, rec)
			}
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
