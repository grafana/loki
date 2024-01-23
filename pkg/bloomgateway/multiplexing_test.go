package bloomgateway

import (
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql/syntax"
	v1 "github.com/grafana/loki/pkg/storage/bloom/v1"
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
	tokenizer := v1.NewNGramTokenizer(4, 0)

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

		it := newTaskMergeIterator(day, tokenizer, t1, t2, t3)
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

		it := newTaskMergeIterator(day, tokenizer, t1, t2, t3)

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

		// fifth item
		require.True(t, it.Next())
		r = it.At()
		require.Equal(t, model.Fingerprint(300), r.Fp)
		require.Equal(t, uint32(500), r.Chks[0].Checksum)

		// no more items
		require.False(t, it.Next())
	})
}

func fpWithChunks(tenant string, fp uint64, fromTs model.Time) *logproto.GroupedChunkRefs {
	chks := make([]*logproto.ShortRef, 0, 24)
	for i := fromTs; i < fromTs.Add(24*time.Hour); i = i.Add(time.Hour) {
		chks = append(chks, &logproto.ShortRef{From: i, Through: i.Add(time.Hour)})
	}
	return &logproto.GroupedChunkRefs{Fingerprint: fp, Tenant: tenant, Refs: chks}
}

func TestChunkIterForDay(t *testing.T) {
	tenant := "fake"

	// Nov 09 2023 10:00 UTC
	ts := mktime("2023-11-09 10:00")

	t.Run("filter chunk refs that fall into the day range", func(t *testing.T) {
		input := &logproto.FilterChunkRefRequest{
			From:    ts.Add(-7 * 24 * time.Hour),
			Through: ts,
			Refs: []*logproto.GroupedChunkRefs{
				fpWithChunks(tenant, 0x0000, ts.Add(-7*24*time.Hour)),
				fpWithChunks(tenant, 0x0400, ts.Add(-6*24*time.Hour)),
				fpWithChunks(tenant, 0x0800, ts.Add(-5*24*time.Hour)),
				fpWithChunks(tenant, 0x0C00, ts.Add(-4*24*time.Hour)),
				fpWithChunks(tenant, 0x1000, ts.Add(-3*24*time.Hour)), // matches partially, t-58h .. t-49h
				fpWithChunks(tenant, 0x1400, ts.Add(-2*24*time.Hour)), // matches partially, t-49h .. t-34h
				fpWithChunks(tenant, 0x1800, ts.Add(-1*24*time.Hour)),
			},
			Filters: []syntax.LineFilter{
				{Ty: labels.MatchEqual, Match: "foo"},
				{Ty: labels.MatchEqual, Match: "bar"},
			},
		}

		// Nov 07 2023 22:00 UTC
		newTs := ts.Add(-36 * time.Hour)
		// Nov 07 2023 00:00 UTC
		// from ts-58h to ts-34h
		day := getDayTime(newTs)

		task, _, _, _ := NewTask(tenant, input)
		it := task.ChunkIterForDay(day)

		output := make([]*logproto.GroupedChunkRefs, 0, len(input.Refs))
		for it.Next() {
			output = append(output, it.At())
		}

		require.Len(t, output, 2)
		require.Equal(t, output[0].Fingerprint, input.Refs[4].Fingerprint)
		require.Len(t, output[0].Refs, 11)
		require.Equal(t, output[1].Fingerprint, input.Refs[5].Fingerprint)
		require.Len(t, output[1].Refs, 15)
	})
}
