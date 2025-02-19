package bench

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"
	"github.com/thanos-io/objstore/providers/filesystem"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/dataobj/uploader"
	"github.com/grafana/loki/v3/pkg/logproto"
)

// testDataBuilder helps build test data for querier tests.
type testDataBuilder struct {
	t      testing.TB
	bucket objstore.Bucket
	dir    string

	tenantID string
	builder  *dataobj.Builder
	meta     *metastore.Manager
	uploader *uploader.Uploader
}

func newTestDataBuilder(t testing.TB, tenantID string) *testDataBuilder {
	dir := t.TempDir()
	bucket, err := filesystem.NewBucket(dir)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, bucket.Close())
		os.RemoveAll(dir)
	})

	// Create required directories for metastore
	metastoreDir := filepath.Join(dir, "tenant-"+tenantID, "metastore")
	require.NoError(t, os.MkdirAll(metastoreDir, 0o755))

	builder, err := dataobj.NewBuilder(dataobj.BuilderConfig{
		TargetPageSize:    1024 * 1024,      // 1MB
		TargetObjectSize:  10 * 1024 * 1024, // 10MB
		TargetSectionSize: 1024 * 1024,      // 1MB
		BufferSize:        1024 * 1024,      // 1MB
	})
	require.NoError(t, err)

	meta := metastore.NewManager(bucket, tenantID, log.NewLogfmtLogger(os.Stdout))
	require.NoError(t, meta.RegisterMetrics(prometheus.NewRegistry()))

	uploader := uploader.New(uploader.Config{SHAPrefixSize: 2}, bucket, tenantID)
	require.NoError(t, uploader.RegisterMetrics(prometheus.NewRegistry()))

	return &testDataBuilder{
		t:        t,
		bucket:   bucket,
		dir:      dir,
		tenantID: tenantID,
		builder:  builder,
		meta:     meta,
		uploader: uploader,
	}
}

func (b *testDataBuilder) addStream(labels string, entries ...logproto.Entry) {
	err := b.builder.Append(logproto.Stream{
		Labels:  labels,
		Entries: entries,
	})
	if err == dataobj.ErrBuilderFull {
		b.flush()
		err = b.builder.Append(logproto.Stream{
			Labels:  labels,
			Entries: entries,
		})
	}
	require.NoError(b.t, err)
}

func (b *testDataBuilder) flush() {
	buf := bytes.NewBuffer(make([]byte, 0, 1024*1024))
	stats, err := b.builder.Flush(buf)
	require.NoError(b.t, err)

	// Upload the data object using the uploader
	path, err := b.uploader.Upload(context.Background(), buf)
	require.NoError(b.t, err)

	// Update metastore with the new data object
	err = b.meta.UpdateMetastore(context.Background(), path, stats)
	require.NoError(b.t, err)

	b.builder.Reset()
}
