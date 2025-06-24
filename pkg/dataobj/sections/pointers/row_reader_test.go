package pointers

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj"
)

var pointerTestData = []ObjPointer{
	{Path: "testPath2", Section: 2, Column: "testColumn", ValuesBloomFilter: []byte{1, 2, 3}},
	{Path: "testPath2", Section: 2, Column: "testColumn2", ValuesBloomFilter: []byte{1, 2, 3, 4}},
	{Path: "testPath1", Section: 1, StreamID: 1, StreamIDRef: 3, StartTs: unixTime(10), EndTs: unixTime(15), LineCount: 2, UncompressedSize: 2},
	{Path: "testPath1", Section: 2, StreamID: 2, StreamIDRef: 4, StartTs: unixTime(12), EndTs: unixTime(17), LineCount: 2, UncompressedSize: 4},
	{Path: "testPath2", Section: 1, StreamID: 1, StreamIDRef: 5, StartTs: unixTime(13), EndTs: unixTime(18), LineCount: 2, UncompressedSize: 3},
}

func TestRowReader(t *testing.T) {
	dec := buildPointersDecoder(t, 300)
	r := NewRowReader(dec)
	actual, err := readAllPointers(context.Background(), r)
	require.NoError(t, err)
	td := pointerTestData
	require.Equal(t, td, actual)
}

func unixTime(sec int64) time.Time { return time.Unix(sec, 0) }

func buildPointersDecoder(t *testing.T, pageSize int) *Section {
	t.Helper()

	pointerBuilder := NewBuilder(nil, pageSize)
	for _, d := range pointerTestData {
		if d.StreamID != 0 {
			pointerBuilder.RecordStreamRef(d.Path, d.StreamIDRef, d.StreamID)
			// Observe the stream twice to record both the start and the end.
			pointerBuilder.ObserveStream(d.Path, d.Section, d.StreamIDRef, d.StartTs, d.UncompressedSize)
			pointerBuilder.ObserveStream(d.Path, d.Section, d.StreamIDRef, d.EndTs, 0)
		} else {
			pointerBuilder.RecordColumnIndex(d.Path, d.Section, d.Column, d.ValuesBloomFilter)
		}
	}

	var buf bytes.Buffer

	objectBuilder := dataobj.NewBuilder()
	require.NoError(t, objectBuilder.Append(pointerBuilder))

	_, err := objectBuilder.Flush(&buf)
	require.NoError(t, err)

	obj, err := dataobj.FromReaderAt(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	require.NoError(t, err)

	sec, err := Open(t.Context(), obj.Sections()[0])
	require.NoError(t, err)
	return sec
}

func readAllPointers(ctx context.Context, r *RowReader) ([]ObjPointer, error) {
	var (
		res []ObjPointer
		buf = make([]ObjPointer, 128)
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
