package fetcher

import (
	"context"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/assert"
	"golang.org/x/exp/slices"

	"github.com/grafana/loki/v3/pkg/chunkenc"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/storage/chunk"
	"github.com/grafana/loki/v3/pkg/storage/chunk/cache"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/testutils"
	"github.com/grafana/loki/v3/pkg/storage/config"
)

func Test(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name            string
		handoff         time.Duration
		storeStart      []chunk.Chunk
		l1Start         []chunk.Chunk
		l2Start         []chunk.Chunk
		fetch           []chunk.Chunk
		l1KeysRequested int
		l1End           []chunk.Chunk
		l2KeysRequested int
		l2End           []chunk.Chunk
	}{
		{
			name:            "all found in L1 cache",
			handoff:         0,
			storeStart:      []chunk.Chunk{},
			l1Start:         makeChunks(now, c{time.Hour, 2 * time.Hour}, c{2 * time.Hour, 3 * time.Hour}, c{3 * time.Hour, 4 * time.Hour}),
			l2Start:         []chunk.Chunk{},
			fetch:           makeChunks(now, c{time.Hour, 2 * time.Hour}, c{2 * time.Hour, 3 * time.Hour}, c{3 * time.Hour, 4 * time.Hour}),
			l1KeysRequested: 3,
			l1End:           makeChunks(now, c{time.Hour, 2 * time.Hour}, c{2 * time.Hour, 3 * time.Hour}, c{3 * time.Hour, 4 * time.Hour}),
			l2End:           []chunk.Chunk{},
		},
		{
			name:            "all found in L2 cache",
			handoff:         1, // Only needs to be greater than zero so that we check L2 cache
			storeStart:      []chunk.Chunk{},
			l1Start:         []chunk.Chunk{},
			l2Start:         makeChunks(now, c{time.Hour, 2 * time.Hour}, c{2 * time.Hour, 3 * time.Hour}, c{3 * time.Hour, 4 * time.Hour}),
			fetch:           makeChunks(now, c{time.Hour, 2 * time.Hour}, c{2 * time.Hour, 3 * time.Hour}, c{3 * time.Hour, 4 * time.Hour}),
			l1End:           []chunk.Chunk{},
			l2KeysRequested: 3,
			l2End:           makeChunks(now, c{time.Hour, 2 * time.Hour}, c{2 * time.Hour, 3 * time.Hour}, c{3 * time.Hour, 4 * time.Hour}),
		},
		{
			name:            "some in L1, some in L2",
			handoff:         5 * time.Hour,
			storeStart:      []chunk.Chunk{},
			l1Start:         makeChunks(now, c{time.Hour, 2 * time.Hour}, c{2 * time.Hour, 3 * time.Hour}, c{3 * time.Hour, 4 * time.Hour}),
			l2Start:         makeChunks(now, c{7 * time.Hour, 8 * time.Hour}, c{8 * time.Hour, 9 * time.Hour}, c{9 * time.Hour, 10 * time.Hour}),
			fetch:           makeChunks(now, c{time.Hour, 2 * time.Hour}, c{2 * time.Hour, 3 * time.Hour}, c{3 * time.Hour, 4 * time.Hour}, c{7 * time.Hour, 8 * time.Hour}, c{8 * time.Hour, 9 * time.Hour}, c{9 * time.Hour, 10 * time.Hour}),
			l1KeysRequested: 3,
			l1End:           makeChunks(now, c{time.Hour, 2 * time.Hour}, c{2 * time.Hour, 3 * time.Hour}, c{3 * time.Hour, 4 * time.Hour}),
			l2KeysRequested: 3,
			l2End:           makeChunks(now, c{7 * time.Hour, 8 * time.Hour}, c{8 * time.Hour, 9 * time.Hour}, c{9 * time.Hour, 10 * time.Hour}),
		},
		{
			name:            "some in L1, some in L2, some in store",
			handoff:         5 * time.Hour,
			storeStart:      makeChunks(now, c{2 * time.Hour, 3 * time.Hour}, c{3 * time.Hour, 4 * time.Hour}, c{8 * time.Hour, 9 * time.Hour}, c{9 * time.Hour, 10 * time.Hour}),
			l1Start:         makeChunks(now, c{time.Hour, 2 * time.Hour}),
			l2Start:         makeChunks(now, c{7 * time.Hour, 8 * time.Hour}),
			fetch:           makeChunks(now, c{time.Hour, 2 * time.Hour}, c{2 * time.Hour, 3 * time.Hour}, c{3 * time.Hour, 4 * time.Hour}, c{7 * time.Hour, 8 * time.Hour}, c{8 * time.Hour, 9 * time.Hour}, c{9 * time.Hour, 10 * time.Hour}),
			l1KeysRequested: 3,
			l1End:           makeChunks(now, c{time.Hour, 2 * time.Hour}, c{2 * time.Hour, 3 * time.Hour}, c{3 * time.Hour, 4 * time.Hour}),
			l2KeysRequested: 3,
			l2End:           makeChunks(now, c{7 * time.Hour, 8 * time.Hour}, c{8 * time.Hour, 9 * time.Hour}, c{9 * time.Hour, 10 * time.Hour}),
		},
		{
			name:            "writeback l1",
			handoff:         24 * time.Hour,
			storeStart:      makeChunks(now, c{time.Hour, 2 * time.Hour}, c{2 * time.Hour, 3 * time.Hour}, c{3 * time.Hour, 4 * time.Hour}),
			l1Start:         []chunk.Chunk{},
			l2Start:         []chunk.Chunk{},
			fetch:           makeChunks(now, c{time.Hour, 2 * time.Hour}, c{2 * time.Hour, 3 * time.Hour}, c{3 * time.Hour, 4 * time.Hour}),
			l1KeysRequested: 3,
			l1End:           makeChunks(now, c{time.Hour, 2 * time.Hour}, c{2 * time.Hour, 3 * time.Hour}, c{3 * time.Hour, 4 * time.Hour}),
			l2End:           []chunk.Chunk{},
		},
		{
			name:            "writeback l2",
			handoff:         24 * time.Hour,
			storeStart:      makeChunks(now, c{31 * time.Hour, 32 * time.Hour}, c{32 * time.Hour, 33 * time.Hour}, c{33 * time.Hour, 34 * time.Hour}),
			l1Start:         []chunk.Chunk{},
			l2Start:         []chunk.Chunk{},
			fetch:           makeChunks(now, c{31 * time.Hour, 32 * time.Hour}, c{32 * time.Hour, 33 * time.Hour}, c{33 * time.Hour, 34 * time.Hour}),
			l1End:           []chunk.Chunk{},
			l2KeysRequested: 3,
			l2End:           makeChunks(now, c{31 * time.Hour, 32 * time.Hour}, c{32 * time.Hour, 33 * time.Hour}, c{33 * time.Hour, 34 * time.Hour}),
		},
		{
			name:            "writeback l1 and l2",
			handoff:         24 * time.Hour,
			storeStart:      makeChunks(now, c{time.Hour, 2 * time.Hour}, c{2 * time.Hour, 3 * time.Hour}, c{3 * time.Hour, 4 * time.Hour}, c{31 * time.Hour, 32 * time.Hour}, c{32 * time.Hour, 33 * time.Hour}, c{33 * time.Hour, 34 * time.Hour}),
			l1Start:         []chunk.Chunk{},
			l2Start:         []chunk.Chunk{},
			fetch:           makeChunks(now, c{time.Hour, 2 * time.Hour}, c{2 * time.Hour, 3 * time.Hour}, c{3 * time.Hour, 4 * time.Hour}, c{31 * time.Hour, 32 * time.Hour}, c{32 * time.Hour, 33 * time.Hour}, c{33 * time.Hour, 34 * time.Hour}),
			l1KeysRequested: 3,
			l1End:           makeChunks(now, c{time.Hour, 2 * time.Hour}, c{2 * time.Hour, 3 * time.Hour}, c{3 * time.Hour, 4 * time.Hour}),
			l2KeysRequested: 3,
			l2End:           makeChunks(now, c{31 * time.Hour, 32 * time.Hour}, c{32 * time.Hour, 33 * time.Hour}, c{33 * time.Hour, 34 * time.Hour}),
		},
		{
			name:            "verify l1 skip optimization",
			handoff:         24 * time.Hour,
			storeStart:      makeChunks(now, c{31 * time.Hour, 32 * time.Hour}, c{32 * time.Hour, 33 * time.Hour}, c{33 * time.Hour, 34 * time.Hour}),
			l1Start:         []chunk.Chunk{},
			l2Start:         []chunk.Chunk{},
			fetch:           makeChunks(now, c{31 * time.Hour, 32 * time.Hour}, c{32 * time.Hour, 33 * time.Hour}, c{33 * time.Hour, 34 * time.Hour}),
			l1KeysRequested: 0,
			l1End:           []chunk.Chunk{},
			l2KeysRequested: 3,
			l2End:           makeChunks(now, c{31 * time.Hour, 32 * time.Hour}, c{32 * time.Hour, 33 * time.Hour}, c{33 * time.Hour, 34 * time.Hour}),
		},
		{
			name:            "verify l1 skip optimization plus extended",
			handoff:         20 * time.Hour, // 20 hours, 10% extension should be 22 hours
			storeStart:      makeChunks(now, c{31 * time.Hour, 32 * time.Hour}, c{32 * time.Hour, 33 * time.Hour}, c{33 * time.Hour, 34 * time.Hour}),
			l1Start:         makeChunks(now, c{20 * time.Hour, 21 * time.Hour}, c{21 * time.Hour, 22 * time.Hour}, c{22 * time.Hour, 23 * time.Hour}),
			l2Start:         makeChunks(now, c{21 * time.Hour, 22 * time.Hour}, c{22 * time.Hour, 23 * time.Hour}),
			fetch:           makeChunks(now, c{20 * time.Hour, 21 * time.Hour}, c{21 * time.Hour, 22 * time.Hour}, c{22 * time.Hour, 23 * time.Hour}),
			l1KeysRequested: 2,
			l1End:           makeChunks(now, c{20 * time.Hour, 21 * time.Hour}, c{21 * time.Hour, 22 * time.Hour}, c{22 * time.Hour, 23 * time.Hour}),
			l2KeysRequested: 1, // We won't look for the extended handoff key in L2, so only one lookup should go to L2
			l2End:           makeChunks(now, c{21 * time.Hour, 22 * time.Hour}, c{22 * time.Hour, 23 * time.Hour}),
		},
		{
			name:            "verify l2 skip optimization",
			handoff:         24 * time.Hour,
			storeStart:      makeChunks(now, c{31 * time.Hour, 32 * time.Hour}, c{32 * time.Hour, 33 * time.Hour}, c{33 * time.Hour, 34 * time.Hour}),
			l1Start:         makeChunks(now, c{time.Hour, 2 * time.Hour}, c{2 * time.Hour, 3 * time.Hour}, c{3 * time.Hour, 4 * time.Hour}),
			l2Start:         []chunk.Chunk{},
			fetch:           makeChunks(now, c{time.Hour, 2 * time.Hour}, c{2 * time.Hour, 3 * time.Hour}, c{3 * time.Hour, 4 * time.Hour}),
			l1KeysRequested: 3,
			l1End:           makeChunks(now, c{time.Hour, 2 * time.Hour}, c{2 * time.Hour, 3 * time.Hour}, c{3 * time.Hour, 4 * time.Hour}),
			l2KeysRequested: 0,
			l2End:           []chunk.Chunk{},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c1 := cache.NewMockCache()
			c2 := cache.NewMockCache()
			s := testutils.NewMockStorage()
			sc := config.SchemaConfig{
				Configs: s.GetSchemaConfigs(),
			}
			chunkClient := client.NewClientWithMaxParallel(s, nil, 1, sc)

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
			assert.NoError(t, chunkClient.PutChunks(context.Background(), test.storeStart))

			// Build fetcher
			f, err := New(c1, c2, false, sc, chunkClient, test.handoff)
			assert.NoError(t, err)

			// Run the test
			chks, err := f.FetchChunks(context.Background(), test.fetch)
			assert.NoError(t, err)
			assertChunks(t, test.fetch, chks)
			l1actual, err := makeChunksFromMapKeys(c1.GetKeys())
			assert.NoError(t, err)
			assert.Equal(t, test.l1KeysRequested, c1.KeysRequested())
			assertChunks(t, test.l1End, l1actual)
			l2actual, err := makeChunksFromMapKeys(c2.GetKeys())
			assert.NoError(t, err)
			assert.Equal(t, test.l2KeysRequested, c2.KeysRequested())
			assertChunks(t, test.l2End, l2actual)
		})
	}
}

func BenchmarkFetch(b *testing.B) {
	now := time.Now()

	numchunks := 100
	l1Start := make([]chunk.Chunk, 0, numchunks/3)
	for i := 0; i < numchunks/3; i++ {
		l1Start = append(l1Start, makeChunks(now, c{time.Duration(i) * time.Hour, time.Duration(i+1) * time.Hour})...)
	}
	l2Start := make([]chunk.Chunk, 0, numchunks/3)
	for i := numchunks/3 + 1000; i < (numchunks/3)+numchunks/3+1000; i++ {
		l2Start = append(l2Start, makeChunks(now, c{time.Duration(i) * time.Hour, time.Duration(i+1) * time.Hour})...)
	}
	storeStart := make([]chunk.Chunk, 0, numchunks/3)
	for i := numchunks/3 + 10000; i < (numchunks/3)+numchunks/3+10000; i++ {
		storeStart = append(storeStart, makeChunks(now, c{time.Duration(i) * time.Hour, time.Duration(i+1) * time.Hour})...)
	}
	fetch := make([]chunk.Chunk, 0, numchunks)
	fetch = append(fetch, l1Start...)
	fetch = append(fetch, l2Start...)
	fetch = append(fetch, storeStart...)

	test := struct {
		name            string
		handoff         time.Duration
		storeStart      []chunk.Chunk
		l1Start         []chunk.Chunk
		l2Start         []chunk.Chunk
		fetch           []chunk.Chunk
		l1KeysRequested int
		l1End           []chunk.Chunk
		l2KeysRequested int
		l2End           []chunk.Chunk
	}{
		name:       "some in L1, some in L2",
		handoff:    time.Duration(numchunks/3+100) * time.Hour,
		storeStart: storeStart,
		l1Start:    l1Start,
		l2Start:    l2Start,
		fetch:      fetch,
	}

	c1 := cache.NewMockCache()
	c2 := cache.NewMockCache()
	s := testutils.NewMockStorage()
	sc := config.SchemaConfig{
		Configs: s.GetSchemaConfigs(),
	}
	chunkClient := client.NewClientWithMaxParallel(s, nil, 1, sc)

	// Prepare l1 cache
	keys := make([]string, 0, len(test.l1Start))
	chunks := make([][]byte, 0, len(test.l1Start))
	for _, c := range test.l1Start {
		// Encode first to set the checksum
		b, _ := c.Encoded()

		k := sc.ExternalKey(c.ChunkRef)
		keys = append(keys, k)
		chunks = append(chunks, b)
	}
	_ = c1.Store(context.Background(), keys, chunks)

	// Prepare l2 cache
	keys = make([]string, 0, len(test.l2Start))
	chunks = make([][]byte, 0, len(test.l2Start))
	for _, c := range test.l2Start {
		b, _ := c.Encoded()

		k := sc.ExternalKey(c.ChunkRef)
		keys = append(keys, k)
		chunks = append(chunks, b)
	}
	_ = c2.Store(context.Background(), keys, chunks)

	// Prepare store
	_ = chunkClient.PutChunks(context.Background(), test.storeStart)

	// Build fetcher
	f, _ := New(c1, c2, false, sc, chunkClient, test.handoff)

	for i := 0; i < b.N; i++ {
		_, err := f.FetchChunks(context.Background(), test.fetch)
		if err != nil {
			b.Fatal(err)
		}
	}
	b.ReportAllocs()
}

type c struct {
	from, through time.Duration
}

func makeChunks(now time.Time, tpls ...c) []chunk.Chunk {
	var chks []chunk.Chunk
	for _, chk := range tpls {
		from := int(chk.from) / int(time.Hour)
		// This is only here because it's helpful for debugging.
		// This isn't even the write format for Loki but we dont' care for the sake of these tests.
		memChk := chunkenc.NewMemChunk(chunkenc.ChunkFormatV4, chunkenc.EncNone, chunkenc.UnorderedWithStructuredMetadataHeadBlockFmt, 256*1024, 0)
		// To make sure the fetcher doesn't swap keys and buffers each chunk is built with different, but deterministic data
		for i := 0; i < from; i++ {
			_, _ = memChk.Append(&logproto.Entry{
				Timestamp: time.Unix(int64(i), 0),
				Line:      fmt.Sprintf("line ts=%d", i),
			})
		}
		data := chunkenc.NewFacade(memChk, 0, 0)
		c := chunk.Chunk{
			ChunkRef: logproto.ChunkRef{
				UserID:  "fake",
				From:    model.TimeFromUnix(now.Add(-chk.from).UTC().Unix()),
				Through: model.TimeFromUnix(now.Add(-chk.through).UTC().Unix()),
			},
			Metric:   labels.Labels{labels.Label{Name: "start", Value: strconv.Itoa(from)}},
			Data:     data,
			Encoding: data.Encoding(),
		}
		// Encode to set the checksum
		if err := c.Encode(); err != nil {
			panic(err)
		}
		chks = append(chks, c)
	}

	return chks
}

func makeChunksFromMapKeys(keys []string) ([]chunk.Chunk, error) {
	chks := make([]chunk.Chunk, 0, len(keys))
	for _, k := range keys {
		c, err := chunk.ParseExternalKey("fake", k)
		if err != nil {
			return nil, err
		}
		chks = append(chks, c)
	}

	return chks, nil
}

func sortChunks(chks []chunk.Chunk) {
	slices.SortFunc(chks, func(i, j chunk.Chunk) int {
		if i.From.Before(j.From) {
			return -1
		}
		return 1
	})
}

func assertChunks(t *testing.T, expected, actual []chunk.Chunk) {
	assert.Eventually(t, func() bool {
		return len(expected) == len(actual)
	}, 2*time.Second, time.Millisecond*100, "expected %d chunks, got %d", len(expected), len(actual))
	sortChunks(expected)
	sortChunks(actual)
	for i := range expected {
		assert.Equal(t, expected[i].ChunkRef, actual[i].ChunkRef)
	}
}
