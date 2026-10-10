// Package compactortest provides object-backed compaction integration fixtures.
package compactortest

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
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
	LogSectionLayouts   []logs.SortLayout
	StatsRowCount       int64
	Lines               []string
}

// New seeds a bucket with the supplied sources, one calculated index per source,
// and a ToC containing those indexes. The caller owns the source objects.
func New(ctx context.Context, t *testing.T, window time.Time, sources []Source) *Scenario {
	t.Helper()
	require.Equal(t, window.Truncate(metastore.MetastoreWindowSize), window, "scenario window must be aligned")
	bucket := objstore.NewInMemBucket()
	tocWriter := metastore.NewTableOfContentsWriter(bucket, log.NewNopLogger())
	seen := make(map[string]struct{}, len(sources))
	for i, source := range sources {
		require.NotContains(t, seen, source.Path, "duplicate log object path")
		require.NotNil(t, source.Object)
		seen[source.Path] = struct{}{}
		reader, err := source.Object.Reader(ctx)
		require.NoError(t, err)
		require.NoError(t, errors.Join(bucket.Upload(ctx, source.Path, reader), reader.Close()))

		seedSourceIndex(ctx, t, bucket, tocWriter, IndexPath(i), source.Path)
	}
	return &Scenario{Bucket: bucket, Window: window}
}

// IndexPath returns the ToC index path for the source at position i.
func IndexPath(i int) string { return fmt.Sprintf("indexes/source-%d", i) }

func seedSourceIndex(ctx context.Context, t *testing.T, bucket objstore.Bucket, tocWriter *metastore.TableOfContentsWriter, indexPath, sourcePath string) {
	t.Helper()

	cfg := logsobj.BuilderBaseConfig{
		TargetPageSize: 2 * 1024, TargetSectionSize: 4 * 1024 * 1024,
		TargetObjectSize: 4 * 1024 * 1024, BufferSize: 16 * 1024,
		MaxPageRows: 10000, SectionStripeMergeLimit: 2, EstimatedCompressionRatio: 8,
	}
	obj, err := dataobj.FromBucket(ctx, bucket, sourcePath, 0)
	require.NoError(t, err)
	tenant, err := obj.Tenant()
	require.NoError(t, err)

	builder, err := indexobj.NewBuilder(tenant, cfg, nil, indexobj.NewBuilderMetrics(nil))
	require.NoError(t, err)

	calculator := dataobjindex.NewCalculator(builder, dataobjindex.NewCalculatorMetrics(nil))
	require.NoError(t, calculator.Calculate(ctx, log.NewNopLogger(), obj, sourcePath))

	indexObj, closer, timeRange, err := calculator.Flush()
	require.NoError(t, err)
	defer closeFixture(t, closer)

	reader, err := indexObj.Reader(ctx)
	require.NoError(t, err)
	defer closeFixture(t, reader)

	require.NoError(t, bucket.Upload(ctx, indexPath, reader))
	require.NoError(t, tocWriter.WriteEntry(ctx, timeRange.Tenant, metastore.TableOfContentsEntry{
		Path:      indexPath,
		StartTime: timeRange.MinTime,
		EndTime:   timeRange.MaxTime,
	}))
}

func closeFixture(t *testing.T, closer io.Closer) {
	t.Helper()
	require.NoError(t, closer.Close())
}

// Indexes returns the persisted ToC pointers belonging to tenant, in ToC order.
func (s *Scenario) Indexes(ctx context.Context, t *testing.T, tenant string) []indexpointers.IndexPointer {
	t.Helper()
	sections := s.tenantSections(ctx, t, metastore.TableOfContentsPath(tenant, s.Window), tenant)
	var entries []indexpointers.IndexPointer
	for _, section := range sections.Filter(indexpointers.CheckSection) {
		opened, err := indexpointers.Open(ctx, section)
		require.NoError(t, err)
		for result := range indexpointers.IterSection(ctx, opened) {
			entry, err := result.Value()
			require.NoError(t, err)
			entries = append(entries, entry)
		}
	}
	return entries
}

// ReadReachableContents reads stats, postings, log lines & sort layouts reachable through
// indexes for a given tenant; it does not inspect unrelated objects in the bucket.
func (s *Scenario) ReadReachableContents(ctx context.Context, t *testing.T, tenant string) Contents {
	t.Helper()
	contents := Contents{
		StatsObjectPaths:    make(map[string]bool),
		PostingsObjectPaths: make(map[string]bool),
		SortSchemas:         make(map[string]bool),
	}
	for _, index := range s.Indexes(ctx, t, tenant) {
		sections := s.tenantSections(ctx, t, index.Path, tenant)
		for _, section := range sections.Filter(stats.CheckSection) {
			opened, err := stats.Open(ctx, section)
			require.NoError(t, err)
			rows := stats.NewRowReader(ctx, opened)
			for rows.Next() {
				row := rows.At()
				contents.StatsObjectPaths[row.ObjectPath] = true
				contents.SortSchemas[row.SortSchema] = true
				contents.StatsRowCount += row.RowCount
			}
			require.NoError(t, errors.Join(rows.Err(), rows.Close()))
		}
		for _, section := range sections.Filter(postings.CheckSection) {
			opened, err := postings.Open(ctx, section)
			require.NoError(t, err)
			inner := postings.NewReader(postings.ReaderOptions{Columns: opened.Columns()})
			require.NoError(t, inner.Open(ctx))
			rows := postings.NewRowReader(ctx, inner)
			for rows.Next() {
				contents.PostingsObjectPaths[rows.At().ObjectPath] = true
			}
			require.NoError(t, errors.Join(rows.Err(), rows.Close()))
		}
	}
	for path := range contents.StatsObjectPaths {
		sections := s.tenantSections(ctx, t, path, tenant)
		for _, section := range sections.Filter(logs.CheckSection) {
			opened, err := logs.Open(ctx, section)
			require.NoError(t, err)
			contents.LogSectionLayouts = append(contents.LogSectionLayouts, opened.SortLayout())
			for result := range logs.IterSection(ctx, opened) {
				record, err := result.Value()
				require.NoError(t, err)
				contents.Lines = append(contents.Lines, string(record.Line))
			}
		}
	}
	return contents
}

func (s *Scenario) tenantSections(ctx context.Context, t *testing.T, path, tenant string) dataobj.Sections {
	t.Helper()
	obj, err := dataobj.FromBucket(ctx, s.Bucket, path, 0)
	require.NoError(t, err)
	return slices.DeleteFunc(slices.Clone(obj.Sections()), func(section *dataobj.Section) bool {
		return section.Tenant != tenant
	})
}
