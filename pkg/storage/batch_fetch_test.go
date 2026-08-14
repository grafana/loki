package storage

import (
	"context"
	"errors"
	"testing"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/storage/chunk"
	"github.com/grafana/loki/v3/pkg/storage/chunk/cache"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/testutils"
	"github.com/grafana/loki/v3/pkg/storage/chunk/fetcher"
)

func TestFetchLazyChunksPropagatesErrorsWhenEnabled(t *testing.T) {
	fetchErr := errors.New("storage unavailable")

	for _, test := range []struct {
		name      string
		propagate bool
	}{
		{name: "disabled"},
		{name: "enabled", propagate: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			schema := testutils.SchemaConfig("inmemory", "v11", model.Time(0))
			f, err := fetcher.New(cache.NewMockCache(), cache.NewMockCache(), false, schema, errorChunkClient{err: fetchErr}, 0, 0)
			require.NoError(t, err)
			t.Cleanup(f.Stop)

			ctx := context.Background()
			if test.propagate {
				ctx = withChunkFetchErrorPropagation(ctx)
			}

			err = fetchLazyChunks(ctx, schema, []*LazyChunk{{
				Chunk:   chunk.Chunk{ChunkRef: logproto.ChunkRef{UserID: "tenant", Fingerprint: 1, From: 1, Through: 2, Checksum: 3}},
				Fetcher: f,
			}})
			if test.propagate {
				require.ErrorIs(t, err, fetchErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

type errorChunkClient struct {
	client.Client
	err error
}

func (c errorChunkClient) GetChunks(context.Context, []chunk.Chunk) ([]chunk.Chunk, error) {
	return nil, c.err
}

func (errorChunkClient) IsChunkNotFoundErr(error) bool {
	return false
}

func (errorChunkClient) IsRetryableErr(error) bool {
	return true
}
