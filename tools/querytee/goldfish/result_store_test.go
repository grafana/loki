package goldfish

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"
)

func TestBucketResultStoreStoresCompressedPayload(t *testing.T) {
	bkt := objstore.NewInMemBucket()
	inst := objstore.WrapWith(bkt, objstore.BucketMetrics(prometheus.NewRegistry(), "test"))
	store := &bucketResultStore{
		bucket:      inst,
		prefix:      "goldfish/results",
		compression: ResultsCompressionGzip,
		backend:     ResultsBackendGCS,
		logger:      log.NewNopLogger(),
	}

	payload := []byte(`{"status":"success","data":{"resultType":"matrix","result":[]}}`)
	ts := time.Unix(1700000000, 0)
	opts := StoreOptions{
		CorrelationID: "abc-123",
		CellLabel:     "cell-a",
		TenantID:      "tenant-1",
		QueryType:     "query_range",
		Hash:          "deadbeef",
		StatusCode:    200,
		Timestamp:     ts,
	}

	res, err := store.Store(context.Background(), payload, opts)
	require.NoError(t, err)
	require.Equal(t, ResultsCompressionGzip, res.Compression)
	require.NotZero(t, res.Size)
	require.Equal(t, int64(len(payload)), res.OriginalSize)

	key := buildObjectKey(store.prefix, opts.CorrelationID, opts.CellLabel, ts, store.compression)
	rc, err := inst.Get(context.Background(), key)
	require.NoError(t, err)
	defer rc.Close()

	gz, err := gzip.NewReader(rc)
	require.NoError(t, err)
	decompressed, err := io.ReadAll(gz)
	require.NoError(t, err)
	require.Equal(t, payload, decompressed)

	metaRC, err := inst.Get(context.Background(), key+".meta.json")
	require.NoError(t, err)
	defer metaRC.Close()

	var meta map[string]any
	require.NoError(t, json.NewDecoder(metaRC).Decode(&meta))
	require.Equal(t, opts.CorrelationID, meta["correlation_id"])
	require.Equal(t, store.compression, meta["compression"])
}

func TestBucketResultStoreStoresUncompressedPayload(t *testing.T) {
	bkt := objstore.NewInMemBucket()
	inst := objstore.WrapWith(bkt, objstore.BucketMetrics(prometheus.NewRegistry(), "test"))
	store := &bucketResultStore{
		bucket:      inst,
		prefix:      "goldfish/results",
		compression: ResultsCompressionNone,
		backend:     ResultsBackendS3,
		logger:      log.NewNopLogger(),
	}

	payload := []byte("{}")
	opts := StoreOptions{CorrelationID: "abc", CellLabel: "cell-b"}

	res, err := store.Store(context.Background(), payload, opts)
	require.NoError(t, err)
	require.Equal(t, ResultsCompressionNone, res.Compression)
	require.Equal(t, int64(len(payload)), res.Size)

	key := buildObjectKey(store.prefix, opts.CorrelationID, opts.CellLabel, opts.Timestamp, store.compression)
	rc, err := inst.Get(context.Background(), key)
	require.NoError(t, err)
	body, err := io.ReadAll(rc)
	require.NoError(t, err)
	require.Equal(t, payload, body)
}
