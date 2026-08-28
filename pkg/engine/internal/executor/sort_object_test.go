package executor

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/pkg/push"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/consumer/logsobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/stats"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/scratch"
)

type recordFixture struct {
	timestamp int64
	metadata  string
}

type streamFixture struct {
	minTimestamp     time.Time
	maxTimestamp     time.Time
	rows             int
	uncompressedSize int64
}

type sortObjectFixture struct {
	records map[string]recordFixture
	streams map[string]streamFixture
}

func TestDoSortObject_RewritesWholeObjectAndReindexes(t *testing.T) {
	ctx := context.Background()
	dataBucket := objstore.NewInMemBucket()
	indexBucket := objstore.NewInMemBucket()
	const sourcePath = "objects/source"
	targetSchema := []string{"label:app"}
	tenants := []string{"tenant-a", "tenant-b"}

	expected := buildUnorderedObject(t, dataBucket, sourcePath, tenants, targetSchema)

	c := newTestExecutorContext(t, indexBucket)
	c.dataBucket = dataBucket
	c.logsobjCfg.TargetPageSize = 512
	c.logsobjCfg.TargetObjectSize = 2048 // Output is larger, but SortObject must not split it.
	c.logsobjCfg.TargetSectionSize = 1500
	c.logsobjCfg.BufferSize = 700 // Force several independently sorted runs.
	artifacts, err := c.doSortObject(ctx, &physical.SortObject{
		SourceObjectPath: sourcePath,
		SortSchema:       targetSchema,
	})
	require.NoError(t, err)
	require.Len(t, artifacts, 1)

	indexObj, err := dataobj.FromBucket(ctx, indexBucket, artifacts[0].Path, 0)
	require.NoError(t, err)
	require.ElementsMatch(t, tenants, indexObj.Tenants(), "the replacement index must cover every source tenant")

	statsRows := readSortObjectStats(ctx, t, indexObj)
	statsPaths := make(map[string]bool)
	for _, stat := range statsRows {
		statsPaths[stat.row.ObjectPath] = true
	}

	postingsRows := readSortObjectPostings(ctx, t, indexObj)
	postingsPaths := make(map[string]bool)
	for _, posting := range postingsRows {
		postingsPaths[posting.row.ObjectPath] = true
	}
	require.Equal(t, statsPaths, postingsPaths)
	require.Len(t, statsPaths, 1, "one source object must remain one output object")

	var outputPath string
	for path := range statsPaths {
		outputPath = path
	}
	require.NotEqual(t, sourcePath, outputPath)
	assertSortObjectIndexContents(t, statsRows, postingsRows, outputPath, tenants, expected)

	output, err := dataobj.FromBucket(ctx, dataBucket, outputPath, 0)
	require.NoError(t, err)
	require.ElementsMatch(t, tenants, output.Tenants())

	actualRecords := make(map[string]recordFixture)
	for _, tenant := range tenants {
		streamLabels := make(map[int64]labels.Labels)
		for _, section := range output.Sections().Filter(func(section *dataobj.Section) bool {
			return streams.CheckSection(section) && section.Tenant == tenant
		}) {
			opened, err := streams.Open(ctx, section)
			require.NoError(t, err)
			for result := range streams.IterSection(ctx, opened) {
				stream, err := result.Value()
				require.NoError(t, err)
				streamLabels[stream.ID] = stream.Labels.Copy()
				expectedStream := expected.streams[tenant+"/"+stream.Labels.Get("app")]
				require.True(t, stream.MinTimestamp.Equal(expectedStream.minTimestamp))
				require.True(t, stream.MaxTimestamp.Equal(expectedStream.maxTimestamp))
				require.Equal(t, expectedStream.rows, stream.Rows)
				require.Equal(t, expectedStream.uncompressedSize, stream.UncompressedSize)
			}
		}

		var previous streams.SortKey
		var previousStreamID int64
		var previousTimestamp time.Time
		havePrevious := false
		for _, section := range output.Sections().Filter(func(section *dataobj.Section) bool {
			return logs.CheckSection(section) && section.Tenant == tenant
		}) {
			opened, err := logs.Open(ctx, section)
			require.NoError(t, err)
			require.Equal(t, logs.SortLayout{
				SchemaLabels: targetSchema,
				StreamOrder:  logs.StreamOrderStableHashV1,
				ShardCount:   streams.ShardFactor,
			}, opened.SortLayout())

			for result := range logs.IterSection(ctx, opened) {
				record, err := result.Value()
				require.NoError(t, err)
				line := string(record.Line)
				actualRecords[line] = recordFixture{
					timestamp: record.Timestamp.UnixNano(),
					metadata:  record.Metadata.Get("sequence"),
				}

				schemaKey, err := logsobj.ComputeSchemaKey(streamLabels[record.StreamID], targetSchema)
				require.NoError(t, err)
				key := streams.NewSortKey(streamLabels[record.StreamID], schemaKey)
				if havePrevious {
					comparison := streams.CompareSortKey(previous, key)
					require.LessOrEqual(t, comparison, 0)
					if comparison == 0 {
						require.LessOrEqual(t, previousStreamID, record.StreamID)
						if previousStreamID == record.StreamID {
							require.False(t, record.Timestamp.After(previousTimestamp))
						}
					}
				}
				previous = key
				previousStreamID = record.StreamID
				previousTimestamp = record.Timestamp
				havePrevious = true
			}
		}
	}
	require.Equal(t, expected.records, actualRecords)
}

func buildUnorderedObject(
	t *testing.T,
	bucket objstore.Bucket,
	path string,
	tenants []string,
	targetSchema []string,
) sortObjectFixture {
	t.Helper()
	cfg := logsobj.BuilderConfig{
		BuilderBaseConfig: logsobj.BuilderBaseConfig{
			TargetPageSize:          512,
			MaxPageRows:             100,
			TargetObjectSize:        1 << 20,
			TargetSectionSize:       600,
			BufferSize:              256,
			SectionStripeMergeLimit: 2,
		},
		AppendOrderedEnabled: true,
	}
	builder, err := logsobj.NewBuilder(
		cfg,
		scratch.NewMemory(),
		logsobj.NewBuilderMetrics(),
		log.NewNopLogger(),
		sortSchemaOverrides([]string{"label:legacy"}),
	)
	require.NoError(t, err)

	type sourceStream struct {
		app    string
		labels string
		key    streams.SortKey
	}
	expected := sortObjectFixture{
		records: make(map[string]recordFixture),
		streams: make(map[string]streamFixture),
	}
	base := time.Date(2026, 8, 27, 0, 0, 0, 0, time.UTC)
	for _, tenant := range tenants {
		var sourceStreams []sourceStream
		for i, app := range []string{"alpha", "bravo", "charlie", "delta"} {
			parsed := labels.FromStrings("app", app, "instance", fmt.Sprintf("%d", i), "legacy", "same")
			schemaKey, err := logsobj.ComputeSchemaKey(parsed, targetSchema)
			require.NoError(t, err)
			sourceStreams = append(sourceStreams, sourceStream{
				app:    app,
				labels: parsed.String(),
				key:    streams.NewSortKey(parsed, schemaKey),
			})
		}
		slices.SortFunc(sourceStreams, func(a, b sourceStream) int {
			return streams.CompareSortKey(b.key, a.key)
		})

		for i, stream := range sourceStreams {
			streamBase := base.Add(time.Duration(i) * time.Minute)
			var entries []push.Entry
			var uncompressedSize int64
			for sequence, offset := range []time.Duration{2 * time.Second, 0, time.Second} {
				line := fmt.Sprintf("%s/%s/%d/", tenant, stream.app, sequence) + strings.Repeat("x", 512)
				metadata := fmt.Sprintf("metadata-%s-%s-%d", tenant, stream.app, sequence)
				timestamp := streamBase.Add(offset)
				entries = append(entries, push.Entry{
					Timestamp:          timestamp,
					Line:               line,
					StructuredMetadata: []push.LabelAdapter{{Name: "sequence", Value: metadata}},
				})
				expected.records[line] = recordFixture{
					timestamp: timestamp.UnixNano(),
					metadata:  metadata,
				}
				uncompressedSize += int64(len(line) + len(metadata))
			}
			expected.streams[tenant+"/"+stream.app] = streamFixture{
				minTimestamp:     streamBase,
				maxTimestamp:     streamBase.Add(2 * time.Second),
				rows:             len(entries),
				uncompressedSize: uncompressedSize,
			}
			require.NoError(t, builder.Append(tenant, logproto.Stream{
				Labels:  stream.labels,
				Entries: entries,
			}, streamBase))
		}
	}

	object, closer, err := builder.Flush()
	require.NoError(t, err)
	defer closer.Close()
	require.Greater(t, object.Sections().Count(logs.CheckSection), len(tenants))
	require.NoError(t, uploadObjectToBucket(context.Background(), bucket, path, object))
	return expected
}

type tenantStat struct {
	tenant string
	row    stats.Stat
}

type tenantPosting struct {
	tenant string
	row    postings.Row
}

func readSortObjectStats(ctx context.Context, t *testing.T, object *dataobj.Object) []tenantStat {
	t.Helper()
	var rows []tenantStat
	for _, section := range object.Sections().Filter(stats.CheckSection) {
		opened, err := stats.Open(ctx, section)
		require.NoError(t, err)
		reader := stats.NewRowReader(ctx, opened)
		for reader.Next() {
			rows = append(rows, tenantStat{tenant: section.Tenant, row: reader.At()})
		}
		require.NoError(t, reader.Err())
		require.NoError(t, reader.Close())
	}
	return rows
}

func readSortObjectPostings(ctx context.Context, t *testing.T, object *dataobj.Object) []tenantPosting {
	t.Helper()
	var rows []tenantPosting
	for _, section := range object.Sections().Filter(postings.CheckSection) {
		opened, err := postings.Open(ctx, section)
		require.NoError(t, err)
		inner := postings.NewReader(postings.ReaderOptions{Columns: opened.Columns()})
		require.NoError(t, inner.Open(ctx))
		reader := postings.NewRowReader(ctx, inner)
		for reader.Next() {
			rows = append(rows, tenantPosting{tenant: section.Tenant, row: reader.At()})
		}
		require.NoError(t, reader.Err())
		require.NoError(t, reader.Close())
	}
	return rows
}

func assertSortObjectIndexContents(
	t *testing.T,
	statsRows []tenantStat,
	postingsRows []tenantPosting,
	outputPath string,
	tenants []string,
	expected sortObjectFixture,
) {
	t.Helper()
	expectedApps := map[string]bool{"alpha": true, "bravo": true, "charlie": true, "delta": true}
	for _, tenant := range tenants {
		var (
			rowCount         int64
			uncompressedSize int64
			apps             = make(map[string]bool)
			postingApps      = make(map[string]bool)
			minTimestamp     int64
			maxTimestamp     int64
			postingsMin      int64
			postingsMax      int64
			haveStats        bool
			havePostings     bool
		)
		// Extract info about this object from stats
		for _, stat := range statsRows {
			if stat.tenant != tenant {
				continue
			}
			require.Equal(t, outputPath, stat.row.ObjectPath)
			require.Equal(t, "label:app", stat.row.SortSchema)
			require.LessOrEqual(t, stat.row.MinTimestamp, stat.row.MaxTimestamp)
			rowCount += stat.row.RowCount
			uncompressedSize += stat.row.UncompressedSize
			apps[stat.row.Labels["app"]] = true
			if !haveStats || stat.row.MinTimestamp < minTimestamp {
				minTimestamp = stat.row.MinTimestamp
			}
			if !haveStats || stat.row.MaxTimestamp > maxTimestamp {
				maxTimestamp = stat.row.MaxTimestamp
			}
			haveStats = true
		}

		// Extract info about the streams from postings
		for _, posting := range postingsRows {
			if posting.tenant != tenant {
				continue
			}
			require.Equal(t, outputPath, posting.row.ObjectPath)
			require.LessOrEqual(t, posting.row.MinTimestamp, posting.row.MaxTimestamp)
			if !havePostings || posting.row.MinTimestamp < postingsMin {
				postingsMin = posting.row.MinTimestamp
			}
			if !havePostings || posting.row.MaxTimestamp > postingsMax {
				postingsMax = posting.row.MaxTimestamp
			}
			havePostings = true
			if posting.row.Kind == postings.KindLabel && posting.row.ColumnName == "app" {
				postingApps[posting.row.LabelValue] = true
			}
		}

		// Calculate expected timestamps from the fixtures
		var expectedSize int64
		var expectedMin, expectedMax time.Time
		for app := range expectedApps {
			stream := expected.streams[tenant+"/"+app]
			expectedSize += stream.uncompressedSize
			if expectedMin.IsZero() || stream.minTimestamp.Before(expectedMin) {
				expectedMin = stream.minTimestamp
			}
			if expectedMax.IsZero() || stream.maxTimestamp.After(expectedMax) {
				expectedMax = stream.maxTimestamp
			}
		}

		// Assert there are valid values
		require.True(t, haveStats)
		require.True(t, havePostings)
		require.Equal(t, int64(len(expectedApps)*3), rowCount)
		require.Equal(t, expectedSize, uncompressedSize)
		require.Equal(t, expectedMin.UnixNano(), minTimestamp)
		require.Equal(t, expectedMax.UnixNano(), maxTimestamp)
		require.Equal(t, expectedMin.UnixNano(), postingsMin)
		require.Equal(t, expectedMax.UnixNano(), postingsMax)
		require.Equal(t, expectedApps, apps)
		require.Equal(t, expectedApps, postingApps)
	}
}
