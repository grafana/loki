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
	{Path: "testPath2", Section: 1, PointerKind: PointerKindColumnIndex, ColumnName: "testColumn", ColumnIndex: 1, ValuesBloomFilter: []byte{1, 2, 3}},
	{Path: "testPath2", Section: 2, PointerKind: PointerKindColumnIndex, ColumnName: "testColumn2", ColumnIndex: 2, ValuesBloomFilter: []byte{1, 2, 3, 4}},
	{Path: "testPath1", Section: 0, PointerKind: PointerKindStreamIndex, StreamID: 1, StreamIDRef: 3, StartTs: unixTime(10), EndTs: unixTime(15), LineCount: 2, UncompressedSize: 2},
	{Path: "testPath1", Section: 1, PointerKind: PointerKindStreamIndex, StreamID: 2, StreamIDRef: 4, StartTs: unixTime(12), EndTs: unixTime(17), LineCount: 2, UncompressedSize: 4},
	{Path: "testPath2", Section: 0, PointerKind: PointerKindStreamIndex, StreamID: 1, StreamIDRef: 5, StartTs: unixTime(13), EndTs: unixTime(18), LineCount: 2, UncompressedSize: 3},
}

func TestRowReader(t *testing.T) {
	dec := buildPointersDecoder(t, 100) // Many pages
	r := NewRowReader(dec)
	actual, err := readAllPointers(context.Background(), r)
	require.NoError(t, err)
	require.Equal(t, pointerTestData, actual)
}

func unixTime(sec int64) time.Time { return time.Unix(sec, 0) }

func buildPointersDecoder(t *testing.T, pageSize int) *Section {
	t.Helper()

	s := NewBuilder(nil, pageSize)
	for _, d := range pointerTestData {
		if d.PointerKind == PointerKindStreamIndex {
			s.ObserveStream(d.Path, d.Section, d.StreamIDRef, d.StreamID, d.StartTs, d.UncompressedSize)
			s.ObserveStream(d.Path, d.Section, d.StreamIDRef, d.StreamID, d.EndTs, 0)
		} else {
			s.RecordColumnIndex(d.Path, d.Section, d.ColumnName, d.ColumnIndex, d.ValuesBloomFilter)
		}
	}

	builder := dataobj.NewBuilder()
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
