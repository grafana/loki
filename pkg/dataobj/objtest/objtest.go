// Package objtest builds a data-object storage directory for testing: a bucket holding logs
// objects, the index objects that describe them, and the tables of contents a
// [metastore.Metastore] resolves through.
//
// Reach for objtest when a test needs section resolution to work, because the metastore, the
// section indexes and the stream IDs are all real.
//
// Reach for [github.com/grafana/loki/v3/pkg/dataobj/fixtures] instead when a test only needs one
// object with a hand-laid section and no bucket. That is cheaper and lets the test state every
// row.
package objtest

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/flagext"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"
	"github.com/thanos-io/objstore/providers/filesystem"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/consumer/logsobj"
	"github.com/grafana/loki/v3/pkg/dataobj/index"
	"github.com/grafana/loki/v3/pkg/dataobj/index/indexobj"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/dataobj/uploader"
	"github.com/grafana/loki/v3/pkg/logproto"
)

// Tenant is the tenant [Builder.Append] stores logs for.
const Tenant = "objtest"

// indexPrefix is where index objects and their table of contents live within the bucket.
// metastore.Config's default IndexStoragePrefix matches it, so a metastore built over the
// bucket finds them.
const indexPrefix = "index/v0"

// Option customizes a [Builder].
type Option func(*builderOptions)

type builderOptions struct {
	targetSectionSize flagext.Bytes
}

// WithTargetSectionSize targets the uncompressed data one logs section holds, so a small value
// forces an object to hold several logs sections. Overflow goes to a new section rather than
// being refused, which is why it is a target and not a limit.
//
// Zero keeps the builder's default, which is large enough that a test's worth of logs lands in
// one section.
func WithTargetSectionSize(size flagext.Bytes) Option {
	return func(o *builderOptions) { o.targetSectionSize = size }
}

// Builder is a bucket holding logs data objects and index data objects. Append logs with
// [Builder.Append], then call [Builder.Close] to write the indexes that make them resolvable.
type Builder struct {
	t      *testing.T // Test associated with the store
	dir    string     // Actual directory holding data
	logger log.Logger

	dirty  bool // Whether there's any pending data to flush.
	closed bool // Whether Close has written the indexes.

	builderConfig                       logsobj.BuilderConfig
	uploader                            *uploader.Uploader
	bucket, indexBucket                 objstore.Bucket
	logsBuilder                         *logsobj.Builder
	logsMetastoreToc, indexMetastoreToc *metastore.TableOfContentsWriter
}

// NewBuilder creates a builder that can be used for accumulating logs.
func NewBuilder(t *testing.T, opts ...Option) *Builder {
	var options builderOptions
	for _, opt := range opts {
		opt(&options)
	}

	logger := log.NewNopLogger()

	dir := t.TempDir()

	require.NoError(t, os.MkdirAll(filepath.Join(dir, "tocs"), 0700), "could not create object toc directory")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, indexPrefix, "tocs"), 0700), "could not create index toc directory")

	bucket, err := filesystem.NewBucket(dir)
	require.NoError(t, err, "expected to be able to create bucket")

	var builderConfig logsobj.BuilderConfig
	builderConfig.RegisterFlagsWithPrefix("", flag.NewFlagSet("", flag.PanicOnError)) // Acquire defaults
	if options.targetSectionSize > 0 {
		builderConfig.TargetSectionSize = options.targetSectionSize
	}

	logsBuilder, err := logsobj.NewBuilder(builderConfig, nil, logsobj.NewBuilderMetrics(), log.NewNopLogger(), nil)
	require.NoError(t, err, "expected to be able to create logs builder")

	indexWriterBucket := objstore.NewPrefixedBucket(bucket, indexPrefix)
	logsMetastoreToc := metastore.NewTableOfContentsWriter(bucket, logger)
	indexMetastoreToc := metastore.NewTableOfContentsWriter(indexWriterBucket, logger)

	return &Builder{
		t:      t,
		dir:    dir,
		logger: logger,

		builderConfig:     builderConfig,
		uploader:          uploader.New(uploader.Config{SHAPrefixSize: 2}, bucket, logger),
		bucket:            bucket,
		indexBucket:       indexWriterBucket,
		logsBuilder:       logsBuilder,
		logsMetastoreToc:  logsMetastoreToc,
		indexMetastoreToc: indexMetastoreToc,
	}
}

// Append appends the given streams to the builder for [Tenant].
func (b *Builder) Append(ctx context.Context, streams ...logproto.Stream) {
	b.AppendFor(ctx, Tenant, streams...)
}

// AppendFor appends the given streams to the builder for tenant. Appending for two tenants
// without an intervening [Builder.Flush] puts both tenants' sections in one object.
func (b *Builder) AppendFor(ctx context.Context, tenant string, streams ...logproto.Stream) {
	require.False(b.t, b.closed, "append before Close: logs appended afterwards reach no index, so a query would not see them")

	for _, stream := range streams {
		if b.logsBuilder.IsFull() {
			require.NoError(b.t, b.flush(ctx), "failed to flush logs builder")
		}

		require.NoError(b.t, b.logsBuilder.Append(tenant, stream, time.Now()), "failed to append stream")

		b.dirty = true
	}
}

// Flush writes the buffered logs as one object, so the streams appended after it land in a
// different object. It does nothing when nothing is buffered.
func (b *Builder) Flush(ctx context.Context) {
	require.False(b.t, b.closed, "flush before Close: an object written afterwards reaches no index")
	require.NoError(b.t, b.flush(ctx), "failed to flush logs builder")
}

// flush flushes any pending data in the logs builder and writes a logs
// metastore entry.
func (b *Builder) flush(ctx context.Context) error {
	if !b.dirty {
		// Nothing to do.
		return nil
	}

	timeRanges := b.logsBuilder.TimeRanges()

	obj, closer, err := b.logsBuilder.Flush()
	if err != nil {
		return fmt.Errorf("flushing builder: %w", err)
	}
	defer closer.Close()

	// Upload the logs object.
	path, err := b.uploader.Upload(ctx, obj)
	if err != nil {
		return fmt.Errorf("uploading logs object: %w", err)
	}

	if err := b.logsMetastoreToc.WriteEntry(ctx, path, timeRanges); err != nil {
		return fmt.Errorf("updating metastore: %w", err)
	}

	b.logsBuilder.Reset()
	b.dirty = false
	return nil
}

// Close flushes all remaining data and writes the indexes that make it resolvable. It does
// nothing on a second call: indexing twice would register every section again and a query would
// then count every row twice.
func (b *Builder) Close() {
	if b.closed {
		return
	}
	require.NoError(b.t, b.flush(b.t.Context()), "must be able to flush logs builder")
	require.NoError(b.t, b.buildIndex(b.t.Context()), "must be able to close logs builder")
	b.closed = true
}

func (b *Builder) buildIndex(ctx context.Context) error {
	indexBuilder, err := indexobj.NewBuilder(b.builderConfig.BuilderBaseConfig, nil, indexobj.NewBuilderMetrics(nil))
	if err != nil {
		return fmt.Errorf("creating logs builder: %w", err)
	}

	calculator := index.NewCalculator(indexBuilder, index.NewCalculatorMetrics(nil))

	var (
		count           int
		objectsPerIndex = 16
	)
	err = b.bucket.Iter(ctx, "", func(name string) error {
		if !strings.Contains(name, "objects") {
			return nil
		}

		reader, err := dataobj.FromBucket(ctx, b.bucket, name, 0)
		if err != nil {
			return fmt.Errorf("reading object: %w", err)
		}

		if err := calculator.Calculate(ctx, b.logger, reader, name); err != nil {
			return fmt.Errorf("calculating index: %w", err)
		}

		count++
		if count%objectsPerIndex != 0 {
			// Stop early if we haven't accumulated enough objects yet.
			return nil
		}

		if err := b.flushAndUpload(ctx, calculator); err != nil {
			return fmt.Errorf("flushing and uploading index: %w", err)
		}
		return nil
	}, objstore.WithRecursiveIter())
	if err != nil {
		return fmt.Errorf("iterating over objects: %w", err)
	}

	if count == 0 {
		// Without an index no query resolves anything, which a test would read as an empty
		// result rather than as a missing fixture.
		return fmt.Errorf("no logs object found to index: append logs before calling Close")
	}
	if count%objectsPerIndex != 0 {
		if err := b.flushAndUpload(ctx, calculator); err != nil {
			return fmt.Errorf("failed to flush and upload index: %w", err)
		}
	}
	return nil
}

func (b *Builder) flushAndUpload(ctx context.Context, calculator *index.Calculator) error {
	obj, closer, timeRanges, err := calculator.Flush()
	if err != nil {
		return fmt.Errorf("failed to flush index: %w", err)
	}
	defer closer.Close()

	key, err := index.ObjectKey(ctx, obj)
	if err != nil {
		return fmt.Errorf("failed to create object key: %w", err)
	}

	reader, err := obj.Reader(ctx)
	if err != nil {
		return fmt.Errorf("failed to create reader for index object: %w", err)
	}
	defer reader.Close()

	if err := b.indexBucket.Upload(ctx, key, reader); err != nil {
		return fmt.Errorf("failed to upload index: %w", err)
	} else if err := b.indexMetastoreToc.WriteEntry(ctx, key, timeRanges); err != nil {
		return fmt.Errorf("failed to update metastore: %w", err)
	}

	calculator.Reset()
	return nil
}

// Location holds information about where objects for a [Builder] are stored.
// Location can be used to read data from a builder.
type Location struct {
	Bucket      objstore.Bucket // Bucket where all data is stored.
	IndexPrefix string          // Prefix for index objects in the Bucket.
}

// Location returns the location of index for b so they can be read. Data is not
// guaranteed to exist in the location until calling [Builder.Close].
func (b *Builder) Location() Location {
	return Location{
		Bucket:      b.bucket,
		IndexPrefix: indexPrefix,
	}
}

// Metastore returns a metastore that resolves the builder's objects. Call it after
// [Builder.Close], which writes the indexes it reads.
//
// It reads postings sections. That is the opt-in flow, because ReadPostingsSections defaults to
// off, so a test through this metastore does not cover the default streams-section flow.
func (b *Builder) Metastore() *metastore.ObjectMetastore {
	require.True(b.t, b.closed, "call Close before Metastore: without the indexes it resolves nothing and a query returns an empty result")

	return metastore.NewObjectMetastore(
		b.bucket,
		metastore.Config{IndexStoragePrefix: indexPrefix, ReadPostingsSections: true},
		b.logger,
		metastore.NewObjectMetastoreMetrics(nil),
	)
}
