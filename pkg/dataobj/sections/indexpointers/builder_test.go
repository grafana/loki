package indexpointers_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/indexpointers"
)

const tenantID = "tenantID"

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
	ib.SetTenant(tenantID)
	for _, p := range pp {
		ib.Append(indexpointers.IndexPointer{Path: p.path, StartTs: p.start, EndTs: p.end})
	}

	b := dataobj.NewBuilder(nil)
	require.NoError(t, b.Append(ib))

	obj, closer, err := b.Flush()
	require.NoError(t, err)
	defer closer.Close()

	expect := []indexpointers.TenantIndexPointer{
		{
			Tenant: tenantID,
			IndexPointer: indexpointers.IndexPointer{
				Path:    "foo",
				StartTs: unixTime(10),
				EndTs:   unixTime(20),
			},
		},
		{
			Tenant: tenantID,
			IndexPointer: indexpointers.IndexPointer{
				Path:    "bar",
				StartTs: unixTime(10),
				EndTs:   unixTime(20),
			},
		},
	}

	var actual []indexpointers.TenantIndexPointer
	for result := range indexpointers.Iter(context.Background(), obj) {
		pointer, err := result.Value()
		require.NoError(t, err)
		actual = append(actual, pointer)
	}

	require.Equal(t, expect, actual)
}

func TestBuilder_RunLength(t *testing.T) {
	encode := func(pointer indexpointers.IndexPointer) error {
		ib := indexpointers.NewBuilder(nil, 1024, 0)
		ib.SetTenant(tenantID)
		ib.Append(pointer)
		return dataobj.NewBuilder(nil).Append(ib)
	}
	pointer := func(runID indexpointers.RunID, runLength int32) indexpointers.IndexPointer {
		return indexpointers.IndexPointer{Path: "foo", StartTs: unixTime(10), EndTs: unixTime(20), RunID: runID, RunLength: runLength}
	}

	t.Run("encodes a positive run length of a pointer with a run ID", func(t *testing.T) {
		require.NoError(t, encode(pointer(1, 2)))
	})

	t.Run("returns an error when the run length is set without a run ID", func(t *testing.T) {
		require.ErrorContains(t, encode(pointer(0, 2)), "without a run ID")
	})

	t.Run("returns an error when the run length is negative", func(t *testing.T) {
		require.ErrorContains(t, encode(pointer(1, -1)), "invalid run length")
	})

}
