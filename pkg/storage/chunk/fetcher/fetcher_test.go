package fetcher

import (
	"context"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/assert"
	"golang.org/x/exp/slices"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/storage/chunk"
	"github.com/grafana/loki/pkg/storage/chunk/cache"
	"github.com/grafana/loki/pkg/storage/chunk/client/testutils"
	"github.com/grafana/loki/pkg/storage/config"
)

func Test(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name       string
		handoff    time.Duration
		storeStart []chunk.Chunk
		l1Start    []chunk.Chunk
		l2Start    []chunk.Chunk
		fetch      []chunk.Chunk
		l1End      []chunk.Chunk
		l2End      []chunk.Chunk
	}{
		{
			name:       "all found in L1 cache",
			handoff:    0,
			storeStart: []chunk.Chunk{},
			l1Start:    makeChunks(now, c{time.Hour, 2 * time.Hour}, c{2 * time.Hour, 3 * time.Hour}, c{3 * time.Hour, 4 * time.Hour}),
			l2Start:    []chunk.Chunk{},
			fetch:      makeChunks(now, c{time.Hour, 2 * time.Hour}, c{2 * time.Hour, 3 * time.Hour}, c{3 * time.Hour, 4 * time.Hour}),
			l1End:      makeChunks(now, c{time.Hour, 2 * time.Hour}, c{2 * time.Hour, 3 * time.Hour}, c{3 * time.Hour, 4 * time.Hour}),
			l2End:      []chunk.Chunk{},
		},
		{
			name:       "all found in L2 cache",
			handoff:    1, // Only needs to be greater than zero so that we check L2 cache
			storeStart: []chunk.Chunk{},
			l1Start:    []chunk.Chunk{},
			l2Start:    makeChunks(now, c{time.Hour, 2 * time.Hour}, c{2 * time.Hour, 3 * time.Hour}, c{3 * time.Hour, 4 * time.Hour}),
			fetch:      makeChunks(now, c{time.Hour, 2 * time.Hour}, c{2 * time.Hour, 3 * time.Hour}, c{3 * time.Hour, 4 * time.Hour}),
			l1End:      []chunk.Chunk{},
			l2End:      makeChunks(now, c{time.Hour, 2 * time.Hour}, c{2 * time.Hour, 3 * time.Hour}, c{3 * time.Hour, 4 * time.Hour}),
		},
		{
			name:       "some in L1, some in L2",
			handoff:    1, // Only needs to be greater than zero so that we check L2 cache
			storeStart: []chunk.Chunk{},
			l1Start:    []chunk.Chunk{},
			l2Start:    makeChunks(now, c{time.Hour, 2 * time.Hour}, c{2 * time.Hour, 3 * time.Hour}, c{3 * time.Hour, 4 * time.Hour}),
			fetch:      makeChunks(now, c{time.Hour, 2 * time.Hour}, c{2 * time.Hour, 3 * time.Hour}, c{3 * time.Hour, 4 * time.Hour}),
			l1End:      []chunk.Chunk{},
			l2End:      makeChunks(now, c{time.Hour, 2 * time.Hour}, c{2 * time.Hour, 3 * time.Hour}, c{3 * time.Hour, 4 * time.Hour}),
		},
		{
			name:       "writeback l1",
			handoff:    24 * time.Hour,
			storeStart: makeChunks(now, c{time.Hour, 2 * time.Hour}, c{2 * time.Hour, 3 * time.Hour}, c{3 * time.Hour, 4 * time.Hour}),
			l1Start:    []chunk.Chunk{},
			l2Start:    []chunk.Chunk{},
			fetch:      makeChunks(now, c{time.Hour, 2 * time.Hour}, c{2 * time.Hour, 3 * time.Hour}, c{3 * time.Hour, 4 * time.Hour}),
			l1End:      makeChunks(now, c{time.Hour, 2 * time.Hour}, c{2 * time.Hour, 3 * time.Hour}, c{3 * time.Hour, 4 * time.Hour}),
			l2End:      []chunk.Chunk{},
		},
		{
			name:       "writeback l2",
			handoff:    24 * time.Hour,
			storeStart: makeChunks(now, c{31 * time.Hour, 32 * time.Hour}, c{32 * time.Hour, 33 * time.Hour}, c{33 * time.Hour, 34 * time.Hour}),
			l1Start:    []chunk.Chunk{},
			l2Start:    []chunk.Chunk{},
			fetch:      makeChunks(now, c{31 * time.Hour, 32 * time.Hour}, c{32 * time.Hour, 33 * time.Hour}, c{33 * time.Hour, 34 * time.Hour}),
			l1End:      []chunk.Chunk{},
			l2End:      makeChunks(now, c{31 * time.Hour, 32 * time.Hour}, c{32 * time.Hour, 33 * time.Hour}, c{33 * time.Hour, 34 * time.Hour}),
		},
		{
			name:       "writeback l1 and l2",
			handoff:    24 * time.Hour,
			storeStart: makeChunks(now, c{time.Hour, 2 * time.Hour}, c{2 * time.Hour, 3 * time.Hour}, c{3 * time.Hour, 4 * time.Hour}, c{31 * time.Hour, 32 * time.Hour}, c{32 * time.Hour, 33 * time.Hour}, c{33 * time.Hour, 34 * time.Hour}),
			l1Start:    []chunk.Chunk{},
			l2Start:    []chunk.Chunk{},
			fetch:      makeChunks(now, c{time.Hour, 2 * time.Hour}, c{2 * time.Hour, 3 * time.Hour}, c{3 * time.Hour, 4 * time.Hour}, c{31 * time.Hour, 32 * time.Hour}, c{32 * time.Hour, 33 * time.Hour}, c{33 * time.Hour, 34 * time.Hour}),
			l1End:      makeChunks(now, c{time.Hour, 2 * time.Hour}, c{2 * time.Hour, 3 * time.Hour}, c{3 * time.Hour, 4 * time.Hour}),
			l2End:      makeChunks(now, c{31 * time.Hour, 32 * time.Hour}, c{32 * time.Hour, 33 * time.Hour}, c{33 * time.Hour, 34 * time.Hour}),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c1 := cache.NewMockCache()
			c2 := cache.NewMockCache()
			s := testutils.NewMockStorage()
			// Note this is copied from the schema config used in the MockStorage
			sc := config.SchemaConfig{
				Configs: []config.PeriodConfig{
					{
						From:      config.DayTime{Time: 0},
						Schema:    "v11",
						RowShards: 16,
					},
				},
			}

			// Prepare l1 cache
			keys := make([]string, 0, len(test.l1Start))
			chunks := make([][]byte, 0, len(test.l1Start))
			for _, c := range test.l1Start {
				// Encode first to set the checksum
				b, err := c.Encoded()
				assert.NoError(t, err)

				k := sc.ExternalKey(c.ChunkRef)
				keys = append(keys, k)
				chunks = append(chunks, b)
			}
			assert.NoError(t, c1.Store(context.Background(), keys, chunks))

			// Prepare l2 cache
			keys = make([]string, 0, len(test.l2Start))
			chunks = make([][]byte, 0, len(test.l2Start))
			for _, c := range test.l2Start {
				b, err := c.Encoded()
				assert.NoError(t, err)

				k := sc.ExternalKey(c.ChunkRef)
				keys = append(keys, k)
				chunks = append(chunks, b)
			}
			assert.NoError(t, c2.Store(context.Background(), keys, chunks))

			// Prepare store
			assert.NoError(t, s.PutChunks(context.Background(), test.storeStart))

			// Build fetcher
			f, err := New(c1, c2, true, sc, s, 1, 1, test.handoff)
			assert.NoError(t, err)

			// Generate keys from chunks
			keys = make([]string, 0, len(test.fetch))
			for _, f := range test.fetch {
				k := sc.ExternalKey(f.ChunkRef)
				keys = append(keys, k)
			}

			// Run the test
			chks, err := f.FetchChunks(context.Background(), test.fetch, keys)
			assert.NoError(t, err)
			assertChunks(t, test.fetch, chks)
			l1actual, err := makeChunksFromMap(c1.GetInternal())
			assert.NoError(t, err)
			assertChunks(t, test.l1End, l1actual)
			l2actual, err := makeChunksFromMap(c2.GetInternal())
			assert.NoError(t, err)
			assertChunks(t, test.l2End, l2actual)
		})
	}
}

type c struct {
	from, through time.Duration
}

func makeChunks(now time.Time, tpls ...c) []chunk.Chunk {
	var chks []chunk.Chunk
	for _, chk := range tpls {
		c := chunk.Chunk{
			ChunkRef: logproto.ChunkRef{
				UserID:  "fake",
				From:    model.TimeFromUnix(now.Add(-chk.from).UTC().Unix()),
				Through: model.TimeFromUnix(now.Add(-chk.through).UTC().Unix()),
			},
		}
		c.Metric = labels.Labels{}
		// This isn't even the write format for Loki but we dont' care for the sake of these tests
		c.Data = chunk.New()
		// Encode to set the checksum
		_ = c.Encode()
		chks = append(chks, c)
	}

	return chks
}

func makeChunksFromMap(m map[string][]byte) ([]chunk.Chunk, error) {
	chks := make([]chunk.Chunk, 0, len(m))
	for k := range m {
		c, err := chunk.ParseExternalKey("fake", k)
		if err != nil {
			return nil, err
		}
		chks = append(chks, c)
	}

	return chks, nil
}

func sortChunks(chks []chunk.Chunk) {
	slices.SortFunc[chunk.Chunk](chks, func(i, j chunk.Chunk) bool {
		return i.From.Before(j.From)
	})
}

func assertChunks(t *testing.T, expected, actual []chunk.Chunk) {
	sortChunks(expected)
	sortChunks(actual)
	assert.Eventually(t, func() bool {
		return len(expected) == len(actual)
	}, 2*time.Second, time.Millisecond*100, "expected %d chunks, got %d", len(expected), len(actual))
	for i := range expected {
		assert.Equal(t, expected[i].ChunkRef, actual[i].ChunkRef)
	}
}
