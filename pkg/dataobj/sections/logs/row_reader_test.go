package logs

import (
	"context"
	"errors"
	"io"
	"slices"
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
)

func TestRowReader_NoPredicates(t *testing.T) {
	logsSection := buildSection(t)

	readBuf := make([]Record, 3)
	rowReader := NewRowReader(logsSection)
	require.NoError(t, rowReader.Open(context.Background()))
	n, err := rowReader.Read(context.Background(), readBuf)
	require.NoError(t, err)
	require.Equal(t, 2, n)
}

func TestRowReader_StreamIDPredicate(t *testing.T) {
	logsSection := buildSection(t)

	readBuf := make([]Record, 3)
	rowReader := NewRowReader(logsSection)

	err := rowReader.MatchStreams(slices.Values([]int64{1}))
	require.NoError(t, err)
	require.NoError(t, rowReader.Open(context.Background()))
	n, err := rowReader.Read(context.Background(), readBuf)
	require.NoError(t, err)
	require.Equal(t, 1, n)
}

func TestRowReader_ReadBeforeOpen(t *testing.T) {
	logsSection := buildSection(t)
	rowReader := NewRowReader(logsSection)

	readBuf := make([]Record, 1)
	n, err := rowReader.Read(context.Background(), readBuf)
	require.Zero(t, n)
	require.ErrorContains(t, err, "row reader not opened")
}

// TestTranslateLogsPredicate_AbsentMetadataColumn covers the reduction of a metadata predicate whose
// column is absent from the section. Every row then reads as an empty value for the key, so the
// predicate becomes keep-all (TruePredicate) when the empty value matches and drop-all (FalsePredicate)
// otherwise. Passing no columns makes every metadata key absent.
func TestTranslateLogsPredicate_AbsentMetadataColumn(t *testing.T) {
	tests := map[string]struct {
		pred RowPredicate
		want dataset.Predicate
	}{
		"equality with empty value keeps every row": {
			pred: MetadataMatcherRowPredicate{Key: "missing", Value: ""},
			want: dataset.TruePredicate{},
		},
		"equality with non-empty value drops every row": {
			pred: MetadataMatcherRowPredicate{Key: "missing", Value: "x"},
			want: dataset.FalsePredicate{},
		},
		"filter keeps every row when the empty value passes": {
			pred: MetadataFilterRowPredicate{Key: "missing", Keep: func(_, value string) bool { return value == "" }},
			want: dataset.TruePredicate{},
		},
		"filter drops every row when the empty value fails": {
			pred: MetadataFilterRowPredicate{Key: "missing", Keep: func(_, value string) bool { return value != "" }},
			want: dataset.FalsePredicate{},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, tc.want, translateLogsPredicate(tc.pred, nil, nil))
		})
	}
}

func TestRowReader_SetColumns(t *testing.T) {
	ctx := context.Background()
	records := []Record{
		{StreamID: 1, Timestamp: time.Unix(10, 0), Metadata: labels.FromStrings("trace_id", "abc"), Line: []byte("hello")},
		{StreamID: 2, Timestamp: time.Unix(20, 0), Metadata: labels.EmptyLabels(), Line: []byte("world")},
	}
	sec := buildLogsSection(t, records, SortStreamASC)

	read := func(t *testing.T, project func(*RowReader) error) []Record {
		t.Helper()
		r := NewRowReader(sec)
		defer r.Close()
		require.NoError(t, project(r))
		require.NoError(t, r.Open(ctx))

		var got []Record
		buf := make([]Record, 8)
		for {
			n, err := r.Read(ctx, buf)
			if err != nil && !errors.Is(err, io.EOF) {
				require.NoError(t, err)
			}
			for i := range buf[:n] {
				rec := buf[i]
				rec.Line = append([]byte(nil), buf[i].Line...) // copy: buf is reused across Read calls
				got = append(got, rec)
			}
			if n == 0 && errors.Is(err, io.EOF) {
				break
			}
		}
		return got
	}

	// Sorted [streamID ASC, timestamp DESC], so stream 1 then stream 2.
	t.Run("stream_id + timestamp only omits message and metadata", func(t *testing.T) {
		got := read(t, func(r *RowReader) error {
			return r.SetColumns([]ColumnType{ColumnTypeStreamID, ColumnTypeTimestamp}, nil)
		})
		require.Len(t, got, 2)

		require.Equal(t, int64(1), got[0].StreamID)
		require.Equal(t, time.Unix(10, 0).UnixNano(), got[0].Timestamp.UnixNano())
		require.Empty(t, got[0].Line, "message must not be decoded when not projected")
		require.Equal(t, 0, got[0].Metadata.Len(), "metadata must not be decoded when not projected")

		require.Equal(t, int64(2), got[1].StreamID)
		require.Equal(t, time.Unix(20, 0).UnixNano(), got[1].Timestamp.UnixNano())
	})

	t.Run("projecting message decodes the line", func(t *testing.T) {
		got := read(t, func(r *RowReader) error {
			return r.SetColumns([]ColumnType{ColumnTypeStreamID, ColumnTypeTimestamp, ColumnTypeMessage}, nil)
		})
		require.Len(t, got, 2)
		require.Equal(t, int64(1), got[0].StreamID)
		require.Equal(t, []byte("hello"), got[0].Line)
		require.Equal(t, 0, got[0].Metadata.Len(), "metadata still not projected")
	})

	t.Run("projecting a metadata key decodes it", func(t *testing.T) {
		got := read(t, func(r *RowReader) error {
			return r.SetColumns([]ColumnType{ColumnTypeStreamID, ColumnTypeTimestamp}, []string{"trace_id"})
		})
		require.Len(t, got, 2)
		require.Equal(t, "abc", got[0].Metadata.Get("trace_id"))
		require.Empty(t, got[0].Line, "message still not projected")
	})

	// The reader must project the columns its predicates need even when SetColumns omits them; otherwise
	// the predicate reduces to FalsePredicate and drops every row.
	t.Run("stream match works when SetColumns omits stream_id", func(t *testing.T) {
		got := read(t, func(r *RowReader) error {
			if err := r.MatchStreams(slices.Values([]int64{2})); err != nil {
				return err
			}
			return r.SetColumns([]ColumnType{ColumnTypeTimestamp}, nil) // stream_id deliberately omitted
		})
		require.Len(t, got, 1, "only the matched stream's row is returned, not zero rows")
		require.Equal(t, int64(2), got[0].StreamID)
	})

	t.Run("time-range predicate works when SetColumns omits timestamp", func(t *testing.T) {
		got := read(t, func(r *RowReader) error {
			if err := r.SetPredicates([]RowPredicate{TimeRangeRowPredicate{
				StartTime: time.Unix(15, 0), EndTime: time.Unix(25, 0), IncludeStart: true, IncludeEnd: true,
			}}); err != nil {
				return err
			}
			return r.SetColumns([]ColumnType{ColumnTypeStreamID}, nil) // timestamp deliberately omitted
		})
		require.Len(t, got, 1, "only the row inside the time range is returned, not zero rows")
		require.Equal(t, int64(2), got[0].StreamID)
	})

	// A metadata predicate is not auto-included: its key must be projected explicitly, else it reduces to
	// a const predicate. With the key projected, the pushed-down filter selects only the matching stream.
	t.Run("metadata predicate filters when its key is projected", func(t *testing.T) {
		got := read(t, func(r *RowReader) error {
			if err := r.SetPredicates([]RowPredicate{MetadataMatcherRowPredicate{Key: "trace_id", Value: "abc"}}); err != nil {
				return err
			}
			return r.SetColumns([]ColumnType{ColumnTypeStreamID, ColumnTypeTimestamp}, []string{"trace_id"})
		})
		require.Len(t, got, 1, "only the stream whose trace_id matches is returned")
		require.Equal(t, int64(1), got[0].StreamID)
	})

	// A time-range predicate nested under AND must still auto-include the timestamp column: if
	// predicateNeedsTimestamp did not recurse, both ranges would reduce to FalsePredicate and drop every row.
	t.Run("time-range nested in AND is detected when SetColumns omits timestamp", func(t *testing.T) {
		got := read(t, func(r *RowReader) error {
			if err := r.SetPredicates([]RowPredicate{AndRowPredicate{
				Left:  TimeRangeRowPredicate{StartTime: time.Unix(5, 0), EndTime: time.Unix(25, 0), IncludeStart: true, IncludeEnd: true},
				Right: TimeRangeRowPredicate{StartTime: time.Unix(15, 0), EndTime: time.Unix(35, 0), IncludeStart: true, IncludeEnd: true},
			}}); err != nil {
				return err
			}
			return r.SetColumns([]ColumnType{ColumnTypeStreamID}, nil) // timestamp omitted; the nested predicate still needs it
		})
		require.Len(t, got, 1, "the AND of the two ranges keeps only the in-range stream, not zero rows")
		require.Equal(t, int64(2), got[0].StreamID)
	})
}

func buildSection(t *testing.T) *Section {
	logsBuilder := NewBuilder(nil, BuilderOptions{
		StripeMergeLimit: 2,
		SortOrder:        SortStreamASC,
	})
	logsBuilder.Append(Record{
		StreamID:  1,
		Timestamp: time.Now(),
		Line:      []byte("test"),
	})
	logsBuilder.Append(Record{
		StreamID:  2,
		Timestamp: time.Now(),
		Line:      []byte("test2"),
	})

	b := dataobj.NewBuilder(nil)
	require.NoError(t, b.Append(logsBuilder))

	obj, closer, err := b.Flush()
	require.NoError(t, err)
	t.Cleanup(func() { closer.Close() })

	var logsSection *Section
	for _, section := range obj.Sections() {
		logsSection, err = Open(context.Background(), section)
		require.NoError(t, err)
	}
	return logsSection
}

// buildLogsSection builds a single logs section from the given records and sort order.
func buildLogsSection(t *testing.T, records []Record, order SortOrder) *Section {
	t.Helper()

	logsBuilder := NewBuilder(nil, BuilderOptions{
		PageSizeHint:     1024,
		BufferSize:       64,
		StripeMergeLimit: 2,
		SortOrder:        order,
	})
	for _, r := range records {
		logsBuilder.Append(r)
	}

	b := dataobj.NewBuilder(nil)
	require.NoError(t, b.Append(logsBuilder))
	obj, closer, err := b.Flush()
	require.NoError(t, err)
	t.Cleanup(func() { _ = closer.Close() })

	for _, section := range obj.Sections() {
		sec, err := Open(context.Background(), section)
		require.NoError(t, err)
		return sec
	}
	t.Fatal("no logs section in object")
	return nil
}
