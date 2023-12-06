package bloomgateway

import (
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/logproto"
)

func TestTask(t *testing.T) {
	t.Run("bounds returns request boundaries", func(t *testing.T) {
		ts := model.Now()
		req := &logproto.FilterChunkRefRequest{
			From:    ts.Add(-1 * time.Hour),
			Through: ts,
		}
		task, _, _, err := NewTask("tenant", req)
		require.NoError(t, err)
		from, through := task.Bounds()
		require.Equal(t, getDayTime(req.From), from)
		require.Equal(t, getDayTime(req.Through), through)
	})
}

func TestTaskMergeIterator(t *testing.T) {
	// Thu Nov 09 2023 10:56:50 UTC
	ts := model.TimeFromUnix(1699523810)
	day := getDayTime(ts)
	tenant := "fake"

	t.Run("empty requests result in empty iterator", func(t *testing.T) {
		r1 := &logproto.FilterChunkRefRequest{
			From:    ts.Add(-3 * time.Hour),
			Through: ts.Add(-2 * time.Hour),
			Refs:    []*logproto.GroupedChunkRefs{},
		}
		t1, _, _, err := NewTask(tenant, r1)
		require.NoError(t, err)

		r2 := &logproto.FilterChunkRefRequest{
			From:    ts.Add(-1 * time.Hour),
			Through: ts,
			Refs:    []*logproto.GroupedChunkRefs{},
		}
		t2, _, _, err := NewTask(tenant, r2)
		require.NoError(t, err)

		r3 := &logproto.FilterChunkRefRequest{
			From:    ts.Add(-1 * time.Hour),
			Through: ts,
			Refs:    []*logproto.GroupedChunkRefs{},
		}
		t3, _, _, err := NewTask(tenant, r3)
		require.NoError(t, err)

		it := newTaskMergeIterator(day, t1, t2, t3)
		// nothing to iterate over
		require.False(t, it.Next())
	})

	t.Run("merge multiple tasks in ascending fingerprint order", func(t *testing.T) {
		r1 := &logproto.FilterChunkRefRequest{
			From:    ts.Add(-3 * time.Hour),
			Through: ts.Add(-2 * time.Hour),
			Refs: []*logproto.GroupedChunkRefs{
				{Fingerprint: 100, Tenant: tenant, Refs: []*logproto.ShortRef{
					{From: ts.Add(-3 * time.Hour), Through: ts.Add(-2 * time.Hour), Checksum: 100},
				}},
			},
		}
		t1, _, _, err := NewTask(tenant, r1)
		require.NoError(t, err)

		r2 := &logproto.FilterChunkRefRequest{
			From:    ts.Add(-1 * time.Hour),
			Through: ts,
			Refs: []*logproto.GroupedChunkRefs{
				{Fingerprint: 100, Tenant: tenant, Refs: []*logproto.ShortRef{
					{From: ts.Add(-1 * time.Hour), Through: ts, Checksum: 200},
				}},
				{Fingerprint: 200, Tenant: tenant, Refs: []*logproto.ShortRef{
					{From: ts.Add(-1 * time.Hour), Through: ts, Checksum: 300},
				}},
			},
		}
		t2, _, _, err := NewTask(tenant, r2)
		require.NoError(t, err)

		r3 := &logproto.FilterChunkRefRequest{
			From:    ts.Add(-1 * time.Hour),
			Through: ts,
			Refs: []*logproto.GroupedChunkRefs{
				{Fingerprint: 200, Tenant: tenant, Refs: []*logproto.ShortRef{
					{From: ts.Add(-1 * time.Hour), Through: ts, Checksum: 400},
				}},
			},
		}
		t3, _, _, err := NewTask(tenant, r3)
		require.NoError(t, err)

		it := newTaskMergeIterator(day, t1, t2, t3)

		// first item
		require.True(t, it.Next())
		r := it.At()
		require.Equal(t, model.Fingerprint(100), r.Fp)
		require.Equal(t, uint32(100), r.Chks[0].Checksum)

		// second item
		require.True(t, it.Next())
		r = it.At()
		require.Equal(t, model.Fingerprint(100), r.Fp)
		require.Equal(t, uint32(200), r.Chks[0].Checksum)

		// third item
		require.True(t, it.Next())
		r = it.At()
		require.Equal(t, model.Fingerprint(200), r.Fp)
		require.Equal(t, uint32(300), r.Chks[0].Checksum)

		// fourth item
		require.True(t, it.Next())
		r = it.At()
		require.Equal(t, model.Fingerprint(200), r.Fp)
		require.Equal(t, uint32(400), r.Chks[0].Checksum)

		// no more items
		require.False(t, it.Next())
	})
}

func TestChunkIterForDay(t *testing.T) {
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

		expected := []*logproto.GroupedChunkRefs{
			{Fingerprint: 500, Tenant: tenant, Refs: []*logproto.ShortRef{
				{From: ts.Add(-47 * time.Hour), Through: ts.Add(-46 * time.Hour), Checksum: 501},
			}},
			{Fingerprint: 600, Tenant: tenant, Refs: []*logproto.ShortRef{
				{From: ts.Add(-48 * time.Hour), Through: ts.Add(-47 * time.Hour), Checksum: 600},
			}},
		}

		task, _, _, _ := NewTask(tenant, input)
		it := task.ChunkIterForDay(day)

		output := make([]*logproto.GroupedChunkRefs, 0, len(input.Refs))
		for it.Next() {
			output = append(output, it.At())
		}

		require.Equal(t, expected, output)
	})
}
