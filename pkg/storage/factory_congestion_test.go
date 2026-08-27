package storage

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/flagext"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/storage/bucket"
	"github.com/grafana/loki/v3/pkg/storage/bucket/s3"
	"github.com/grafana/loki/v3/pkg/storage/chunk"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/congestion"
	"github.com/grafana/loki/v3/pkg/storage/config"

	"github.com/grafana/loki/v3/pkg/logproto"
)

// Each test here that calls NewChunkClient must use a unique PeriodConfig.From date.
// NewChunkClient registers congestion metrics under a name built from that date and
// never unregisters them, so a shared date panics the second test.

// The XML body must keep this shape. minio-go maps it to a retryable SlowDown.
func writeS3SlowDown(w http.ResponseWriter, key string) {
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(http.StatusServiceUnavailable)
	_, _ = fmt.Fprintf(w,
		`<?xml version="1.0" encoding="UTF-8"?><Error><Code>SlowDown</Code><Message>Please reduce your request rate.</Message><Resource>/test-bucket/%s</Resource><RequestId>test</RequestId></Error>`,
		key)
}

func newThrottlingS3(t *testing.T, chunkKey string) (*httptest.Server, *atomic.Int64) {
	t.Helper()

	var serverGets atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, chunkKey) {
			serverGets.Add(1)
			writeS3SlowDown(w, chunkKey)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	return srv, &serverGets
}

func congestionTestCfg(t *testing.T, srv *httptest.Server, controller congestion.ControllerConfig, retry congestion.RetrierConfig) Config {
	t.Helper()

	var cfg Config
	flagext.DefaultValues(&cfg)

	cfg.UseThanosObjstore = true
	cfg.ObjectStore.S3 = s3.Config{
		// Region avoids a GetBucketLocation round trip.
		Endpoint:        srv.Listener.Addr().String(),
		Region:          "test",
		BucketName:      "test-bucket",
		AccessKeyID:     "test",
		SecretAccessKey: flagext.SecretWithValue("test"),
		Insecure:        true,
	}
	cfg.CongestionControl = congestion.Config{
		Enabled:    true,
		Controller: controller,
		Retry:      retry,
	}

	return cfg
}

func aimdController() congestion.ControllerConfig {
	return congestion.ControllerConfig{
		Strategy: congestion.StrategyAIMD,
		// Start must stay high enough that the rate limiter never blocks these tests.
		AIMD: congestion.AIMD{Start: 2000, UpperBound: 10000, BackoffFactor: 0.5},
	}
}

func limitedRetrier() congestion.RetrierConfig {
	return congestion.RetrierConfig{Strategy: congestion.RetryStrategyLimited, Limit: 2}
}

func TestCongestionControlReplacesThanosRetries(t *testing.T) {
	enabled := congestion.Config{
		Enabled:    true,
		Controller: aimdController(),
		Retry:      limitedRetrier(),
	}

	tests := map[string]struct {
		cfg       congestion.Config
		storeType string
		expected  bool
	}{
		"s3":                {cfg: enabled, storeType: bucket.S3, expected: true},
		"gcs":               {cfg: enabled, storeType: bucket.GCS, expected: true},
		"unsupported store": {cfg: enabled, storeType: bucket.Azure, expected: false},
		"disabled":          {cfg: congestion.Config{}, storeType: bucket.S3, expected: false},
		"no controller":     {cfg: congestion.Config{Enabled: true, Retry: limitedRetrier()}, storeType: bucket.S3, expected: false},
		"no retrier":        {cfg: congestion.Config{Enabled: true, Controller: aimdController()}, storeType: bucket.S3, expected: false},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, test.expected, congestionControlReplacesThanosRetries(test.cfg, test.storeType))
		})
	}
}

func chunkFixture(from string) (config.SchemaConfig, config.PeriodConfig, chunk.Chunk, string) {
	periodCfg := config.PeriodConfig{
		From:       config.DayTime{Time: timeToModelTime(parseDate(from))},
		IndexType:  "tsdb",
		ObjectType: "s3",
		Schema:     "v13",
		IndexTables: config.IndexPeriodicTableConfig{
			PeriodicTableConfig: config.PeriodicTableConfig{
				Prefix: "index_",
				Period: 24 * time.Hour,
			}},
	}
	schemaCfg := config.SchemaConfig{Configs: []config.PeriodConfig{periodCfg}}

	c := chunk.Chunk{
		ChunkRef: logproto.ChunkRef{
			UserID:      "fake",
			Fingerprint: 1,
			From:        periodCfg.From.Time,
			Through:     periodCfg.From.Add(time.Hour),
			Checksum:    1,
		},
	}

	return schemaCfg, periodCfg, c, schemaCfg.ExternalKey(c.ChunkRef)
}

func TestNewChunkClient_ThanosCongestionControl_DisablesInnerRetries(t *testing.T) {
	schemaCfg, periodCfg, c, key := chunkFixture("2026-01-01")
	srv, serverGets := newThrottlingS3(t, key)

	cfg := congestionTestCfg(t, srv, aimdController(), limitedRetrier())

	chunkClient, err := NewChunkClient(bucket.S3, "test", cfg, schemaCfg, periodCfg, prometheus.NewPedanticRegistry(), cm, log.NewNopLogger())
	require.NoError(t, err)
	t.Cleanup(chunkClient.Stop)

	_, err = chunkClient.GetChunks(context.Background(), []chunk.Chunk{c})
	require.Error(t, err)

	require.EqualValues(t, 3, serverGets.Load())
}

// Other callers of NewObjectClient have no replacement retrier, so the exported
// constructor must keep the object-store retries.
func TestNewObjectClient_ThanosKeepsInnerRetries(t *testing.T) {
	t.Parallel()

	_, _, _, key := chunkFixture("2026-01-02")
	srv, serverGets := newThrottlingS3(t, key)

	cfg := congestionTestCfg(t, srv, aimdController(), limitedRetrier())

	objectClient, err := NewObjectClient(bucket.S3, "test", cfg, cm)
	require.NoError(t, err)
	t.Cleanup(objectClient.Stop)

	_, _, err = objectClient.GetObject(context.Background(), key)
	require.Error(t, err)

	require.EqualValues(t, 10, serverGets.Load())
}

func TestNewChunkClient_ThanosCongestionControl_NamedStore(t *testing.T) {
	schemaCfg, periodCfg, c, key := chunkFixture("2026-01-03")
	srv, serverGets := newThrottlingS3(t, key)

	cfg := congestionTestCfg(t, srv, aimdController(), limitedRetrier())
	cfg.ObjectStore.NamedStores.S3 = map[string]bucket.NamedS3StorageConfig{
		"my-store": bucket.NamedS3StorageConfig(cfg.ObjectStore.S3),
	}
	// Only Validate populates the map that LookupStoreType reads. Without this call,
	// NewClient rejects "my-store" as an unsupported backend.
	require.NoError(t, cfg.ObjectStore.NamedStores.Validate())

	chunkClient, err := NewChunkClient("my-store", "test", cfg, schemaCfg, periodCfg, prometheus.NewPedanticRegistry(), cm, log.NewNopLogger())
	require.NoError(t, err)
	t.Cleanup(chunkClient.Stop)

	_, err = chunkClient.GetChunks(context.Background(), []chunk.Chunk{c})
	require.Error(t, err)

	require.EqualValues(t, 3, serverGets.Load())
}

// If the count drops to 1, no retrier remains, and Fetcher.FetchChunks returns
// partial results with a nil error.
func TestNewChunkClient_ThanosCongestionControl_TwoFlagKeepsInnerRetries(t *testing.T) {
	schemaCfg, periodCfg, c, key := chunkFixture("2026-01-04")
	srv, serverGets := newThrottlingS3(t, key)

	cfg := congestionTestCfg(t, srv, aimdController(), congestion.RetrierConfig{})

	chunkClient, err := NewChunkClient(bucket.S3, "test", cfg, schemaCfg, periodCfg, prometheus.NewPedanticRegistry(), cm, log.NewNopLogger())
	require.NoError(t, err)
	t.Cleanup(chunkClient.Stop)

	_, err = chunkClient.GetChunks(context.Background(), []chunk.Chunk{c})
	require.Error(t, err)

	require.EqualValues(t, 10, serverGets.Load())
}

func TestNewChunkClient_ThanosCongestionControl_NonAIMDKeepsInnerRetries(t *testing.T) {
	schemaCfg, periodCfg, c, key := chunkFixture("2026-01-05")
	srv, serverGets := newThrottlingS3(t, key)

	cfg := congestionTestCfg(t, srv, congestion.ControllerConfig{}, limitedRetrier())

	chunkClient, err := NewChunkClient(bucket.S3, "test", cfg, schemaCfg, periodCfg, prometheus.NewPedanticRegistry(), cm, log.NewNopLogger())
	require.NoError(t, err)
	t.Cleanup(chunkClient.Stop)

	_, err = chunkClient.GetChunks(context.Background(), []chunk.Chunk{c})
	require.Error(t, err)

	require.EqualValues(t, 10, serverGets.Load())
}
