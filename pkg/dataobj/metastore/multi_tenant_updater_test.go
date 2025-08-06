package metastore

import (
	"bytes"
	"context"
	io "io"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/backoff"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/consumer/logsobj"
	"github.com/grafana/loki/v3/pkg/dataobj/index/indexobj"
	"github.com/grafana/loki/v3/pkg/logproto"
)

func TestMultiTenantUpdater(t *testing.T) {
	t.Run("append new top-level object to deprecated metastore v1", func(t *testing.T) {
		tenantID := "test"
		builder, err := logsobj.NewMultiTenantBuilder(metastoreBuilderCfg)
		require.NoError(t, err)

		ls := labels.New(
			labels.Label{Name: labelNameStart, Value: strconv.FormatInt(unixTime(10).UnixNano(), 10)},
			labels.Label{Name: labelNameEnd, Value: strconv.FormatInt(unixTime(20).UnixNano(), 10)},
			labels.Label{Name: labelNamePath, Value: "testdata/metastore.obj"},
		)

		err = builder.Append(tenantID, logproto.Stream{
			Labels:  ls.String(),
			Entries: []logproto.Entry{{Line: ""}},
		})
		require.NoError(t, err)

		var buf bytes.Buffer
		_, err = builder.Flush(&buf)
		require.NoError(t, err)

		bucket := newMultiTenantInMemoryBucket(t, unixTime(0), &buf)
		builder.Reset()

		updater := newMultiTenantUpdater(t, bucket, builder, nil)
		err = updater.Update(context.Background(), []string{tenantID}, "testdata/metastore.obj", unixTime(20), unixTime(30))
		require.NoError(t, err)
	})

	// t.Run("append new top-level object to new metastore v2", func(t *testing.T) {
	// 	tenantID := "test"
	// 	builder, err := indexobj.NewBuilder(indexobj.BuilderConfig{
	// 		TargetPageSize:          metastoreBuilderCfg.TargetPageSize,
	// 		TargetObjectSize:        metastoreBuilderCfg.TargetObjectSize,
	// 		TargetSectionSize:       metastoreBuilderCfg.TargetSectionSize,
	// 		BufferSize:              metastoreBuilderCfg.BufferSize,
	// 		SectionStripeMergeLimit: metastoreBuilderCfg.SectionStripeMergeLimit,
	// 	})
	// 	require.NoError(t, err)

	// 	err = builder.AppendIndexPointer("testdata/metastore.obj", unixTime(10), unixTime(20))
	// 	require.NoError(t, err)

	// 	var buf bytes.Buffer
	// 	_, err = builder.Flush(&buf)
	// 	require.NoError(t, err)

	// 	bucket := newMultiTenantInMemoryBucket(t, unixTime(0), &buf)
	// 	builder.Reset()

	// 	updater := newMultiTenantUpdater(t, bucket, nil, builder)
	// 	err = updater.Update(context.Background(), []string{tenantID}, "testdata/metastore.obj", unixTime(20), unixTime(30))
	// 	require.NoError(t, err)
	// })

	t.Run("append default to new top-level metastore v1", func(t *testing.T) {
		tenantID := "test"
		builder, err := indexobj.NewBuilder(indexobj.BuilderConfig{
			TargetPageSize:          metastoreBuilderCfg.TargetPageSize,
			TargetObjectSize:        metastoreBuilderCfg.TargetObjectSize,
			TargetSectionSize:       metastoreBuilderCfg.TargetSectionSize,
			BufferSize:              metastoreBuilderCfg.BufferSize,
			SectionStripeMergeLimit: metastoreBuilderCfg.SectionStripeMergeLimit,
		})
		require.NoError(t, err)

		bucket := newMultiTenantInMemoryBucket(t, unixTime(0), nil)

		updater := newMultiTenantUpdater(t, bucket, nil, builder)
		err = updater.Update(context.Background(), []string{tenantID}, "testdata/metastore.obj", unixTime(0), unixTime(30))
		require.NoError(t, err)

		reader, err := bucket.Get(context.Background(), multiTenantMetastorePath(unixTime(0)))
		require.NoError(t, err)

		object, err := io.ReadAll(reader)
		require.NoError(t, err)

		dobj, err := dataobj.FromReaderAt(bytes.NewReader(object), int64(len(object)))
		require.NoError(t, err)

		ty, err := updater.readFromExisting(context.Background(), dobj)
		require.NoError(t, err)
		require.Equal(t, StorageFormatTypeV1, ty)
	})
}

func newMultiTenantUpdater(t *testing.T, bucket objstore.Bucket, v1 *logsobj.MultiTenantBuilder, v2 *indexobj.Builder) *MultiTenantUpdater {
	t.Helper()

	updater := &MultiTenantUpdater{
		builder:          v2,
		metastoreBuilder: v1,
		bucket:           bucket,
		metrics:          newMetastoreMetrics(),
		logger:           log.NewNopLogger(),
		backoff: backoff.New(context.TODO(), backoff.Config{
			MinBackoff: 50 * time.Millisecond,
			MaxBackoff: 10 * time.Second,
		}),
		builderOnce: sync.Once{},
		buf:         bytes.NewBuffer(make([]byte, 0, metastoreBuilderCfg.TargetObjectSize)),
	}

	err := updater.RegisterMetrics(prometheus.NewPedanticRegistry())
	require.NoError(t, err)

	return updater
}

func newMultiTenantInMemoryBucket(t *testing.T, window time.Time, buf *bytes.Buffer) objstore.Bucket {
	t.Helper()

	var (
		bucket = objstore.NewInMemBucket()
		path   = multiTenantMetastorePath(window)
	)

	if buf != nil && buf.Len() > 0 {
		err := bucket.Upload(context.Background(), path, buf)
		require.NoError(t, err)
	}

	return bucket
}
