package pointers

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj"
)

var pointerTestData = []SectionPointer{
	{Path: "testPath1", Section: 0, PointerKind: PointerKindStreamIndex, StreamID: 1, StreamIDRef: 3, StartTs: unixTime(10), EndTs: unixTime(15), LineCount: 2, UncompressedSize: 2},
	{Path: "testPath2", Section: 0, PointerKind: PointerKindStreamIndex, StreamID: 1, StreamIDRef: 5, StartTs: unixTime(13), EndTs: unixTime(18), LineCount: 2, UncompressedSize: 3},
	{Path: "testPath1", Section: 1, PointerKind: PointerKindStreamIndex, StreamID: 2, StreamIDRef: 4, StartTs: unixTime(12), EndTs: unixTime(17), LineCount: 2, UncompressedSize: 4},
	{Path: "testPath2", Section: 1, PointerKind: PointerKindColumnIndex, ColumnName: "testColumn", ColumnIndex: 1, ValuesBloomFilter: []byte{1, 2, 3}},
	{Path: "testPath2", Section: 2, PointerKind: PointerKindColumnIndex, ColumnName: "testColumn2", ColumnIndex: 2, ValuesBloomFilter: []byte{1, 2, 3, 4}},
}

func TestRowReader(t *testing.T) {
	dec := buildPointersDecoder(t, 0, 2) // 3 pages
	r := NewRowReader(dec)
	actual, err := readAllPointers(context.Background(), r)
	require.NoError(t, err)
	require.Equal(t, pointerTestData, actual)
}

func TestRowReaderTimeRange(t *testing.T) {
	var streamTestData []SectionPointer
	for _, d := range pointerTestData {
		if d.PointerKind == PointerKindStreamIndex {
			streamTestData = append(streamTestData, d)
		}
	}

	tests := []struct {
		name      string
		predicate TimeRangeRowPredicate
		want      []SectionPointer
	}{
		{
			name:      "no match",
			predicate: TimeRangeRowPredicate{Start: unixTime(100), End: unixTime(200)},
			want:      nil,
		},
		{
			name:      "all match",
			predicate: TimeRangeRowPredicate{Start: unixTime(0), End: unixTime(20)},
			want:      streamTestData,
		},
		{
			name:      "partial match",
			predicate: TimeRangeRowPredicate{Start: unixTime(16), End: unixTime(18)},
			want: []SectionPointer{
				streamTestData[1],
				streamTestData[2],
			},
		},
		{
			name:      "end predicate equal start of stream",
			predicate: TimeRangeRowPredicate{Start: unixTime(0), End: unixTime(10)},
			want: []SectionPointer{
				streamTestData[0],
			},
		},
		{
			name:      "start predicate equal end of stream",
			predicate: TimeRangeRowPredicate{Start: unixTime(18), End: unixTime(100)},
			want: []SectionPointer{
				streamTestData[1],
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dec := buildPointersDecoder(t, 0, 2)
			r := NewRowReader(dec)
			err := r.SetPredicate(tt.predicate)
			require.NoError(t, err)
			actual, err := readAllPointers(context.Background(), r)
			require.NoError(t, err)
			require.Equal(t, tt.want, actual)
		})
	}
}

func unixTime(sec int64) time.Time { return time.Unix(sec, 0) }

func buildPointersDecoder(t *testing.T, pageSize, pageRows int) *Section {
	t.Helper()

	s := NewBuilder(nil, pageSize, pageRows)
	for _, d := range pointerTestData {
		if d.PointerKind == PointerKindStreamIndex {
			s.ObserveStream(d.Path, d.Section, d.StreamIDRef, d.StreamID, d.StartTs, d.UncompressedSize)
			s.ObserveStream(d.Path, d.Section, d.StreamIDRef, d.StreamID, d.EndTs, 0)
		} else {
			s.RecordColumnIndex(d.Path, d.Section, d.ColumnName, d.ColumnIndex, d.ValuesBloomFilter)
		}
	}

	builder := dataobj.NewBuilder(nil)
	require.NoError(t, builder.Append(s))

	obj, closer, err := builder.Flush()
	require.NoError(t, err)
	t.Cleanup(func() { closer.Close() })

	sec, err := Open(t.Context(), obj.Sections()[0])
	require.NoError(t, err)
	return sec
}

func readAllPointers(ctx context.Context, r *RowReader) ([]SectionPointer, error) {
	var (
		res []SectionPointer
		buf = make([]SectionPointer, 128)
	)

	for {
		n, err := r.Read(ctx, buf)
		if n > 0 {
			res = append(res, buf[:n]...)
		}
		if errors.Is(err, io.EOF) {
			return res, nil
		} else if err != nil {
			return res, err
		}

		clear(buf)
	}
}
