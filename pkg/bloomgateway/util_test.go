package bloomgateway

import (
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/logproto"
)

func TestSliceIterWithIndex(t *testing.T) {
	t.Run("SliceIterWithIndex implements v1.PeekingIterator interface", func(t *testing.T) {
		xs := []string{"a", "b", "c"}
		it := NewSliceIterWithIndex(xs, 123)

		// peek at first item
		p, ok := it.Peek()
		require.True(t, ok)
		require.Equal(t, "a", p.val)
		require.Equal(t, 123, p.idx)

		// proceed to first item
		require.True(t, it.Next())
		require.Equal(t, "a", it.At().val)
		require.Equal(t, 123, it.At().idx)

		// proceed to second and third item
		require.True(t, it.Next())
		require.True(t, it.Next())

		// peek at non-existing fourth item
		p, ok = it.Peek()
		require.False(t, ok)
		require.Equal(t, "", p.val) // "" is zero value for type string
		require.Equal(t, 123, p.idx)
	})
}

func TestFilterRequestForDay(t *testing.T) {
	tenant := "fake"

	// Thu Nov 09 2023 10:56:50 UTC
	ts := model.TimeFromUnix(1699523810)

	t.Run("filter chunk refs that fall into the day range", func(t *testing.T) {
		input := &logproto.FilterChunkRefRequest{
			From:    ts.Add(-168 * time.Hour), // 1w ago
			Through: ts,
			Refs: []*logproto.GroupedChunkRefs{
				{Fingerprint: 100, Tenant: tenant, Refs: []*logproto.ShortRef{
					{From: ts.Add(-168 * time.Hour), Through: ts.Add(-167 * time.Hour), Checksum: 100},
					{From: ts.Add(-143 * time.Hour), Through: ts.Add(-142 * time.Hour), Checksum: 101},
				}},
				{Fingerprint: 200, Tenant: tenant, Refs: []*logproto.ShortRef{
					{From: ts.Add(-144 * time.Hour), Through: ts.Add(-143 * time.Hour), Checksum: 200},
					{From: ts.Add(-119 * time.Hour), Through: ts.Add(-118 * time.Hour), Checksum: 201},
				}},
				{Fingerprint: 300, Tenant: tenant, Refs: []*logproto.ShortRef{
					{From: ts.Add(-120 * time.Hour), Through: ts.Add(-119 * time.Hour), Checksum: 300},
					{From: ts.Add(-95 * time.Hour), Through: ts.Add(-94 * time.Hour), Checksum: 301},
				}},
				{Fingerprint: 400, Tenant: tenant, Refs: []*logproto.ShortRef{
					{From: ts.Add(-96 * time.Hour), Through: ts.Add(-95 * time.Hour), Checksum: 400},
					{From: ts.Add(-71 * time.Hour), Through: ts.Add(-70 * time.Hour), Checksum: 401},
				}},
				{Fingerprint: 500, Tenant: tenant, Refs: []*logproto.ShortRef{
					{From: ts.Add(-72 * time.Hour), Through: ts.Add(-71 * time.Hour), Checksum: 500},
					{From: ts.Add(-47 * time.Hour), Through: ts.Add(-46 * time.Hour), Checksum: 501},
				}},
				{Fingerprint: 600, Tenant: tenant, Refs: []*logproto.ShortRef{
					{From: ts.Add(-48 * time.Hour), Through: ts.Add(-47 * time.Hour), Checksum: 600},
					{From: ts.Add(-23 * time.Hour), Through: ts.Add(-22 * time.Hour), Checksum: 601},
				}},
				{Fingerprint: 700, Tenant: tenant, Refs: []*logproto.ShortRef{
					{From: ts.Add(-24 * time.Hour), Through: ts.Add(-23 * time.Hour), Checksum: 700},
					{From: ts.Add(-1 * time.Hour), Through: ts, Checksum: 701},
				}},
			},
			Filters: []*logproto.LineFilterExpression{
				{Operator: 1, Match: "foo"},
				{Operator: 1, Match: "bar"},
			},
		}

		// day ranges from ts-48h to ts-24h
		day := getDayTime(ts.Add(-36 * time.Hour))

		expected := &logproto.FilterChunkRefRequest{
			From:    ts.Add(-48 * time.Hour),
			Through: ts.Add(-46 * time.Hour),
			Refs: []*logproto.GroupedChunkRefs{
				{Fingerprint: 500, Tenant: tenant, Refs: []*logproto.ShortRef{
					{From: ts.Add(-47 * time.Hour), Through: ts.Add(-46 * time.Hour), Checksum: 501},
				}},
				{Fingerprint: 600, Tenant: tenant, Refs: []*logproto.ShortRef{
					{From: ts.Add(-48 * time.Hour), Through: ts.Add(-47 * time.Hour), Checksum: 600},
				}},
			},
			Filters: []*logproto.LineFilterExpression{
				{Operator: 1, Match: "foo"},
				{Operator: 1, Match: "bar"},
			},
		}

		output := filterRequestForDay(input, day)
		require.Equal(t, expected, output)
	})

	t.Run("empty response returns time range from input day", func(t *testing.T) {
		input := &logproto.FilterChunkRefRequest{
			From:    ts.Add(-168 * time.Hour), // 1w ago
			Through: ts,
			Refs: []*logproto.GroupedChunkRefs{
				{Fingerprint: 100, Tenant: tenant, Refs: []*logproto.ShortRef{
					{From: ts.Add(-168 * time.Hour), Through: ts.Add(-167 * time.Hour), Checksum: 100},
				}},
			},
			Filters: []*logproto.LineFilterExpression{
				{Operator: 1, Match: "foo"},
				{Operator: 1, Match: "bar"},
			},
		}

		// day ranges from ts-48h to ts-24h
		day := getDayTime(ts.Add(-36 * time.Hour))

		expected := &logproto.FilterChunkRefRequest{
			From:    model.TimeFromUnix(day.Unix()),
			Through: model.TimeFromUnix(day.Add(24 * time.Hour).Unix()),
			Refs:    []*logproto.GroupedChunkRefs{},
			Filters: []*logproto.LineFilterExpression{
				{Operator: 1, Match: "foo"},
				{Operator: 1, Match: "bar"},
			},
		}

		output := filterRequestForDay(input, day)
		require.Equal(t, expected, output)
	})
}
