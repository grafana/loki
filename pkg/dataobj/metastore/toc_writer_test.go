package metastore

import (
	"bytes"
	"context"
	io "io"
	"sync"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/backoff"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/index/indexobj"
)

func TestTableOfContentsWriter(t *testing.T) {
	t.Run("append new top-level object to new metastore v2", func(t *testing.T) {
		tenantID := "test"
		tocBuilder, err := indexobj.NewBuilder(indexobj.BuilderConfig{
			TargetPageSize:          tocBuilderCfg.TargetPageSize,
			TargetObjectSize:        tocBuilderCfg.TargetObjectSize,
			TargetSectionSize:       tocBuilderCfg.TargetSectionSize,
			BufferSize:              tocBuilderCfg.BufferSize,
			SectionStripeMergeLimit: tocBuilderCfg.SectionStripeMergeLimit,
		})
		require.NoError(t, err)

		err = tocBuilder.AppendIndexPointer("testdata/metastore.obj", unixTime(10), unixTime(20))
		require.NoError(t, err)

		obj, closer, err := tocBuilder.Flush()
		require.NoError(t, err)
		t.Cleanup(func() { closer.Close() })

		bucket := newInMemoryBucket(t, tenantID, unixTime(0), obj)
		tocBuilder.Reset()

		writer := NewTableOfContentsWriter(Config{}, bucket, tenantID, log.NewNopLogger())
		err = writer.WriteEntry(context.Background(), "testdata/metastore.obj", unixTime(20), unixTime(30))
		require.NoError(t, err)
	})

	t.Run("append default to new top-level metastore v1", func(t *testing.T) {
		tenantID := "test"
		builder, err := indexobj.NewBuilder(indexobj.BuilderConfig{
			TargetPageSize:          tocBuilderCfg.TargetPageSize,
			TargetObjectSize:        tocBuilderCfg.TargetObjectSize,
			TargetSectionSize:       tocBuilderCfg.TargetSectionSize,
			BufferSize:              tocBuilderCfg.BufferSize,
			SectionStripeMergeLimit: tocBuilderCfg.SectionStripeMergeLimit,
		}, nil)
		require.NoError(t, err)

		bucket := newInMemoryBucket(t, tenantID, unixTime(0), nil)

		writer := newTableOfContentsWriter(t, tenantID, bucket, builder)
		err = writer.WriteEntry(context.Background(), "testdata/metastore.obj", unixTime(0), unixTime(30))
		require.NoError(t, err)

		reader, err := bucket.Get(context.Background(), tableOfContentsPath(tenantID, unixTime(0), ""))
		require.NoError(t, err)

		object, err := io.ReadAll(reader)
		require.NoError(t, err)

		dobj, err := dataobj.FromReaderAt(bytes.NewReader(object), int64(len(object)))
		require.NoError(t, err)

		err = writer.copyFromExistingToc(context.Background(), dobj)
		require.NoError(t, err)
	})
}

func newTableOfContentsWriter(t *testing.T, tenantID string, bucket objstore.Bucket, tocBuilder *indexobj.Builder) *TableOfContentsWriter {
	t.Helper()

	updater := &TableOfContentsWriter{
		tocBuilder: tocBuilder,
		bucket:     bucket,
		tenantID:   tenantID,
		metrics:    newTableOfContentsMetrics(),
		logger:     log.NewNopLogger(),
		backoff: backoff.New(context.TODO(), backoff.Config{
			MinBackoff: 50 * time.Millisecond,
			MaxBackoff: 10 * time.Second,
		}),
		builderOnce: sync.Once{},
		buf:         bytes.NewBuffer(make([]byte, 0, tocBuilderCfg.TargetObjectSize)),
	}

	err := updater.RegisterMetrics(prometheus.NewPedanticRegistry())
	require.NoError(t, err)

	return updater
}

func newInMemoryBucket(t *testing.T, tenantID string, window time.Time, obj *dataobj.Object) objstore.Bucket {
	t.Helper()

	var (
		bucket = objstore.NewInMemBucket()
		path   = tableOfContentsPath(tenantID, window, "")
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
