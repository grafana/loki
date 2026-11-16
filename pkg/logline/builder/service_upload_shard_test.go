package builder

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"
	"golang.org/x/sync/errgroup"

	"github.com/grafana/loki/v3/pkg/logline"
	"github.com/grafana/loki/v3/pkg/logline/store"
	"github.com/grafana/loki/v3/pkg/logproto"
)

func TestPreparedIndexes_UploadZeroValue(t *testing.T) {
	var prepared PreparedIndexes
	require.Zero(t, prepared.Len())
	require.Error(t, prepared.Upload(context.Background(), nil))
}

func TestPreparedIndexes_ConcurrentUpload(t *testing.T) {
	cfg := roundTripConfig(t, 4, minPostingsBufferPairs)
	b, err := NewBuilder(cfg, "2026-01-01", nil, nil)
	require.NoError(t, err)
	defer b.Clear()
	require.NoError(t, b.ProcessStream(&logproto.Stream{Entries: retryTestEntries()}, nil, docIDEpoch, RecordRef{}))
	prepared, err := b.Prepare()
	require.NoError(t, err)
	require.Equal(t, 4, prepared.Len())

	contents := make(map[string][]byte)
	metas := make([]store.Meta, 0, prepared.Len())
	for _, f := range prepared.files {
		meta := store.Meta{Date: f.date, StorageID: f.storageID}
		require.NoError(t, meta.SetFileInfo(f.file))
		data, err := os.ReadFile(f.file.Name())
		require.NoError(t, err)
		contents[meta.IndexPath()] = data
		metas = append(metas, meta)
	}

	bucket, indexStore := newTestStore(t)
	ctx := context.Background()
	start := make(chan struct{})
	var uploads errgroup.Group
	for range 16 {
		copied := prepared
		uploads.Go(func() error {
			<-start
			return copied.Upload(ctx, indexStore)
		})
	}
	close(start)
	require.NoError(t, uploads.Wait())
	// Retrying must preserve the same objects, not hide a corrupt first upload.
	require.NoError(t, prepared.Upload(ctx, indexStore))
	for _, want := range metas {
		r, err := bucket.Get(ctx, want.IndexPath())
		require.NoError(t, err)
		data, err := io.ReadAll(r)
		require.NoError(t, err)
		require.NoError(t, r.Close())
		require.Equal(t, contents[want.IndexPath()], data)

		r, err = bucket.Get(ctx, want.MetaPath())
		require.NoError(t, err)
		var got store.Meta
		require.NoError(t, json.NewDecoder(r).Decode(&got))
		require.NoError(t, r.Close())
		require.Equal(t, want.SizeBytes, got.SizeBytes)
		require.Equal(t, want.Hash, got.Hash)
	}
}

// TestService_UploadSetsShardMeta verifies that shard fields from fileInfo are
// propagated into the Meta written to the store during uploadPartialIndexes.
func TestService_UploadSetsShardMeta(t *testing.T) {
	cluster, cfg := setupKafkaTest(t)
	defer cluster.Close()

	cfg.Index.ShardCount = 4
	cfg.Index.ShardAlgorithm = "first_byte"
	cfg.Index.NgramLength = 6
	cfg.Index.DocumentInterval = 100 * time.Millisecond
	cfg.Index.DocumentShardBits = logline.DefaultDocumentShardBits

	metas := uploadAndReadMetas(t, cfg)
	for _, m := range metas {
		require.Equal(t, 4, m.ShardCount)
		require.Equal(t, "first_byte", m.ShardAlgorithm)
		require.GreaterOrEqual(t, m.ShardValue, 0)
		require.Less(t, m.ShardValue, 4)
		require.Zero(t, m.DocumentShardBits, "v3 indexes are time-only")
	}
}

// TestService_UploadSetsDocumentShardBitsMeta verifies that v5 uploads record
// the document shard bits in meta.json.
func TestService_UploadSetsDocumentShardBitsMeta(t *testing.T) {
	cluster, cfg := setupKafkaTest(t)
	defer cluster.Close()

	cfg.Index.Version = "v5"
	cfg.Index.DocumentInterval = 0
	cfg.Index.DocumentShardBits = logline.DefaultDocumentShardBits

	metas := uploadAndReadMetas(t, cfg)
	for _, m := range metas {
		require.Equal(t, "v5", m.Version)
		require.Equal(t, 16*time.Second, m.DocumentInterval)
		require.Equal(t, logline.DefaultDocumentShardBits, m.DocumentShardBits)
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
	require.NoError(t, svc.activeBuilder.ProcessStream(stream, parseLabelsOrNil(stream.Labels), now, RecordRef{}))

	prepared, err := svc.activeBuilder.Prepare()
	require.NoError(t, err)
	require.NotZero(t, prepared.Len())

	ctx := context.Background()
	require.NoError(t, prepared.Upload(ctx, indexStore))

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
