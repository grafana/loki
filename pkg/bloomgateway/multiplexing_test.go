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
		require.NotNil(t, it.heap)
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
		require.NotNil(t, it.heap)

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

	t.Run("reset of iterator allows for multiple iterations", func(t *testing.T) {
		r1 := &logproto.FilterChunkRefRequest{
			From:    ts.Add(-1 * time.Hour),
			Through: ts,
			Refs: []*logproto.GroupedChunkRefs{
				{Fingerprint: 100, Tenant: tenant, Refs: []*logproto.ShortRef{
					{From: ts.Add(-1 * time.Hour), Through: ts, Checksum: 100},
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
			},
		}
		t2, _, _, err := NewTask(tenant, r2)
		require.NoError(t, err)

		r3 := &logproto.FilterChunkRefRequest{
			From:    ts.Add(-1 * time.Hour),
			Through: ts,
			Refs: []*logproto.GroupedChunkRefs{
				{Fingerprint: 200, Tenant: tenant, Refs: []*logproto.ShortRef{
					{From: ts.Add(-1 * time.Hour), Through: ts, Checksum: 300},
				}},
			},
		}
		t3, _, _, err := NewTask(tenant, r3)
		require.NoError(t, err)

		it := newTaskMergeIterator(day, t1, t2, t3)
		require.NotNil(t, it.heap)

		checksums := []uint32{100, 200, 300}
		for i := 0; i < 3; i++ {
			count := 0
			for it.Next() {
				require.Equal(t, checksums[count], it.At().Chks[0].Checksum)
				count++
			}
			it.Reset()
		}
	})
}
