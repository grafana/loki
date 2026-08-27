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

func TestDoSortObject_RewritesWholeObjectAndReindexes(t *testing.T) {
	ctx := context.Background()
	dataBucket := objstore.NewInMemBucket()
	indexBucket := objstore.NewInMemBucket()
	const sourcePath = "objects/source"
	targetSchema := []string{"label:app"}
	tenants := []string{"tenant-a", "tenant-b"}

	expectedLines := buildArbitrarilyOrderedObject(t, dataBucket, sourcePath, tenants, targetSchema)

	c := newTestExecutorContext(t, indexBucket)
	c.dataBucket = dataBucket
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
	statsPaths := referencedStatsPaths(t, ctx, indexObj)
	postingsPaths := referencedPostingsPaths(t, ctx, indexObj)
	require.Equal(t, statsPaths, postingsPaths)
	require.Len(t, statsPaths, 1, "one source object must remain one output object")

	var outputPath string
	for path := range statsPaths {
		outputPath = path
	}
	require.NotEqual(t, sourcePath, outputPath)

	output, err := dataobj.FromBucket(ctx, dataBucket, outputPath, 0)
	require.NoError(t, err)
	require.ElementsMatch(t, tenants, output.Tenants())

	var actualLines []string
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
				actualLines = append(actualLines, string(record.Line))

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
	require.ElementsMatch(t, expectedLines, actualLines)
}

func buildArbitrarilyOrderedObject(
	t *testing.T,
	bucket objstore.Bucket,
	path string,
	tenants []string,
	targetSchema []string,
) []string {
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
		labels string
		key    streams.SortKey
	}
	var expectedLines []string
	base := time.Date(2026, 8, 27, 0, 0, 0, 0, time.UTC)
	for _, tenant := range tenants {
		var sourceStreams []sourceStream
		for i, app := range []string{"alpha", "bravo", "charlie", "delta"} {
			parsed := labels.FromStrings("app", app, "instance", fmt.Sprintf("%d", i), "legacy", "same")
			schemaKey, err := logsobj.ComputeSchemaKey(parsed, targetSchema)
			require.NoError(t, err)
			sourceStreams = append(sourceStreams, sourceStream{
				labels: parsed.String(),
				key:    streams.NewSortKey(parsed, schemaKey),
			})
		}
		slices.SortFunc(sourceStreams, func(a, b sourceStream) int {
			return streams.CompareSortKey(b.key, a.key)
		})

		for i, stream := range sourceStreams {
			line := fmt.Sprintf("%s/%d/", tenant, i) + strings.Repeat("x", 512)
			expectedLines = append(expectedLines, line)
			ts := base.Add(time.Duration(i) * time.Second)
			require.NoError(t, builder.Append(tenant, logproto.Stream{
				Labels: stream.labels,
				Entries: []push.Entry{{
					Timestamp: ts,
					Line:      line,
				}},
			}, ts))
		}
	}

	object, closer, err := builder.Flush()
	require.NoError(t, err)
	defer closer.Close()
	require.Greater(t, object.Sections().Count(logs.CheckSection), len(tenants))
	require.NoError(t, uploadObjectToBucket(context.Background(), bucket, path, object))
	return expectedLines
}

func referencedStatsPaths(t *testing.T, ctx context.Context, object *dataobj.Object) map[string]bool {
	t.Helper()
	paths := make(map[string]bool)
	for _, section := range object.Sections().Filter(stats.CheckSection) {
		opened, err := stats.Open(ctx, section)
		require.NoError(t, err)
		reader := stats.NewRowReader(ctx, opened)
		for reader.Next() {
			paths[reader.At().ObjectPath] = true
		}
		require.NoError(t, reader.Err())
		require.NoError(t, reader.Close())
	}
	return paths
}

func referencedPostingsPaths(t *testing.T, ctx context.Context, object *dataobj.Object) map[string]bool {
	t.Helper()
	paths := make(map[string]bool)
	for _, section := range object.Sections().Filter(postings.CheckSection) {
		opened, err := postings.Open(ctx, section)
		require.NoError(t, err)
		inner := postings.NewReader(postings.ReaderOptions{Columns: opened.Columns()})
		require.NoError(t, inner.Open(ctx))
		reader := postings.NewRowReader(ctx, inner)
		for reader.Next() {
			paths[reader.At().ObjectPath] = true
		}
		require.NoError(t, reader.Err())
		require.NoError(t, reader.Close())
	}
	return paths
}
