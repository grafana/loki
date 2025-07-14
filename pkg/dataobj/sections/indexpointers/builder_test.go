package indexpointers

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj"
)

func TestBuilder(t *testing.T) {
	type pointers struct {
		path  string
		start time.Time
		end   time.Time
	}

	pp := []pointers{
		{path: "foo", start: unixTime(10), end: unixTime(20)},
		{path: "bar", start: unixTime(10), end: unixTime(20)},
	}

	ib := NewBuilder(nil, 1024)
	for _, p := range pp {
		ib.Append(p.path, p.start, p.end)
	}

	var buf bytes.Buffer
	b := dataobj.NewBuilder()
	err := b.Append(ib)
	require.NoError(t, err)

	_, err = b.Flush(&buf)
	require.NoError(t, err)

	expect := []IndexPointer{
		{
			Path:    "foo",
			StartTs: unixTime(10),
			EndTs:   unixTime(20),
		},
		{
			Path:    "bar",
			StartTs: unixTime(10),
			EndTs:   unixTime(20),
		},
	}

	bufBytes := buf.Bytes()
	obj, err := dataobj.FromReaderAt(bytes.NewReader(bufBytes), int64(len(bufBytes)))
	require.NoError(t, err)

	var actual []IndexPointer
	for result := range Iter(context.Background(), obj) {
		pointer, err := result.Value()
		require.NoError(t, err)
		actual = append(actual, pointer)
	}

	require.Equal(t, expect, actual)
}
