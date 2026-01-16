package indexpointers_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/indexpointers"
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

	ib := indexpointers.NewBuilder(nil, 1024, 0)
	for _, p := range pp {
		ib.Append(p.path, p.start, p.end)
	}

	b := dataobj.NewBuilder(nil)
	require.NoError(t, b.Append(ib))

	obj, closer, err := b.Flush()
	require.NoError(t, err)
	defer closer.Close()

	expect := []indexpointers.IndexPointer{
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

	var actual []indexpointers.IndexPointer
	for result := range indexpointers.Iter(context.Background(), obj) {
		pointer, err := result.Value()
		require.NoError(t, err)
		actual = append(actual, pointer)
	}

	require.Equal(t, expect, actual)
}
