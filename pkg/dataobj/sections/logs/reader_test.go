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
	"github.com/grafana/loki/v3/pkg/dataobj/internal/util/arrowtest"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
)

// TestReader does a basic end-to-end test over a reader with a predicate applied.
func TestReader(t *testing.T) {
	alloc := memory.NewCheckedAllocator(memory.DefaultAllocator)
	defer alloc.AssertSize(t, 0)

	sec := buildSection(t, []logs.Record{
		{StreamID: 1, Timestamp: unixTime(10), Metadata: nil, Line: []byte("hello, world!")},
		{StreamID: 1, Timestamp: unixTime(20), Metadata: labels.FromStrings("trace_id", "abcdef"), Line: []byte("goodbye, world!")},
		{StreamID: 2, Timestamp: unixTime(30), Metadata: labels.FromStrings("trace_id", "123456"), Line: []byte("foo bar")},
		{StreamID: 2, Timestamp: unixTime(40), Metadata: labels.FromStrings("trace_id", "789012"), Line: []byte("baz qux")},
	})

	var (
		traceID = sec.Columns()[2]
		message = sec.Columns()[3]
	)

	require.Equal(t, "trace_id", traceID.Name)
	require.Equal(t, logs.ColumnTypeMetadata, traceID.Type)
	require.Equal(t, "", message.Name)
	require.Equal(t, logs.ColumnTypeMessage, message.Type)

	r := logs.NewReader(logs.ReaderOptions{
		Columns:   []*logs.Column{traceID, message},
		Allocator: alloc,
		Predicates: []logs.Predicate{
			logs.FuncPredicate{
				Column: traceID,
				Keep: func(_ *logs.Column, value scalar.Scalar) bool {
					if !value.IsValid() {
						return false
					}

					bb := value.(*scalar.Binary).Value.Bytes()
					return bytes.Equal(bb, []byte("abcdef")) || bytes.Equal(bb, []byte("123456"))
				},
			},
		},
	})

	expect := arrowtest.Rows{
		{"trace_id.metadata.binary": []byte("abcdef"), "message.binary": []byte("goodbye, world!")},
		{"trace_id.metadata.binary": []byte("123456"), "message.binary": []byte("foo bar")},
	}

	actualTable, err := readTable(context.Background(), r)
	if actualTable != nil {
		defer actualTable.Release()
	}
	require.NoError(t, err)

	actual, err := arrowtest.TableRows(alloc, actualTable)
	require.NoError(t, err, "failed to get rows from table")
	require.Equal(t, expect, actual)
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

	objectBuilder := dataobj.NewBuilder()
	require.NoError(t, objectBuilder.Append(sectionBuilder))

	var buf bytes.Buffer
	_, err := objectBuilder.Flush(&buf)
	require.NoError(t, err)

	obj, err := dataobj.FromReaderAt(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	require.NoError(t, err)

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
