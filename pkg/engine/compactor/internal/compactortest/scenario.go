// Package compactortest provides object-backed compaction integration fixtures.
package compactortest

import (
	"context"
	"flag"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/dataobj"
	dataobjindex "github.com/grafana/loki/v3/pkg/dataobj/index"
	"github.com/grafana/loki/v3/pkg/dataobj/index/indexobj"
	"github.com/grafana/loki/v3/pkg/dataobj/logsobj"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore/multitenancy"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/indexpointers"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/stats"
)

// Source is a built log object and the bucket key to use for it.
type Source struct {
	Path   string
	Object *dataobj.Object
}

// Scenario holds persisted inputs and the bucket shared with a compaction worker.
type Scenario struct {
	Bucket objstore.Bucket
	Window time.Time
}

// Contents describes log objects reachable through the supplied ToC indexes.
type Contents struct {
	StatsObjectPaths    map[string]bool
	PostingsObjectPaths map[string]bool
	SortSchemas         map[string]bool
	Layouts             []logs.SortLayout
	StatsRowCount       int64
	Lines               []string
}

// New seeds a bucket with the supplied sources, one calculated index per source,
// and a ToC containing those indexes. The caller owns the source objects.
func New(ctx context.Context, t *testing.T, window time.Time, sources []Source) *Scenario {
	t.Helper()
	require.Equal(t, window.Truncate(metastore.MetastoreWindowSize), window, "scenario window must be aligned")
	bucket := objstore.NewInMemBucket()
	seen := make(map[string]struct{}, len(sources))
	for _, source := range sources {
		require.NotContains(t, seen, source.Path, "duplicate log object path")
		require.NotNil(t, source.Object)
		seen[source.Path] = struct{}{}
		reader, err := source.Object.Reader(ctx)
		require.NoError(t, err)
		require.NoError(t, bucket.Upload(ctx, source.Path, reader))
		require.NoError(t, reader.Close())
	}

	tocWriter := metastore.NewTableOfContentsWriter(bucket, log.NewNopLogger())
	for i, source := range sources {
		indexPath := IndexPath(i)
		ranges := buildAndUploadIndexFile(ctx, t, bucket, indexPath, source.Path)
		require.NotEmpty(t, ranges)
		attrs, err := bucket.Attributes(ctx, indexPath)
		require.NoError(t, err)
		for j := range ranges {
			ranges[j].FileSize = uint64(attrs.Size)
		}
		require.NoError(t, tocWriter.WriteEntry(ctx, indexPath, ranges))
	}
	return &Scenario{Bucket: bucket, Window: window}
}

// IndexPath returns the ToC index path for the source at position i.
func IndexPath(i int) string { return fmt.Sprintf("indexes/source-%d", i) }

func buildAndUploadIndexFile(ctx context.Context, t *testing.T, bucket objstore.Bucket, indexPath, sourcePath string) []multitenancy.TimeRange {
	t.Helper()

	var cfg logsobj.BuilderBaseConfig
	cfg.RegisterFlagsWithPrefix("", flag.NewFlagSet("scenario-index", flag.PanicOnError))
	require.NoError(t, cfg.TargetPageSize.Set("2KB"))
	require.NoError(t, cfg.TargetSectionSize.Set("4MB"))
	require.NoError(t, cfg.TargetObjectSize.Set("4MB"))
	require.NoError(t, cfg.BufferSize.Set("16KB"))
	builder, err := indexobj.NewBuilder(cfg, nil, indexobj.NewBuilderMetrics(nil))
	require.NoError(t, err)

	calculator := dataobjindex.NewCalculator(builder, dataobjindex.NewCalculatorMetrics(nil))
	obj, err := dataobj.FromBucket(ctx, bucket, sourcePath, 0)
	require.NoError(t, err)
	require.NoError(t, calculator.Calculate(ctx, log.NewNopLogger(), obj, sourcePath))

	indexObj, closer, ranges, err := calculator.Flush()
	require.NoError(t, err)
	defer func() { require.NoError(t, closer.Close()) }()

	reader, err := indexObj.Reader(ctx)
	require.NoError(t, err)
	defer func() { require.NoError(t, reader.Close()) }()

	require.NoError(t, bucket.Upload(ctx, indexPath, reader))
	return ranges
}

// Indexes returns the persisted ToC pointers belonging to tenant, in ToC order.
func (s *Scenario) Indexes(ctx context.Context, t *testing.T, tenant string) []indexpointers.IndexPointer {
	t.Helper()
	obj, err := dataobj.FromBucket(ctx, s.Bucket, metastore.TableOfContentsPath(s.Window), 0)
	require.NoError(t, err)
	var entries []indexpointers.IndexPointer
	for _, section := range obj.Sections().Filter(indexpointers.CheckSection) {
		if section.Tenant != tenant {
			continue
		}
		opened, err := indexpointers.Open(ctx, section)
		require.NoError(t, err)
		reader := indexpointers.NewRowReader(opened)
		require.NoError(t, reader.Open(ctx))
		buf := make([]indexpointers.IndexPointer, 128)
		for {
			n, err := reader.Read(ctx, buf)
			entries = append(entries, buf[:n]...)
			if err == io.EOF {
				break
			}
			require.NoError(t, err)
		}
		require.NoError(t, reader.Close())
	}
	return entries
}

// ReadContents reads stats, postings, and tenant log lines reachable through
// indexes; it does not inspect unrelated objects in the bucket.
func (s *Scenario) ReadContents(ctx context.Context, t *testing.T, tenant string, indexes []indexpointers.IndexPointer) Contents {
	t.Helper()
	contents := Contents{
		StatsObjectPaths:    make(map[string]bool),
		PostingsObjectPaths: make(map[string]bool),
		SortSchemas:         make(map[string]bool),
	}
	for _, index := range indexes {
		indexObj, err := dataobj.FromBucket(ctx, s.Bucket, index.Path, 0)
		require.NoError(t, err)
		for _, section := range indexObj.Sections().Filter(stats.CheckSection) {
			if section.Tenant != tenant {
				continue
			}
			opened, err := stats.Open(ctx, section)
			require.NoError(t, err)
			rows := stats.NewRowReader(ctx, opened)
			for rows.Next() {
				row := rows.At()
				contents.StatsObjectPaths[row.ObjectPath] = true
				contents.SortSchemas[row.SortSchema] = true
				contents.StatsRowCount += row.RowCount
			}
			require.NoError(t, rows.Err())
			require.NoError(t, rows.Close())
		}
		for _, section := range indexObj.Sections().Filter(postings.CheckSection) {
			if section.Tenant != tenant {
				continue
			}
			opened, err := postings.Open(ctx, section)
			require.NoError(t, err)
			inner := postings.NewReader(postings.ReaderOptions{Columns: opened.Columns()})
			require.NoError(t, inner.Open(ctx))
			rows := postings.NewRowReader(ctx, inner)
			for rows.Next() {
				contents.PostingsObjectPaths[rows.At().ObjectPath] = true
			}
			require.NoError(t, rows.Err())
			require.NoError(t, rows.Close())
		}
	}
	for path := range contents.StatsObjectPaths {
		logObj, err := dataobj.FromBucket(ctx, s.Bucket, path, 0)
		require.NoError(t, err)
		for _, section := range logObj.Sections().Filter(logs.CheckSection) {
			if section.Tenant != tenant {
				continue
			}
			opened, err := logs.Open(ctx, section)
			require.NoError(t, err)
			contents.Layouts = append(contents.Layouts, opened.SortLayout())
			for result := range logs.IterSection(ctx, opened) {
				record, err := result.Value()
				require.NoError(t, err)
				contents.Lines = append(contents.Lines, string(record.Line))
			}
		}
	}
	return contents
}
