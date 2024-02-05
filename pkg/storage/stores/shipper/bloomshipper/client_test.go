package bloomshipper

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/go-kit/log"
	v1 "github.com/grafana/loki/pkg/storage/bloom/v1"
	"github.com/grafana/loki/pkg/storage/chunk/client/testutils"
	"github.com/grafana/loki/pkg/storage/config"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"
)

const (
	day = 24 * time.Hour
)

func parseTime(s string) model.Time {
	t, err := time.Parse("2006-01-02 15:04", s)
	if err != nil {
		panic(err)
	}
	return model.TimeFromUnix(t.Unix())
}

func parseDayTime(s string) config.DayTime {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return config.DayTime{
		Time: model.TimeFromUnix(t.Unix()),
	}
}

func newMockBloomClient(t *testing.T) (*BloomClient, string) {
	oc := testutils.NewInMemoryObjectClient()
	dir := t.TempDir()
	logger := log.NewLogfmtLogger(os.Stderr)
	cfg := bloomStoreConfig{
		workingDir: dir,
		numWorkers: 3,
	}
	client, err := NewBloomClient(cfg, oc, logger)
	require.NoError(t, err)
	return client, dir
}

func putMeta(c *BloomClient, tenant string, start model.Time, minFp, maxFp model.Fingerprint) (Meta, error) {
	step := int64((24 * time.Hour).Seconds())
	day := start.Unix() / step
	meta := Meta{
		MetaRef: MetaRef{
			Ref: Ref{
				TenantID:  tenant,
				Bounds:    v1.NewBounds(minFp, maxFp),
				TableName: fmt.Sprintf("table_%d", day),
				// Unused
				// StartTimestamp: start,
				// EndTimestamp:   start.Add(12 * time.Hour),
			},
		},
		Blocks:     []BlockRef{},
		Tombstones: []BlockRef{},
	}
	raw, _ := json.Marshal(meta)
	return meta, c.client.PutObject(context.Background(), c.Meta(meta.MetaRef).Addr(), bytes.NewReader(raw))
}

func TestBloomClient_GetMeta(t *testing.T) {
	c, _ := newMockBloomClient(t)
	ctx := context.Background()

	m, err := putMeta(c, "tenant", parseTime("2024-02-05 00:00"), 0x0000, 0xffff)
	require.NoError(t, err)

	t.Run("exists", func(t *testing.T) {
		meta, err := c.GetMeta(ctx, m.MetaRef)
		require.NoError(t, err)
		require.Equal(t, meta, m)
	})

	t.Run("does not exist", func(t *testing.T) {
		meta, err := c.GetMeta(ctx, MetaRef{})
		require.Error(t, err)
		require.True(t, c.client.IsObjectNotFoundErr(err))
		require.Equal(t, meta, Meta{})
	})
}

func TestBloomClient_GetMetas(t *testing.T) {
	c, _ := newMockBloomClient(t)
	ctx := context.Background()

	m1, err := putMeta(c, "tenant", parseTime("2024-02-05 00:00"), 0x0000, 0x0fff)
	require.NoError(t, err)
	m2, err := putMeta(c, "tenant", parseTime("2024-02-05 00:00"), 0x1000, 0xffff)
	require.NoError(t, err)

	t.Run("exists", func(t *testing.T) {
		metas, err := c.GetMetas(ctx, []MetaRef{m1.MetaRef, m2.MetaRef})
		require.NoError(t, err)
		require.Equal(t, metas, []Meta{m1, m2})
	})

	t.Run("does not exist", func(t *testing.T) {
		metas, err := c.GetMetas(ctx, []MetaRef{MetaRef{}})
		require.Error(t, err)
		require.True(t, c.client.IsObjectNotFoundErr(err))
		require.Equal(t, metas, []Meta{Meta{}})
	})
}

func TestBloomClient_PutMeta(t *testing.T) {
	c, _ := newMockBloomClient(t)
	ctx := context.Background()

	meta := Meta{
		MetaRef: MetaRef{
			Ref: Ref{
				TenantID:  "tenant",
				Bounds:    v1.NewBounds(0x0000, 0xffff),
				TableName: "table_1234",
				// Unused
				// StartTimestamp: start,
				// EndTimestamp:   start.Add(12 * time.Hour),
			},
		},
		Blocks:     []BlockRef{},
		Tombstones: []BlockRef{},
	}

	err := c.PutMeta(ctx, meta)
	require.NoError(t, err)

	oc := c.client.(*testutils.InMemoryObjectClient)
	stored := oc.Internals()
	_, found := stored[c.Meta(meta.MetaRef).Addr()]
	require.True(t, found)

	fromStorage, err := c.GetMeta(ctx, meta.MetaRef)
	require.NoError(t, err)

	require.Equal(t, meta, fromStorage)
}

func TestBloomClient_DeleteMetas(t *testing.T) {
	c, _ := newMockBloomClient(t)
	ctx := context.Background()

	m1, err := putMeta(c, "tenant", parseTime("2024-02-05 00:00"), 0x0000, 0xffff)
	require.NoError(t, err)
	m2, err := putMeta(c, "tenant", parseTime("2024-02-06 00:00"), 0x0000, 0xffff)
	require.NoError(t, err)
	m3, err := putMeta(c, "tenant", parseTime("2024-02-07 00:00"), 0x0000, 0xffff)
	require.NoError(t, err)

	oc := c.client.(*testutils.InMemoryObjectClient)
	stored := oc.Internals()
	_, found := stored[c.Meta(m1.MetaRef).Addr()]
	require.True(t, found)
	_, found = stored[c.Meta(m2.MetaRef).Addr()]
	require.True(t, found)
	_, found = stored[c.Meta(m3.MetaRef).Addr()]
	require.True(t, found)

	t.Run("all deleted", func(t *testing.T) {
		err = c.DeleteMetas(ctx, []MetaRef{m1.MetaRef, m2.MetaRef})
		require.NoError(t, err)

		_, found = stored[c.Meta(m1.MetaRef).Addr()]
		require.False(t, found)
		_, found = stored[c.Meta(m2.MetaRef).Addr()]
		require.False(t, found)
	})

	t.Run("some not found", func(t *testing.T) {
		err = c.DeleteMetas(ctx, []MetaRef{m3.MetaRef, m1.MetaRef})
		require.Error(t, err)
		require.True(t, c.client.IsObjectNotFoundErr(err))

		_, found = stored[c.Meta(m3.MetaRef).Addr()]
		require.False(t, found)
	})
}

func TestBloomClient_GetBlock(t *testing.T) {}

func TestBloomClient_GetBlocks(t *testing.T) {}

func TestBloomClient_PutBlock(t *testing.T) {}

func TestBloomClient_DeleteBlocks(t *testing.T) {}
