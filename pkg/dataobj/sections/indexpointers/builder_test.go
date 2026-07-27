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
		ib.Append(p.path, p.start, p.end, 0, 0)
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

func TestBuilder_EncodesSizeColumns(t *testing.T) {
	b := indexpointers.NewBuilder(nil, 1024, 100)
	b.SetTenant(tenantID)
	b.Append("indexes/aa/one", unixTime(10), unixTime(20), 4096, 1<<20)
	b.Append("indexes/aa/two", unixTime(30), unixTime(40), 0, 0)

	objectBuilder := dataobj.NewBuilder(nil)
	require.NoError(t, objectBuilder.Append(b))

	obj, closer, err := objectBuilder.Flush()
	require.NoError(t, err)
	defer closer.Close()

	var got []indexpointers.TenantIndexPointer
	for result := range indexpointers.Iter(context.Background(), obj) {
		pointer, err := result.Value()
		require.NoError(t, err)
		got = append(got, pointer)
	}

	require.Len(t, got, 2)
	require.Equal(t, uint64(4096), got[0].FileSize)
	require.Equal(t, uint64(1<<20), got[0].UncompressedLogsSize)
	require.Equal(t, uint64(0), got[1].FileSize)
	require.Equal(t, uint64(0), got[1].UncompressedLogsSize)
}
