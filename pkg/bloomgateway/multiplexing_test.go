package bloomgateway

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	v1 "github.com/grafana/loki/v3/pkg/storage/bloom/v1"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/bloomshipper"
)

func TestTask(t *testing.T) {
	ts := mktime("2024-01-24 12:00")
	t.Run("bounds returns boundaries of chunks", func(t *testing.T) {
		req := &logproto.FilterChunkRefRequest{
			From:    ts.Add(-24 * time.Hour),
			Through: ts,
			Refs: []*logproto.GroupedChunkRefs{
				{
					Fingerprint: 0x00,
					Refs: []*logproto.ShortRef{
						{From: ts.Add(-1 * time.Hour), Through: ts},
					},
				},
			},
		}
		swb := partitionRequest(req)[0]
		task, err := NewTask(context.Background(), "tenant", swb, nil, nil)
		require.NoError(t, err)
		from, through := task.Bounds()
		require.Equal(t, ts.Add(-1*time.Hour), from)
		require.Equal(t, ts, through)
	})
}

func createTasksForRequests(t *testing.T, tenant string, requests ...*logproto.FilterChunkRefRequest) []Task {
	t.Helper()

	tasks := make([]Task, 0, len(requests))
	for _, r := range requests {
		for _, swb := range partitionRequest(r) {
			task, err := NewTask(context.Background(), tenant, swb, nil, nil)
			require.NoError(t, err)
			tasks = append(tasks, task)
		}
	}
	return tasks
}

func TestTask_RequestIterator(t *testing.T) {
	ts := mktime("2024-01-24 12:00")
	tenant := "fake"
	tokenizer := v1.NewNGramTokenizer(4, 0)

	t.Run("empty request yields empty iterator", func(t *testing.T) {
		swb := seriesWithInterval{
			interval: bloomshipper.Interval{Start: 0, End: math.MaxInt64},
			series:   []*logproto.GroupedChunkRefs{},
		}
		task, _ := NewTask(context.Background(), tenant, swb, []syntax.LineFilterExpr{}, nil)
		it := task.RequestIter(tokenizer)
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

		r3 := &logproto.FilterChunkRefRequest{
			From:    ts.Add(-1 * time.Hour),
			Through: ts,
			Refs: []*logproto.GroupedChunkRefs{
				{Fingerprint: 200, Tenant: tenant, Refs: []*logproto.ShortRef{
					{From: ts.Add(-1 * time.Hour), Through: ts, Checksum: 400},
				}},
			},
		}

		tasks := createTasksForRequests(t, tenant, r1, r2, r3)

		iters := make([]v1.PeekingIterator[v1.Request], 0, len(tasks))
		for _, task := range tasks {
			iters = append(iters, v1.NewPeekingIter(task.RequestIter(tokenizer)))
		}

		// merge the request iterators using the heap sort iterator
		it := v1.NewHeapIterator[v1.Request](func(r1, r2 v1.Request) bool { return r1.Fp < r2.Fp }, iters...)

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
