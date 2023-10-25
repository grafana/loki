package bloomgateway

import (
	"context"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/pkg/errors"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/logproto"
)

type noopClient struct {
	err       error // error to return
	callCount int
}

// FilterChunks implements Client.
func (c *noopClient) FilterChunks(ctx context.Context, tenant string, from, through model.Time, groups []*logproto.GroupedChunkRefs, filters ...*logproto.LineFilterExpression) ([]*logproto.GroupedChunkRefs, error) {
	c.callCount++
	return groups, c.err
}

func TestBloomQuerier(t *testing.T) {
	logger := log.NewNopLogger()
	tenant := "fake"

	t.Run("client not called when filters are empty", func(t *testing.T) {
		c := &noopClient{}
		bq := NewBloomQuerier(c, logger)

		ctx := context.Background()
		through := model.Now()
		from := through.Add(-12 * time.Hour)
		chunkRefs := []*logproto.ChunkRef{
			{Fingerprint: 3000, UserID: tenant, Checksum: 1},
			{Fingerprint: 1000, UserID: tenant, Checksum: 2},
			{Fingerprint: 2000, UserID: tenant, Checksum: 3},
		}
		filters := []*logproto.LineFilterExpression{}
		res, err := bq.FilterChunkRefs(ctx, tenant, from, through, chunkRefs, filters...)
		require.NoError(t, err)
		require.Equal(t, chunkRefs, res)
		require.Equal(t, 0, c.callCount)
	})

	t.Run("client not called when chunkRefs are empty", func(t *testing.T) {
		c := &noopClient{}
		bq := NewBloomQuerier(c, logger)

		ctx := context.Background()
		through := model.Now()
		from := through.Add(-12 * time.Hour)
		chunkRefs := []*logproto.ChunkRef{}
		filters := []*logproto.LineFilterExpression{
			{Operator: 0, Match: "uuid"},
		}
		res, err := bq.FilterChunkRefs(ctx, tenant, from, through, chunkRefs, filters...)
		require.NoError(t, err)
		require.Equal(t, chunkRefs, res)
		require.Equal(t, 0, c.callCount)
	})

	t.Run("querier propagates error from client", func(t *testing.T) {
		c := &noopClient{err: errors.New("something went wrong")}
		bq := NewBloomQuerier(c, logger)

		ctx := context.Background()
		through := model.Now()
		from := through.Add(-12 * time.Hour)
		chunkRefs := []*logproto.ChunkRef{
			{Fingerprint: 3000, UserID: tenant, Checksum: 1},
			{Fingerprint: 1000, UserID: tenant, Checksum: 2},
			{Fingerprint: 2000, UserID: tenant, Checksum: 3},
		}
		filters := []*logproto.LineFilterExpression{
			{Operator: 0, Match: "uuid"},
		}
		res, err := bq.FilterChunkRefs(ctx, tenant, from, through, chunkRefs, filters...)
		require.Error(t, err)
		require.Nil(t, res)
	})
}
