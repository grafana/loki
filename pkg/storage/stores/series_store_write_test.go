package stores

import (
	"context"
	"testing"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/chunkenc"
	"github.com/grafana/loki/pkg/logqlmodel/stats"
	"github.com/grafana/loki/pkg/storage/chunk"
	"github.com/grafana/loki/pkg/storage/chunk/fetcher"
	"github.com/grafana/loki/pkg/storage/config"
)

type mockCache struct {
	called int
	data   map[string]string
}

func (m *mockCache) Store(_ context.Context, _ []string, _ [][]byte) error {
	m.called++
	return nil
}

func (m *mockCache) Fetch(ctx context.Context, keys []string) (found []string, bufs [][]byte, missing []string, err error) {
	for _, key := range keys {
		val, ok := m.data[key]
		if !ok {
			missing = append(missing, key)
			continue
		}
		found = append(found, key)
		bufs = append(bufs, []byte(val))
	}

	return
}

func (m *mockCache) Stop()                         {}
func (m *mockCache) GetCacheType() stats.CacheType { return stats.ChunkCache }

type mockIndexWriter struct {
	called int
}

func (m *mockIndexWriter) IndexChunk(_ context.Context, _, _ model.Time, _ chunk.Chunk) error {
	m.called++
	return nil
}

type mockChunksClient struct {
	called int
}

func (m *mockChunksClient) PutChunks(ctx context.Context, chunks []chunk.Chunk) error {
	m.called++
	return nil
}

func (m *mockChunksClient) Stop() {
}
func (m *mockChunksClient) GetChunks(ctx context.Context, chunks []chunk.Chunk) ([]chunk.Chunk, error) {
	panic("GetChunks not implemented")
}
func (m *mockChunksClient) DeleteChunk(ctx context.Context, userID, chunkID string) error {
	panic("DeleteChunk not implemented")
}
func (m *mockChunksClient) IsChunkNotFoundErr(err error) bool {
	panic("IsChunkNotFoundErr not implemented")
}

func TestChunkWriter_PutOne(t *testing.T) {
	schemaConfig := config.SchemaConfig{
		Configs: []config.PeriodConfig{
			{
				From:   config.DayTime{Time: 0},
				Schema: "v11",
			},
		},
	}

	memchk := chunkenc.NewMemChunk(chunkenc.EncGZIP, chunkenc.UnorderedHeadBlockFmt, 256*1024, 0)
	chk := chunk.NewChunk("fake", model.Fingerprint(0), []labels.Label{{Name: "foo", Value: "bar"}}, chunkenc.NewFacade(memchk, 0, 0), 100, 400)

	for name, tc := range map[string]struct {
		from, through                                                             model.Time
		populateCache                                                             bool
		expectedWriteChunkCalls, expectedIndexWriteCalls, expectedWriteCacheCalls int
	}{
		"found_in_cache": {
			from:                    0,
			through:                 500,
			populateCache:           true,
			expectedWriteChunkCalls: 0,
			expectedIndexWriteCalls: 1,
			expectedWriteCacheCalls: 0,
		},
		"not_found_in_cache": {
			from:                    0,
			through:                 500,
			expectedWriteChunkCalls: 1,
			expectedIndexWriteCalls: 1,
			expectedWriteCacheCalls: 1,
		},
		"overlapping_chunk_right": {
			from:                    0,
			through:                 200,
			expectedWriteChunkCalls: 1,
			expectedIndexWriteCalls: 1,
			expectedWriteCacheCalls: 1,
		},
		"overlapping_chunk_left": {
			from:                    200,
			through:                 500,
			expectedWriteChunkCalls: 1,
			expectedIndexWriteCalls: 1,
			expectedWriteCacheCalls: 1,
		},
		"overlapping_chunk_whole": {
			from:                    200,
			through:                 300,
			expectedWriteChunkCalls: 1,
			expectedIndexWriteCalls: 1,
			expectedWriteCacheCalls: 1,
		},
		"overlapping_chunk_found_in_cache": {
			from:                    200,
			through:                 500,
			populateCache:           true,
			expectedWriteChunkCalls: 1,
			expectedIndexWriteCalls: 1,
			expectedWriteCacheCalls: 0,
		},
	} {
		t.Run(name, func(t *testing.T) {
			cache := &mockCache{}
			if tc.populateCache {
				cacheKey := schemaConfig.ExternalKey(chk.ChunkRef)
				cache = &mockCache{
					data: map[string]string{
						cacheKey: "foo",
					},
				}
			}

			idx := &mockIndexWriter{}
			client := &mockChunksClient{}

			f, err := fetcher.New(cache, false, schemaConfig, client, 1, 1)
			require.NoError(t, err)

			cw := NewChunkWriter(f, schemaConfig, idx, true)

			err = cw.PutOne(context.Background(), tc.from, tc.through, chk)
			require.NoError(t, err)
			require.Equal(t, tc.expectedWriteCacheCalls, cache.called)
			require.Equal(t, tc.expectedIndexWriteCalls, idx.called)
			require.Equal(t, tc.expectedWriteChunkCalls, client.called)
		})
	}
}
