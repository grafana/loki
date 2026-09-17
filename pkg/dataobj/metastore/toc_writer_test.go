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
	"github.com/grafana/loki/v3/pkg/dataobj/consumer/logsobj"
	"github.com/grafana/loki/v3/pkg/dataobj/index/indexobj"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore/multitenancy"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/indexpointers"
)

func TestTableOfContentsWriter(t *testing.T) {
	t.Run("append new top-level object to new metastore", func(t *testing.T) {
		tenantID := "test"
		tocBuilder, err := indexobj.NewBuilder(logsobj.BuilderBaseConfig{
			TargetPageSize:          tocBuilderCfg.TargetPageSize,
			TargetObjectSize:        tocBuilderCfg.TargetObjectSize,
			TargetSectionSize:       tocBuilderCfg.TargetSectionSize,
			BufferSize:              tocBuilderCfg.BufferSize,
			SectionStripeMergeLimit: tocBuilderCfg.SectionStripeMergeLimit,
		}, nil)
		require.NoError(t, err)

		err = tocBuilder.AppendIndexPointer("test", indexpointers.IndexPointer{Path: "testdata/metastore.obj", StartTs: unixTime(10), EndTs: unixTime(20), FileSize: 0, UncompressedLogsSize: 0})
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

	t.Run("append object whose time range sits exactly on a window boundary", func(t *testing.T) {
		tenantID := "test"
		builder, err := indexobj.NewBuilder(logsobj.BuilderBaseConfig{
			TargetPageSize:          tocBuilderCfg.TargetPageSize,
			TargetObjectSize:        tocBuilderCfg.TargetObjectSize,
			TargetSectionSize:       tocBuilderCfg.TargetSectionSize,
			BufferSize:              tocBuilderCfg.BufferSize,
			SectionStripeMergeLimit: tocBuilderCfg.SectionStripeMergeLimit,
		}, nil)
		require.NoError(t, err)

		bucket := newInMemoryBucket(t, unixTime(0), nil)

		writer := newTableOfContentsWriter(t, bucket, builder)

		// An object holding a single log at a 12h-aligned instant produces a
		// zero-width time range sitting exactly on the ToC window boundary.
		// The pointer must still land in that window; regressing the overlap
		// check leaves the builder empty and WriteEntry retries forever, so we
		// bound the context to fail fast rather than hang.
		boundary := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
		require.Equal(t, boundary, boundary.Truncate(MetastoreWindowSize))

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		err = writer.WriteEntry(ctx, "testdata/metastore.obj", []multitenancy.TimeRange{
			{
				Tenant:  tenantID,
				MinTime: boundary,
				MaxTime: boundary,
			},
		})
		require.NoError(t, err)

		reader, err := bucket.Get(context.Background(), TableOfContentsPath(boundary))
		require.NoError(t, err)
		object, err := io.ReadAll(reader)
		require.NoError(t, err)
		dobj, err := dataobj.FromReaderAt(bytes.NewReader(object), int64(len(object)))
		require.NoError(t, err)
		require.NotEmpty(t, dobj.Sections())
	})

	t.Run("append default to new top-level metastore v1", func(t *testing.T) {
		tenantID := "test"
		builder, err := indexobj.NewBuilder(logsobj.BuilderBaseConfig{
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

		reader, err := bucket.Get(context.Background(), TableOfContentsPath(unixTime(0)))
		require.NoError(t, err)

		object, err := io.ReadAll(reader)
		require.NoError(t, err)

		dobj, err := dataobj.FromReaderAt(bytes.NewReader(object), int64(len(object)))
		require.NoError(t, err)

		err = writer.copyFromExistingToc(context.Background(), dobj)
		require.NoError(t, err)
	})

	t.Run("WriteEntry persists TimeRange sizes", func(t *testing.T) {
		builder, err := indexobj.NewBuilder(tocBuilderCfg, nil)
		require.NoError(t, err)

		bucket := newInMemoryBucket(t, unixTime(0), nil)
		writer := newTableOfContentsWriter(t, bucket, builder)

		const (
			fileSize   = uint64(1024 * 1024)
			uncompSize = uint64(2048 * 1024)
			objectPath = "testdata/metastore.obj"
		)

		require.NoError(t, writer.WriteEntry(context.Background(), objectPath, []multitenancy.TimeRange{
			{
				Tenant:               "test",
				MinTime:              unixTime(10),
				MaxTime:              unixTime(20),
				FileSize:             fileSize,
				UncompressedLogsSize: uncompSize,
			},
		}))

		rows := readToC(context.Background(), t, bucket, TableOfContentsPath(unixTime(0)))
		var found bool
		for _, r := range rows {
			if r.Path == objectPath {
				found = true
				require.Equal(t, fileSize, r.FileSize)
				require.Equal(t, uncompSize, r.UncompressedLogsSize)
			}
		}
		require.True(t, found, "written object row must be present")
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
		path   = TableOfContentsPath(window)
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
