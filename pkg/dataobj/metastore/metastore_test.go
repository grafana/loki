package metastore

import (
	"context"
	"fmt"
	"os"
	"runtime/pprof"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/grafana/dskit/backoff"
	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/thanos-io/objstore"
)

func BenchmarkWriteMetastores(t *testing.B) {
	start := time.Now()
	ctx := context.Background()
	bucket := objstore.NewInMemBucket()
	tenantID := "test-tenant"
	warmupLabels := pprof.Labels("state", "warmup")
	/* 	targetObjectSize := 128 // MB
	   	targetPageSize := 4     // MB
	   	bufferSize := targetPageSize * 8
	   	targetSectionSize := targetObjectSize / 8

	   	builderCfg := dataobj.BuilderConfig{
	   		SHAPrefixSize: 2,
	   	}
	   	_ = builderCfg.TargetObjectSize.Set(fmt.Sprintf("%dMB", targetObjectSize))
	   	_ = builderCfg.TargetPageSize.Set(fmt.Sprintf("%dMB", targetPageSize))
	   	_ = builderCfg.BufferSize.Set(fmt.Sprintf("%dMB", bufferSize))
	   	_ = builderCfg.TargetSectionSize.Set(fmt.Sprintf("%dMB", targetSectionSize)) */

	m, err := NewMetastoreManager(bucket, tenantID, log.NewNopLogger(), prometheus.DefaultRegisterer)
	require.NoError(t, err)

	// Set limits for the test
	m.backoff = backoff.New(context.TODO(), backoff.Config{
		MinBackoff: 10 * time.Millisecond,
		MaxBackoff: 100 * time.Millisecond,
		MaxRetries: 3,
	})

	// Add test data spanning multiple metastore windows
	now := time.Date(2025, 1, 1, 15, 0, 0, 0, time.UTC)
	testStreams := make([]dataobj.StreamSummary, 0, 1_000_000)

	baseLabels, err := syntax.ParseLabels(`{__time_shard__="1737500400_1737504000", category="kube-audit", cluster="dev-eu-west-2", job="integration/azure-logexport", service_name="integration/azure-logexport"}`)
	require.NoError(t, err)

	scratchBuilder := labels.NewBuilder(baseLabels)

	for i := 0; i < 1_000_000; i++ {
		scratchBuilder.Set("__stream_shard__", fmt.Sprintf("%d", i))
		labels := scratchBuilder.Labels()

		testStreams = append(testStreams, dataobj.StreamSummary{
			Labels:       labels,
			MinTimestamp: now.Add(-1 * time.Hour),
			MaxTimestamp: now,
		})
	}
	flushResult := dataobj.FlushResult{
		Path:         "test-dataobj-path",
		Streams:      testStreams,
		MinTimestamp: now.Add(-1 * time.Hour),
		MaxTimestamp: now,
	}

	f, err := os.OpenFile("/Users/benclive/Downloads/2025-01-24T00_00_00Z.store", os.O_CREATE|os.O_RDWR, 0644)
	require.NoError(t, err)
	defer f.Close()

	err = bucket.Upload(ctx, "tenant-test-tenant/metastore/2025-01-01T12:00:00Z.store", f)
	require.NoError(t, err)

	fmt.Printf("init took %s\n", time.Since(start))
	initialWriteStart := time.Now()
	pprof.Do(ctx, warmupLabels, func(ctx context.Context) {
		// Initial write to warm the pool etc.
		err := m.UpdateMetastore(ctx, flushResult)
		require.NoError(t, err)
	})
	fmt.Printf("initial write took %s\n", time.Since(initialWriteStart))

	warmupLabels = pprof.Labels("state", "benchmark")
	pprof.Do(ctx, warmupLabels, func(ctx context.Context) {
		t.ResetTimer()
		t.ReportAllocs()
		for i := 0; i < t.N; i++ {
			// Test writing metastores
			err = m.UpdateMetastore(ctx, flushResult)
			require.NoError(t, err)
		}
	})
	/*
		// Verify the metastores were written correctly
		// We expect two metastore files because our test data spans two 12-hour windows
		for _, window := range []time.Time{
			now.Truncate(metastoreWindowSize),
			//now.Add(-12 * time.Hour).Truncate(metastoreWindowSize),
		} {
			metastorePath := "tenant-test-tenant/metastore/" + window.Format(time.RFC3339) + ".store"
			exists, err := bucket.Exists(ctx, metastorePath)
			require.NoError(t, err)
			require.True(t, exists, "metastore file should exist: %s", metastorePath)

			reader, err := bucket.Get(ctx, metastorePath)
			require.NoError(t, err)

			buf, err := io.ReadAll(reader)
			require.NoError(t, err)

			dec := encoding.ReadSeekerDecoder(bytes.NewReader(buf))
			streamCount := 0
			for range streams.Iter(ctx, dec) {
				streamCount++
			}
			require.Greater(t, streamCount, 0, "Metastore should contain at least one stream")
		} */

}
