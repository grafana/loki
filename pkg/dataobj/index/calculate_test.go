package index

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/thanos-io/objstore/providers/filesystem"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/consumer/logsobj"
	"github.com/grafana/loki/v3/pkg/dataobj/fixtures"
	"github.com/grafana/loki/v3/pkg/dataobj/index/indexobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/pointers"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/stats"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
	"github.com/grafana/loki/v3/pkg/logproto"

	"github.com/grafana/loki/pkg/push"
)

// Used to expose SortSchemaLabels to logs builders
type fakeLimits struct{}

func (fakeLimits) CompactionPhases(_ string) (runIndex, runLog bool) {
	return true, true
}
func (fakeLimits) SortSchemaLabels(string) []string {
	return fakeSchema
}

var testCalculatorConfig = logsobj.BuilderBaseConfig{
	TargetPageSize:          2048,
	TargetObjectSize:        1 << 22, // 4 MiB
	BufferSize:              2048 * 8,
	SectionStripeMergeLimit: 2,

	// This is set low because Pointers & Streams sections ignore section size. There must be a single pointers section per tenant to maintain state.
	TargetSectionSize: 1,
}

// createTestLogObject creates a test data object with both streams and logs sections
func createTestLogObject(t *testing.T, tenants int) *dataobj.Object {
	t.Helper()

	lr := fixtures.NewLogsFixtureBuilder(t)
	lr.ForStream(`{cluster="test",app="foo",env="prod"}`).
		Entry(10, `{trace_id="123", span_id="456"}`, "hello from foo").
		Entry(15, `{trace_id="789"}`, "another message from foo")
	lr.ForStream(`{cluster="test",app="bar",env="dev"}`).
		Entry(20, `{trace_id="abc",user_id="user123"}`, "hello from bar").
		Entry(25, `{trace_id="def",level="error"}`, "error message from bar")

	var allTenantSections []dataobj.SectionBuilder
	for i := range tenants {
		tenant := fmt.Sprintf("tenant-%d", i)
		allTenantSections = append(allTenantSections,
			fixtures.LogsSection(t, tenant, lr.Logs()),
			fixtures.StreamsSection(t, tenant, lr.Streams()),
		)
	}
	obj, closer := fixtures.DataObject(t, allTenantSections...)
	t.Cleanup(func() { closer.Close() })

	// Validate
	streamSections := obj.Sections().Count(streams.CheckSection)
	require.Equal(t, tenants, streamSections)

	logSections := obj.Sections().Count(logs.CheckSection)
	require.Equal(t, tenants, logSections)

	return obj
}

func TestCalculator_Calculate_StatsShardBuckets(t *testing.T) {
	// Two streams share service_name (the default stats grouping key) but
	// land in different shard buckets. Calculate must populate
	// streamShardBuckets from labels; a missing or zeroed map would either
	// error or collapse these into one row.
	first := labels.FromStrings("service_name", "api", "instance", "0")
	firstShard := streams.ShardBucket(first)
	var second labels.Labels
	for i := 1; i < 256; i++ {
		candidate := labels.FromStrings("service_name", "api", "instance", strconv.Itoa(i))
		if streams.ShardBucket(candidate) != firstShard {
			second = candidate
			break
		}
	}
	require.NotEmpty(t, second)

	logBuilder, err := logsobj.NewBuilder(logsobj.BuilderConfig{
		BuilderBaseConfig: logsobj.BuilderBaseConfig{
			TargetPageSize:          2048,
			TargetObjectSize:        1 << 22,
			TargetSectionSize:       1 << 21,
			BufferSize:              2048 * 8,
			SectionStripeMergeLimit: 2,
		},
	}, nil, logsobj.NewBuilderMetrics(), log.NewNopLogger(), fakeLimits{})
	require.NoError(t, err)

	ts := time.Unix(10, 0).UTC()
	for _, lbls := range []labels.Labels{first, second} {
		require.NoError(t, logBuilder.Append("tenant-1", logproto.Stream{
			Labels: lbls.String(),
			Entries: []push.Entry{{
				Timestamp: ts,
				Line:      "hello",
			}},
		}, ts))
	}

	logObj, logCloser, err := logBuilder.Flush()
	require.NoError(t, err)
	t.Cleanup(func() { _ = logCloser.Close() })

	indexBuilder, err := indexobj.NewBuilder(testCalculatorConfig, nil)
	require.NoError(t, err)
	calculator := NewCalculator(indexBuilder)
	require.NoError(t, calculator.Calculate(context.Background(), log.NewNopLogger(), logObj, "test/path/obj1"))

	indexObj, indexCloser, _, err := calculator.Flush()
	require.NoError(t, err)
	t.Cleanup(func() { _ = indexCloser.Close() })

	rows := readAllStatsRows(t, indexObj)
	require.Len(t, rows, 2)
	gotShards := make(map[int64]struct{}, len(rows))
	for _, row := range rows {
		require.Equal(t, "api", row["service_name.label.utf8"])
		gotShards[row["__shard_bucket__.int64"].(int64)] = struct{}{}
	}
	require.Equal(t, map[int64]struct{}{
		int64(firstShard):                  {},
		int64(streams.ShardBucket(second)): {},
	}, gotShards)
}

func TestCalculator_Calculate(t *testing.T) {
	logger := log.NewNopLogger()
	tenants := 4
	objects := 10

	t.Run("successful calculation from readerAt", func(t *testing.T) {
		indexBuilder, err := indexobj.NewBuilder(testCalculatorConfig, nil)
		require.NoError(t, err)

		calculator := NewCalculator(indexBuilder)
		for i := 0; i < objects; i++ {
			obj := createTestLogObject(t, tenants)

			path := fmt.Sprintf("test/path-%d", i)
			err = calculator.Calculate(context.Background(), logger, obj, path)
			require.NoError(t, err)
		}

		// Verify we can flush the results
		obj, closer, timeRanges, err := calculator.Flush()
		require.NoError(t, err)
		defer closer.Close()

		require.Greater(t, obj.Size(), int64(0))
		require.Equal(t, len(timeRanges), tenants)
		for _, timeRange := range timeRanges {
			require.NotEmpty(t, timeRange.Tenant)
			require.Equal(t, time.Unix(10, 0).UTC(), timeRange.MinTime)
			require.Equal(t, time.Unix(25, 0).UTC(), timeRange.MaxTime)
		}

		// Confirm we have multiple pointers sections
		count := obj.Sections().Count(pointers.CheckSection)
		require.GreaterOrEqual(t, count, tenants)

		requireValidPointers(t, obj)
	})

	t.Run("successful calculation from FS bucket", func(t *testing.T) {
		indexBuilder, err := indexobj.NewBuilder(testCalculatorConfig, nil)
		require.NoError(t, err)

		bucket, err := filesystem.NewBucket(t.TempDir())
		require.NoError(t, err)

		calculator := NewCalculator(indexBuilder)
		for i := 0; i < objects; i++ {
			obj := createTestLogObject(t, tenants)

			// Upload to bucket
			reader, err := obj.Reader(context.Background())
			require.NoError(t, err)
			err = bucket.Upload(context.Background(), fmt.Sprintf("obj-%d", i), reader)
			require.NoError(t, err)
			bucketObj, err := dataobj.FromBucket(context.Background(), bucket, fmt.Sprintf("obj-%d", i), 0)
			require.NoError(t, err)

			err = calculator.Calculate(context.Background(), logger, bucketObj, fmt.Sprintf("test/path-%d", i))
			require.NoError(t, err)
		}

		// Verify we can flush the results
		obj, closer, timeRanges, err := calculator.Flush()
		require.NoError(t, err)
		defer closer.Close()

		require.Greater(t, obj.Size(), int64(0))
		require.Equal(t, len(timeRanges), tenants)
		for _, timeRange := range timeRanges {
			require.NotEmpty(t, timeRange.Tenant)
			require.False(t, timeRange.MinTime.IsZero())
			require.Equal(t, timeRange.MinTime, time.Unix(10, 0).UTC())
			require.False(t, timeRange.MaxTime.IsZero())
			require.Equal(t, timeRange.MaxTime, time.Unix(25, 0).UTC())
		}

		// Confirm we have multiple pointers sections
		count := obj.Sections().Count(pointers.CheckSection)
		require.GreaterOrEqual(t, count, tenants)

		requireValidPointers(t, obj)
	})
}

func TestCalculator_Calculate_SectionIndexesCountOnlyLogsAcrossTenants(t *testing.T) {
	ctx := context.Background()
	const path = "objects/section-index-test"
	sourceBuilder := dataobj.NewBuilder(nil)
	// Physical sections: streams A, logs A, logs A, streams B, logs B, logs B.
	// References must be 0,1 for A and 2,3 for B, rather than physical indexes
	// 1,2,4,5 or tenant-local indexes 0,1 for both tenants.
	for _, tenant := range []string{"A", "B"} {
		streamBuilder := streams.NewBuilder(nil, 2048, 10000)
		streamBuilder.SetTenant(tenant)
		ts := time.Unix(10, 0).UTC()
		id := streamBuilder.Record(labels.FromStrings("service_name", tenant), ts, 10)
		require.NoError(t, sourceBuilder.Append(streamBuilder))
		for range 2 {
			logBuilder := logs.NewBuilder(nil, logs.BuilderOptions{
				PageSizeHint: 2048, BufferSize: 2048, StripeMergeLimit: 2, SortOrder: logs.SortStreamASC,
			})
			logBuilder.SetTenant(tenant)
			logBuilder.Append(logs.Record{StreamID: id, Timestamp: ts, Line: []byte("line"), Metadata: labels.FromStrings("trace_id", "trace")})
			require.NoError(t, sourceBuilder.Append(logBuilder))
		}
	}
	source, sourceCloser, err := sourceBuilder.Flush()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sourceCloser.Close()) })
	require.Len(t, source.Sections(), 6)
	for _, i := range []int{0, 3} {
		require.True(t, streams.CheckSection(source.Sections()[i]))
	}
	for _, i := range []int{1, 2, 4, 5} {
		require.True(t, logs.CheckSection(source.Sections()[i]))
	}

	builder, err := indexobj.NewBuilder(testCalculatorConfig, nil)
	require.NoError(t, err)
	calculator := NewCalculator(builder)
	require.NoError(t, calculator.Calculate(ctx, log.NewNopLogger(), source, path))
	obj, closer, _, err := calculator.Flush()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, closer.Close()) })

	want := map[string]map[int64]bool{"A": {0: true, 1: true}, "B": {2: true, 3: true}}
	postingIndexes := map[string]map[int64]bool{}
	for _, section := range obj.Sections().Filter(postings.CheckSection) {
		sec, err := postings.Open(ctx, section)
		require.NoError(t, err)
		inner := postings.NewReader(postings.ReaderOptions{Columns: sec.Columns()})
		require.NoError(t, inner.Open(ctx))
		reader := postings.NewRowReader(ctx, inner)
		for reader.Next() {
			row := reader.At()
			require.Equal(t, path, row.ObjectPath)
			require.True(t, want[section.Tenant][row.SectionIndex], "unexpected posting reference for tenant %s: %d", section.Tenant, row.SectionIndex)
			if postingIndexes[section.Tenant] == nil {
				postingIndexes[section.Tenant] = map[int64]bool{}
			}
			postingIndexes[section.Tenant][row.SectionIndex] = true
		}
		require.NoError(t, reader.Err())
		require.NoError(t, reader.Close())
	}
	require.Equal(t, want, postingIndexes)

	statsIndexes := map[string]map[int64]bool{}
	for _, section := range obj.Sections().Filter(stats.CheckSection) {
		sec, err := stats.Open(ctx, section)
		require.NoError(t, err)
		reader := stats.NewRowReader(ctx, sec)
		for reader.Next() {
			row := reader.At()
			require.Equal(t, path, row.ObjectPath)
			if statsIndexes[section.Tenant] == nil {
				statsIndexes[section.Tenant] = map[int64]bool{}
			}
			statsIndexes[section.Tenant][row.SectionIndex] = true
		}
		require.NoError(t, reader.Err())
		require.NoError(t, reader.Close())
	}
	require.Equal(t, want, statsIndexes)
}

func requireValidPointers(t *testing.T, obj *dataobj.Object) {
	totalPointers := 0
	pointersByTenant := make(map[string]int)
	for _, section := range obj.Sections().Filter(pointers.CheckSection) {
		require.NotEmpty(t, section.Tenant)

		sec, err := pointers.Open(context.Background(), section)
		require.NoError(t, err)

		reader := pointers.NewRowReader(sec)
		require.NoError(t, reader.Open(context.Background()))

		buf := make([]pointers.SectionPointer, 1024)
		for {
			n, err := reader.Read(context.Background(), buf)
			if !errors.Is(err, io.EOF) {
				require.NoError(t, err)
			}
			if n == 0 && errors.Is(err, io.EOF) {
				break
			}
			for _, pointer := range buf[:n] {
				require.NotEqual(t, pointer.Path, "")
				require.Greater(t, pointer.PointerKind, pointers.PointerKind(0))
				if pointer.PointerKind == pointers.PointerKindStreamIndex {
					key := fmt.Sprintf("%s:%s:%d", section.Tenant, pointer.Path, pointer.Section)
					pointersByTenant[key]++
					require.Greater(t, pointer.StreamIDRef, int64(0))
					require.Greater(t, pointer.StreamID, int64(0))
					require.Greater(t, pointer.StartTs, time.Unix(0, 0))
					require.Greater(t, pointer.EndTs, time.Unix(0, 0))
					require.Greater(t, pointer.LineCount, int64(0))
					require.Greater(t, pointer.UncompressedSize, int64(0))
				} else {
					require.Greater(t, pointer.ColumnIndex, int64(0))
					require.Greater(t, len(pointer.ValuesBloomFilter), 0)
				}
				totalPointers++
			}
		}
		require.Greater(t, totalPointers, 0)
	}

	// Expect two pointers for each object section, per tenant. This is because we write two streams to the log objects for every tenant.
	for _, count := range pointersByTenant {
		require.Equal(t, 2, count)
	}
}
