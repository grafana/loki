package builder

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/logline/store"
	"github.com/grafana/loki/v3/pkg/logproto"
)

// TestService_UploadSetsShardMeta verifies that shard fields from fileInfo are
// propagated into the Meta written to the store during uploadPartialIndexes.
func TestService_UploadSetsShardMeta(t *testing.T) {
	cluster, cfg := setupKafkaTest(t)
	defer cluster.Close()

	cfg.Index.ShardCount = 4
	cfg.Index.ShardAlgorithm = "first_byte"
	cfg.Index.NgramLength = 6
	cfg.Index.DocumentInterval = 100 * time.Millisecond

	metas := uploadAndReadMetas(t, cfg)
	for _, m := range metas {
		require.Equal(t, 4, m.ShardCount)
		require.Equal(t, "first_byte", m.ShardAlgorithm)
		require.GreaterOrEqual(t, m.ShardValue, 0)
		require.Less(t, m.ShardValue, 4)
		require.Zero(t, m.DocumentShards, "v3 indexes are time-only")
	}
}

// TestService_UploadSetsDocumentShardsMeta verifies that v5 uploads record the
// resolved document shard count in meta.json.
func TestService_UploadSetsDocumentShardsMeta(t *testing.T) {
	cluster, cfg := setupKafkaTest(t)
	defer cluster.Close()

	cfg.Index.Version = "v5"
	cfg.Index.DocumentInterval = 0
	cfg.Index.DocumentShards = 0

	metas := uploadAndReadMetas(t, cfg)
	for _, m := range metas {
		require.Equal(t, "v5", m.Version)
		require.Equal(t, 16*time.Second, m.DocumentInterval)
		require.Equal(t, 32, m.DocumentShards)
	}
}

// uploadAndReadMetas validates cfg, indexes one stream, uploads the result,
// and returns every meta.json written to the store.
func uploadAndReadMetas(t *testing.T, cfg Config) []store.Meta {
	t.Helper()
	cfg.ScratchDir = t.TempDir()
	require.NoError(t, cfg.Validate())

	bucket := objstore.NewInMemBucket()
	indexStore, err := store.NewStore(bucket, store.Config{MinDate: "0001-01-01"}, log.NewNopLogger(), nil)
	require.NoError(t, err)

	svc, err := New(indexStore, cfg, "2026-01-01", newDefaultFakePartitionRing(), log.NewNopLogger(), prometheus.NewRegistry())
	require.NoError(t, err)
	require.NoError(t, svc.initKafkaClient())
	defer svc.client.Close()

	now := time.Now()
	stream := &logproto.Stream{
		Labels: `{app="api"}`,
		Entries: []logproto.Entry{
			{Timestamp: now, Line: "error: connection failed to primary database server"},
		},
	}
	_ = svc.activeBuilder.processStream(stream, parseLabelsOrNil(stream.Labels), now, recordRef{})

	files, err := svc.activeBuilder.prepareIndexes()
	require.NoError(t, err)
	require.NotEmpty(t, files)

	ctx := context.Background()
	require.NoError(t, svc.uploadPartialIndexes(ctx, files))

	var metas []store.Meta
	require.NoError(t, bucket.Iter(ctx, "", func(name string) error {
		if !strings.HasSuffix(name, "meta.json") {
			return nil
		}
		r, err := bucket.Get(ctx, name)
		if err != nil {
			return err
		}
		defer r.Close()
		data, err := io.ReadAll(r)
		if err != nil {
			return err
		}
		var m store.Meta
		if err := json.Unmarshal(data, &m); err != nil {
			return err
		}
		metas = append(metas, m)
		return nil
	}, objstore.WithRecursiveIter()))
	require.NotEmpty(t, metas)
	return metas
}
