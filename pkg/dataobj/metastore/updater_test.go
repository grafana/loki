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

func TestUpdater(t *testing.T) {
	t.Run("append new top-level object to deprecated metastore v1", func(t *testing.T) {
		tenantID := "test"
		builder, err := logsobj.NewBuilder(metastoreBuilderCfg)
		require.NoError(t, err)

		ls := labels.New(
			labels.Label{Name: labelNameStart, Value: strconv.FormatInt(unixTime(10).UnixNano(), 10)},
			labels.Label{Name: labelNameEnd, Value: strconv.FormatInt(unixTime(20).UnixNano(), 10)},
			labels.Label{Name: labelNamePath, Value: "testdata/metastore.obj"},
		)

		err = builder.Append(logproto.Stream{
			Labels:  ls.String(),
			Entries: []logproto.Entry{{Line: ""}},
		})
		require.NoError(t, err)

		obj, closer, err := builder.Flush()
		require.NoError(t, err)
		t.Cleanup(func() { closer.Close() })

		bucket := newInMemoryBucket(t, tenantID, unixTime(0), obj)
		builder.Reset()

		updater := newUpdater(t, tenantID, bucket, builder, nil)
		err = updater.Update(context.Background(), "testdata/metastore.obj", unixTime(20), unixTime(30))
		require.NoError(t, err)
	})

	t.Run("append new top-level object to new metastore v2", func(t *testing.T) {
		tenantID := "test"
		builder, err := indexobj.NewBuilder(indexobj.BuilderConfig{
			TargetPageSize:          metastoreBuilderCfg.TargetPageSize,
			TargetObjectSize:        metastoreBuilderCfg.TargetObjectSize,
			TargetSectionSize:       metastoreBuilderCfg.TargetSectionSize,
			BufferSize:              metastoreBuilderCfg.BufferSize,
			SectionStripeMergeLimit: metastoreBuilderCfg.SectionStripeMergeLimit,
		})
		require.NoError(t, err)

		err = builder.AppendIndexPointer("testdata/metastore.obj", unixTime(10), unixTime(20))
		require.NoError(t, err)

		obj, closer, err := builder.Flush()
		require.NoError(t, err)
		t.Cleanup(func() { closer.Close() })

		bucket := newInMemoryBucket(t, tenantID, unixTime(0), obj)
		builder.Reset()

		updater := newUpdater(t, tenantID, bucket, nil, builder)
		err = updater.Update(context.Background(), "testdata/metastore.obj", unixTime(20), unixTime(30))
		require.NoError(t, err)
	})

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

		bucket := newInMemoryBucket(t, tenantID, unixTime(0), nil)

		updater := newUpdater(t, tenantID, bucket, nil, builder)
		err = updater.Update(context.Background(), "testdata/metastore.obj", unixTime(0), unixTime(30))
		require.NoError(t, err)

		reader, err := bucket.Get(context.Background(), metastorePath(tenantID, unixTime(0)))
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

func newUpdater(t *testing.T, tenantID string, bucket objstore.Bucket, v1 *logsobj.Builder, v2 *indexobj.Builder) *Updater {
	t.Helper()

	updater := &Updater{
		builder:          v2,
		metastoreBuilder: v1,
		bucket:           bucket,
		tenantID:         tenantID,
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

func newInMemoryBucket(t *testing.T, tenantID string, window time.Time, obj *dataobj.Object) objstore.Bucket {
	t.Helper()

	var (
		bucket = objstore.NewInMemBucket()
		path   = metastorePath(tenantID, window)
	)

	if obj != nil && obj.Size() > 0 {
		reader, err := obj.Reader(t.Context())
		require.NoError(t, err)
		defer reader.Close()

		require.NoError(t, bucket.Upload(t.Context(), path, reader))
	}

	return bucket
}

func unixTime(sec int64) time.Time {
	return time.Unix(sec, 0).UTC()
}
