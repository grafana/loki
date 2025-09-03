package metastore

import (
	"bytes"
	"context"
	io "io"
	"sync"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/index/indexobj"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore/multitenancy"
)

func TestTableOfContentsWriter(t *testing.T) {
	t.Run("append new top-level object to new metastore", func(t *testing.T) {
		tenantID := "test"
		tocBuilder, err := indexobj.NewBuilder(indexobj.BuilderConfig{
			TargetPageSize:          tocBuilderCfg.TargetPageSize,
			TargetObjectSize:        tocBuilderCfg.TargetObjectSize,
			TargetSectionSize:       tocBuilderCfg.TargetSectionSize,
			BufferSize:              tocBuilderCfg.BufferSize,
			SectionStripeMergeLimit: tocBuilderCfg.SectionStripeMergeLimit,
		}, nil)
		require.NoError(t, err)

		err = tocBuilder.AppendIndexPointer("testdata/metastore.obj", "test", unixTime(10), unixTime(20))
		require.NoError(t, err)

		obj, closer, err := tocBuilder.Flush()
		require.NoError(t, err)
		t.Cleanup(func() { closer.Close() })

		bucket := newInMemoryBucket(t, unixTime(0), obj)
		tocBuilder.Reset()

		writer := NewTableOfContentsWriter(bucket, log.NewNopLogger())
		err = writer.WriteEntry(context.Background(), "testdata/metastore.obj", []multitenancy.TimeRange{
			{
				Tenant:  tenantID,
				MinTime: unixTime(20),
				MaxTime: unixTime(30),
			},
		})
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

		bucket := newInMemoryBucket(t, unixTime(0), nil)

		writer := newTableOfContentsWriter(t, bucket, builder)
		err = writer.WriteEntry(context.Background(), "testdata/metastore.obj", []multitenancy.TimeRange{
			{
				Tenant:  tenantID,
				MinTime: unixTime(0),
				MaxTime: unixTime(30),
			},
		})
		require.NoError(t, err)

		reader, err := bucket.Get(context.Background(), tableOfContentsPath(unixTime(0)))
		require.NoError(t, err)

		object, err := io.ReadAll(reader)
		require.NoError(t, err)

		dobj, err := dataobj.FromReaderAt(bytes.NewReader(object), int64(len(object)))
		require.NoError(t, err)

		err = writer.copyFromExistingToc(context.Background(), dobj)
		require.NoError(t, err)
	})
}

func newTableOfContentsWriter(t *testing.T, bucket objstore.Bucket, tocBuilder *indexobj.Builder) *TableOfContentsWriter {
	t.Helper()

	updater := &TableOfContentsWriter{
		tocBuilder:  tocBuilder,
		bucket:      bucket,
		metrics:     newTableOfContentsMetrics(),
		logger:      log.NewNopLogger(),
		builderOnce: sync.Once{},
		buf:         bytes.NewBuffer(make([]byte, 0, tocBuilderCfg.TargetObjectSize)),
	}

	err := updater.RegisterMetrics(prometheus.NewPedanticRegistry())
	require.NoError(t, err)

	return updater
}

func newInMemoryBucket(t *testing.T, window time.Time, obj *dataobj.Object) objstore.Bucket {
	t.Helper()

	var (
		bucket = objstore.NewInMemBucket()
		path   = tableOfContentsPath(window)
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
