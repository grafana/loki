package indexpointers_test

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/indexpointers"
)

var indexPointerTestData = []indexpointers.IndexPointer{
	{Path: "/path/to/index/object/1", StartTs: unixTime(10), EndTs: unixTime(20)},
	{Path: "/path/to/index/object/2", StartTs: unixTime(12), EndTs: unixTime(17)},
	{Path: "/path/to/index/object/3", StartTs: unixTime(13), EndTs: unixTime(18)},
}

func unixTime(sec int64) time.Time { return time.Unix(sec, 0) }

func TestRowReader(t *testing.T) {
	dec := buildIndexPointersDecoder(t, 100, 0) // Many pages
	r := indexpointers.NewRowReader(dec)
	actual, err := readAllIndexPointers(context.Background(), r)
	require.NoError(t, err)
	require.Equal(t, indexPointerTestData, actual)
}

func buildIndexPointersDecoder(t *testing.T, pageSize, pageRows int) *indexpointers.Section {
	t.Helper()

	s := indexpointers.NewBuilder(nil, pageSize, pageRows)
	for _, d := range indexPointerTestData {
		s.Append(d.Path, d.StartTs, d.EndTs)
	}

	builder := dataobj.NewBuilder(nil)
	require.NoError(t, builder.Append(s))

	obj, closer, err := builder.Flush()
	require.NoError(t, err)
	t.Cleanup(func() { closer.Close() })

	sec, err := indexpointers.Open(t.Context(), obj.Sections()[0])
	require.NoError(t, err)
	return sec
}

func readAllIndexPointers(ctx context.Context, r *indexpointers.RowReader) ([]indexpointers.IndexPointer, error) {
	var (
		res []indexpointers.IndexPointer
		buf = make([]indexpointers.IndexPointer, 128)
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
