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
	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
	"github.com/grafana/loki/v3/pkg/util/arrowtest"
)

// TestReader does a basic end-to-end test over a reader with a predicate applied.
func TestReader(t *testing.T) {
	alloc := memory.NewCheckedAllocator(memory.DefaultAllocator)
	defer alloc.AssertSize(t, 0)

	sec := buildSection(t, []logs.Record{
		{StreamID: 2, Timestamp: unixTime(40), Metadata: labels.FromStrings("trace_id", "789012"), Line: []byte("baz qux")},
		{StreamID: 2, Timestamp: unixTime(30), Metadata: labels.FromStrings("trace_id", "123456"), Line: []byte("foo bar")},
		{StreamID: 1, Timestamp: unixTime(20), Metadata: labels.FromStrings("trace_id", "abcdef"), Line: []byte("goodbye, world!")},
		{StreamID: 1, Timestamp: unixTime(10), Metadata: labels.EmptyLabels(), Line: []byte("hello, world!")},
	})

	var (
		streamID = sec.Columns()[0]
		traceID  = sec.Columns()[2]
		message  = sec.Columns()[3]
	)

	require.Equal(t, "", streamID.Name)
	require.Equal(t, logs.ColumnTypeStreamID, streamID.Type)
	require.Equal(t, "trace_id", traceID.Name)
	require.Equal(t, logs.ColumnTypeMetadata, traceID.Type)
	require.Equal(t, "", message.Name)
	require.Equal(t, logs.ColumnTypeMessage, message.Type)

	for _, tt := range []struct {
		name     string
		columns  []*logs.Column
		expected arrowtest.Rows
	}{
		{
			name:    "basic reads with predicate",
			columns: []*logs.Column{streamID, traceID, message},
			expected: arrowtest.Rows{
				{"stream_id.int64": int64(2), "trace_id.metadata.utf8": "123456", "message.utf8": "foo bar"},
				{"stream_id.int64": int64(1), "trace_id.metadata.utf8": "abcdef", "message.utf8": "goodbye, world!"},
			},
		},
		// tests that the reader evaluates predicates correctly even when predicate columns are not projected.
		{
			name:    "reads with predicate columns that are not projected",
			columns: []*logs.Column{streamID, message},
			expected: arrowtest.Rows{
				{"stream_id.int64": int64(2), "message.utf8": "foo bar"},
				{"stream_id.int64": int64(1), "message.utf8": "goodbye, world!"},
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := logs.NewReader(logs.ReaderOptions{
				Columns:   tt.columns,
				Allocator: alloc,
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
			if actualTable != nil {
				defer actualTable.Release()
			}
			require.NoError(t, err)

			actual, err := arrowtest.TableRows(alloc, actualTable)
			require.NoError(t, err, "failed to get rows from table")
			require.Equal(t, tt.expected, actual)
		})

	}
}

func buildSection(t *testing.T, recs []logs.Record) *logs.Section {
	t.Helper()

	sectionBuilder := logs.NewBuilder(nil, logs.BuilderOptions{
		PageSizeHint:     8192,
		BufferSize:       4192,
		StripeMergeLimit: 2,
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

// TestReaderStats tests that the reader properly tracks statistics
func TestReaderStats(t *testing.T) {
	alloc := memory.NewCheckedAllocator(memory.DefaultAllocator)
	defer alloc.AssertSize(t, 0)

	sec := buildSection(t, []logs.Record{
		{StreamID: 2, Timestamp: unixTime(40), Metadata: labels.FromStrings("trace_id", "789012"), Line: []byte("baz qux")},
		{StreamID: 2, Timestamp: unixTime(30), Metadata: labels.FromStrings("trace_id", "123456"), Line: []byte("foo bar")},
		{StreamID: 1, Timestamp: unixTime(20), Metadata: labels.FromStrings("trace_id", "abcdef"), Line: []byte("goodbye, world!")},
		{StreamID: 1, Timestamp: unixTime(10), Metadata: labels.EmptyLabels(), Line: []byte("hello, world!")},
	})

	var (
		streamID = sec.Columns()[0]
		traceID  = sec.Columns()[2]
		message  = sec.Columns()[3]
	)

	// Create a reader with predicates
	r := logs.NewReader(logs.ReaderOptions{
		Columns:   []*logs.Column{streamID, traceID, message},
		Allocator: alloc,
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

	// Create stats context
	statsCtx, ctx := stats.NewContext(context.Background())

	// Read the data
	actualTable, err := readTable(ctx, r)
	if actualTable != nil {
		defer actualTable.Release()
	}
	require.NoError(t, err)

	// Get the reader stats
	readerStats := r.Stats()

	// Verify the stats are properly populated
	require.Equal(t, int64(2), readerStats.ReadCalls)
	require.Equal(t, uint64(2), readerStats.PrimaryColumns) // from 2 predicates
	require.Equal(t, uint64(1), readerStats.SecondaryColumns)
	require.Equal(t, uint64(2), readerStats.PrimaryColumnPages)
	require.Equal(t, uint64(1), readerStats.SecondaryColumnPages)

	require.Equal(t, uint64(4), readerStats.MaxRows)
	require.Equal(t, uint64(4), readerStats.RowsToReadAfterPruning)
	require.Equal(t, uint64(4), readerStats.PrimaryRowsRead)
	require.Equal(t, uint64(2), readerStats.SecondaryRowsRead) // 2 rows pass the predicate

	// Verify download stats - these should be populated by the downloader
	require.Equal(t, uint64(3), readerStats.DownloadStats.PagesScanned) // one page per column
	require.Equal(t, uint64(2), readerStats.DownloadStats.PrimaryColumnPages)
	require.Equal(t, uint64(1), readerStats.DownloadStats.SecondaryColumnPages)

	// Verify global stats are updated
	result := statsCtx.Result(0, 0, 0)
	require.Equal(t, int64(4), result.Querier.Store.Dataobj.PrePredicateDecompressedRows)
	require.Equal(t, int64(2), result.Querier.Store.Dataobj.PostPredicateRows)
}
