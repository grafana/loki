package bloomgateway

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	v2 "github.com/grafana/loki/v3/pkg/iter/v2"
	"github.com/grafana/loki/v3/pkg/logproto"
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
		task := newTask(context.Background(), "tenant", swb, nil, nil)
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
			task := newTask(context.Background(), tenant, swb, nil, nil)
			tasks = append(tasks, task)
		}
	}
	return tasks
}

func TestTask_RequestIterator(t *testing.T) {
	ts := mktime("2024-01-24 12:00")
	tenant := "fake"

	t.Run("empty request yields empty iterator", func(t *testing.T) {
		swb := seriesWithInterval{
			interval: bloomshipper.Interval{Start: 0, End: math.MaxInt64},
			series:   []*logproto.GroupedChunkRefs{},
		}
		task := newTask(context.Background(), tenant, swb, nil, nil)
		it := task.RequestIter()
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
				}, Labels: &logproto.IndexSeries{
					Labels: logproto.FromLabelsToLabelAdapters(labels.FromStrings("foo", "100")),
				}},
			},
		}

		r2 := &logproto.FilterChunkRefRequest{
			From:    ts.Add(-1 * time.Hour),
			Through: ts,
			Refs: []*logproto.GroupedChunkRefs{
				{Fingerprint: 100, Tenant: tenant, Refs: []*logproto.ShortRef{
					{From: ts.Add(-1 * time.Hour), Through: ts, Checksum: 200},
				}, Labels: &logproto.IndexSeries{
					Labels: logproto.FromLabelsToLabelAdapters(labels.FromStrings("foo", "100")),
				}},
				{Fingerprint: 200, Tenant: tenant, Refs: []*logproto.ShortRef{
					{From: ts.Add(-1 * time.Hour), Through: ts, Checksum: 300},
				}, Labels: &logproto.IndexSeries{
					Labels: logproto.FromLabelsToLabelAdapters(labels.FromStrings("foo", "200")),
				}},
			},
		}

		r3 := &logproto.FilterChunkRefRequest{
			From:    ts.Add(-1 * time.Hour),
			Through: ts,
			Refs: []*logproto.GroupedChunkRefs{
				{Fingerprint: 200, Tenant: tenant, Refs: []*logproto.ShortRef{
					{From: ts.Add(-1 * time.Hour), Through: ts, Checksum: 400},
				}, Labels: &logproto.IndexSeries{
					Labels: logproto.FromLabelsToLabelAdapters(labels.FromStrings("foo", "200")),
				}},
			},
		}

		tasks := createTasksForRequests(t, tenant, r1, r2, r3)

		iters := make([]v2.PeekIterator[v1.Request], 0, len(tasks))
		for _, task := range tasks {
			iters = append(iters, v2.NewPeekIter(task.RequestIter()))
		}

		// merge the request iterators using the heap sort iterator
		it := v1.NewHeapIterator(func(r1, r2 v1.Request) bool { return r1.Fp < r2.Fp }, iters...)

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
