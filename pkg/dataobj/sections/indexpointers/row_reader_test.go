package indexpointers

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj"
)

var indexPointerTestData = []IndexPointer{
	{Path: "/path/to/index/object/1", StartTs: unixTime(10), EndTs: unixTime(20)},
	{Path: "/path/to/index/object/2", StartTs: unixTime(12), EndTs: unixTime(17)},
	{Path: "/path/to/index/object/3", StartTs: unixTime(13), EndTs: unixTime(18)},
}

func TestDumpRowReader(t *testing.T) {
	path := "/Users/benclive/Downloads/2025-07-18T00_00_00Z.store"
	fileBytes, err := os.ReadFile(path)
	require.NoError(t, err)

	obj, err := dataobj.FromReaderAt(bytes.NewReader(fileBytes), int64(len(fileBytes)))
	require.NoError(t, err)

	for _, section := range obj.Sections().Filter(CheckSection) {
		sec, err := Open(t.Context(), section)
		require.NoError(t, err)

		r := NewRowReader(sec)
		r.SetPredicate(TimeRangeRowPredicate{
			Start: time.Date(2025, 7, 18, 1, 30, 0, 0, time.UTC),
			End:   time.Date(2025, 7, 18, 1, 59, 59, 0, time.UTC),
		})
		buf := make([]IndexPointer, 128)
		for {
			n, err := r.Read(t.Context(), buf)
			if errors.Is(err, io.EOF) {
				break
			}
			require.NoError(t, err)
			for _, pointer := range buf[:n] {
				fmt.Printf("Path=%s StartTs=%s EndTs=%s\n", pointer.Path, pointer.StartTs.UTC().Format(time.RFC3339), pointer.EndTs.UTC().Format(time.RFC3339))
			}
		}
	}
}

func unixTime(sec int64) time.Time { return time.Unix(sec, 0) }

func TestRowReader(t *testing.T) {
	dec := buildIndexPointersDecoder(t, 100) // Many pages
	r := NewRowReader(dec)
	actual, err := readAllIndexPointers(context.Background(), r)
	require.NoError(t, err)
	require.Equal(t, indexPointerTestData, actual)
}

func buildIndexPointersDecoder(t *testing.T, pageSize int) *Section {
	t.Helper()

	s := NewBuilder(nil, pageSize)
	for _, d := range indexPointerTestData {
		s.Append(d.Path, d.StartTs, d.EndTs)
	}

	var buf bytes.Buffer

	builder := dataobj.NewBuilder()
	require.NoError(t, builder.Append(s))

	_, err := builder.Flush(&buf)
	require.NoError(t, err)

	obj, err := dataobj.FromReaderAt(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	require.NoError(t, err)

	sec, err := Open(t.Context(), obj.Sections()[0])
	require.NoError(t, err)
	return sec
}

func readAllIndexPointers(ctx context.Context, r *RowReader) ([]IndexPointer, error) {
	var (
		res []IndexPointer
		buf = make([]IndexPointer, 128)
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
