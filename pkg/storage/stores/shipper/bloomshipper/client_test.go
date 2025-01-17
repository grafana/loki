package bloomshipper

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/compression"
	v1 "github.com/grafana/loki/v3/pkg/storage/bloom/v1"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/testutils"
	"github.com/grafana/loki/v3/pkg/storage/config"
)

var supportedCompressions = []compression.Codec{
	compression.None,
	compression.GZIP,
	compression.Snappy,
	compression.LZ4_64k,
	compression.LZ4_256k,
	compression.LZ4_1M,
	compression.LZ4_4M,
	compression.Flate,
	compression.Zstd,
}

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
		workingDirs: []string{dir},
		numWorkers:  3,
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
		Blocks: []BlockRef{},
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

	t.Run("does not exist - skips empty meta", func(t *testing.T) {
		notExist := MetaRef{
			Ref: Ref{
				TenantID:       "tenant",
				TableName:      "table",
				Bounds:         v1.FingerprintBounds{},
				StartTimestamp: 1000,
				EndTimestamp:   2000,
				Checksum:       1234,
			},
		}
		metas, err := c.GetMetas(ctx, []MetaRef{notExist, m1.MetaRef})
		require.NoError(t, err)
		require.Equal(t, metas, []Meta{m1})
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
		Blocks: []BlockRef{},
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

func putBlock(t *testing.T, c *BloomClient, tenant string, start model.Time, minFp, maxFp model.Fingerprint, enc compression.Codec) (Block, error) {
	step := int64((24 * time.Hour).Seconds())
	day := start.Unix() / step

	tmpDir := t.TempDir()
	fp, _ := os.CreateTemp(t.TempDir(), "*"+blockExtension+compression.ToFileExtension(enc))

	blockWriter := v1.NewDirectoryBlockWriter(tmpDir)
	err := blockWriter.Init()
	require.NoError(t, err)

	err = v1.TarCompress(enc, fp, v1.NewDirectoryBlockReader(tmpDir))
	require.NoError(t, err)

	_, _ = fp.Seek(0, 0)

	block := Block{
		BlockRef: BlockRef{
			Ref: Ref{
				TenantID:       tenant,
				Bounds:         v1.NewBounds(minFp, maxFp),
				TableName:      fmt.Sprintf("table_%d", day),
				StartTimestamp: start,
				EndTimestamp:   start.Add(12 * time.Hour),
			},
			Codec: enc,
		},
		Data: fp,
	}
	key := c.Block(block.BlockRef).Addr()
	t.Logf("PUT block to storage: %s", key)
	return block, c.client.PutObject(context.Background(), key, block.Data)
}

func TestBloomClient_GetBlock(t *testing.T) {
	for _, enc := range supportedCompressions {
		c, _ := newMockBloomClient(t)
		ctx := context.Background()

		b, err := putBlock(t, c, "tenant", parseTime("2024-02-05 00:00"), 0x0000, 0xffff, enc)
		require.NoError(t, err)

		t.Run(enc.String(), func(t *testing.T) {

			t.Run("exists", func(t *testing.T) {
				blockDir, err := c.GetBlock(ctx, b.BlockRef)
				require.NoError(t, err)
				require.Equal(t, b.BlockRef, blockDir.BlockRef)
			})

			t.Run("does not exist", func(t *testing.T) {
				blockDir, err := c.GetBlock(ctx, BlockRef{})
				require.Error(t, err)
				require.True(t, c.client.IsObjectNotFoundErr(err))
				require.Equal(t, blockDir, BlockDirectory{})
			})
		})
	}
}

func TestBloomClient_GetBlocks(t *testing.T) {
	c, _ := newMockBloomClient(t)
	ctx := context.Background()

	b1, err := putBlock(t, c, "tenant", parseTime("2024-02-05 00:00"), 0x0000, 0x0fff, compression.GZIP)
	require.NoError(t, err)
	b2, err := putBlock(t, c, "tenant", parseTime("2024-02-05 00:00"), 0x1000, 0xffff, compression.None)
	require.NoError(t, err)

	t.Run("exists", func(t *testing.T) {
		blockDirs, err := c.GetBlocks(ctx, []BlockRef{b1.BlockRef, b2.BlockRef})
		require.NoError(t, err)
		require.Equal(t, []BlockRef{b1.BlockRef, b2.BlockRef}, []BlockRef{blockDirs[0].BlockRef, blockDirs[1].BlockRef})
	})

	t.Run("does not exist", func(t *testing.T) {
		_, err := c.GetBlocks(ctx, []BlockRef{{}})
		require.Error(t, err)
		require.True(t, c.client.IsObjectNotFoundErr(err))
	})
}

func TestBloomClient_PutBlock(t *testing.T) {
	for _, enc := range supportedCompressions {
		t.Run(enc.String(), func(t *testing.T) {
			c, _ := newMockBloomClient(t)
			ctx := context.Background()

			start := parseTime("2024-02-05 12:00")

			tmpDir := t.TempDir()
			fp, _ := os.CreateTemp(t.TempDir(), "*"+blockExtension+compression.ToFileExtension(enc))

			blockWriter := v1.NewDirectoryBlockWriter(tmpDir)
			err := blockWriter.Init()
			require.NoError(t, err)

			err = v1.TarCompress(enc, fp, v1.NewDirectoryBlockReader(tmpDir))
			require.NoError(t, err)

			block := Block{
				BlockRef: BlockRef{
					Ref: Ref{
						TenantID:       "tenant",
						Bounds:         v1.NewBounds(0x0000, 0xffff),
						TableName:      "table_1234",
						StartTimestamp: start,
						EndTimestamp:   start.Add(12 * time.Hour),
					},
					Codec: enc,
				},
				Data: fp,
			}

			err = c.PutBlock(ctx, block)
			require.NoError(t, err)

			oc := c.client.(*testutils.InMemoryObjectClient)
			stored := oc.Internals()
			_, found := stored[c.Block(block.BlockRef).Addr()]
			require.True(t, found)

			blockDir, err := c.GetBlock(ctx, block.BlockRef)
			require.NoError(t, err)

			require.Equal(t, block.BlockRef, blockDir.BlockRef)
		})
	}
}

func TestBloomClient_DeleteBlocks(t *testing.T) {
	c, _ := newMockBloomClient(t)
	ctx := context.Background()

	b1, err := putBlock(t, c, "tenant", parseTime("2024-02-05 00:00"), 0x0000, 0xffff, compression.None)
	require.NoError(t, err)
	b2, err := putBlock(t, c, "tenant", parseTime("2024-02-06 00:00"), 0x0000, 0xffff, compression.GZIP)
	require.NoError(t, err)
	b3, err := putBlock(t, c, "tenant", parseTime("2024-02-07 00:00"), 0x0000, 0xffff, compression.Snappy)
	require.NoError(t, err)

	oc := c.client.(*testutils.InMemoryObjectClient)
	stored := oc.Internals()
	_, found := stored[c.Block(b1.BlockRef).Addr()]
	require.True(t, found)
	_, found = stored[c.Block(b2.BlockRef).Addr()]
	require.True(t, found)
	_, found = stored[c.Block(b3.BlockRef).Addr()]
	require.True(t, found)

	t.Run("all deleted", func(t *testing.T) {
		err = c.DeleteBlocks(ctx, []BlockRef{b1.BlockRef, b2.BlockRef})
		require.NoError(t, err)

		_, found = stored[c.Block(b1.BlockRef).Addr()]
		require.False(t, found)
		_, found = stored[c.Block(b2.BlockRef).Addr()]
		require.False(t, found)
	})

	t.Run("some not found", func(t *testing.T) {
		err = c.DeleteBlocks(ctx, []BlockRef{b3.BlockRef, b1.BlockRef})
		require.Error(t, err)
		require.True(t, c.client.IsObjectNotFoundErr(err))

		_, found = stored[c.Block(b3.BlockRef).Addr()]
		require.False(t, found)
	})
}

type mockListClient struct {
	client.ObjectClient
	counter int
}

func (c *mockListClient) List(_ context.Context, prefix string, _ string) ([]client.StorageObject, []client.StorageCommonPrefix, error) {
	c.counter++
	objects := []client.StorageObject{
		{Key: path.Join(path.Base(prefix), "object")},
	}
	prefixes := []client.StorageCommonPrefix{
		client.StorageCommonPrefix(prefix),
	}
	return objects, prefixes, nil
}

func (c *mockListClient) Stop() {
}

func TestBloomClient_CachedListOpObjectClient(t *testing.T) {

	t.Run("list call with delimiter returns error", func(t *testing.T) {
		downstreamClient := &mockListClient{}
		c := newCachedListOpObjectClient(downstreamClient, 100*time.Millisecond, 10*time.Millisecond)
		t.Cleanup(c.Stop)

		_, _, err := c.List(context.Background(), "prefix/", "/")
		require.Error(t, err)
	})

	t.Run("list calls are cached by prefix", func(t *testing.T) {
		downstreamClient := &mockListClient{}
		c := newCachedListOpObjectClient(downstreamClient, 100*time.Millisecond, 10*time.Millisecond)
		t.Cleanup(c.Stop)

		// cache miss
		res, _, err := c.List(context.Background(), "a/", "")
		require.NoError(t, err)
		require.Equal(t, 1, downstreamClient.counter)
		require.Equal(t, []client.StorageObject{{Key: "a/object"}}, res)

		// cache miss
		res, _, err = c.List(context.Background(), "b/", "")
		require.NoError(t, err)
		require.Equal(t, 2, downstreamClient.counter)
		require.Equal(t, []client.StorageObject{{Key: "b/object"}}, res)

		// cache hit
		res, _, err = c.List(context.Background(), "a/", "")
		require.NoError(t, err)
		require.Equal(t, 2, downstreamClient.counter)
		require.Equal(t, []client.StorageObject{{Key: "a/object"}}, res)

		// wait for >=ttl so items are expired
		time.Sleep(150 * time.Millisecond)

		// cache miss
		res, _, err = c.List(context.Background(), "a/", "")
		require.NoError(t, err)
		require.Equal(t, 3, downstreamClient.counter)
		require.Equal(t, []client.StorageObject{{Key: "a/object"}}, res)
	})

}
