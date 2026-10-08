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
	"github.com/grafana/loki/v3/pkg/dataobj/fixtures"
	"github.com/grafana/loki/v3/pkg/dataobj/index/indexobj"
	"github.com/grafana/loki/v3/pkg/dataobj/logsobj"
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

	// This is set low because Pointers & Streams sections ignore section size. There must be a single pointers section per index object to maintain state.
	TargetSectionSize: 1,
}

// testLogsFixture returns two streams with two entries each, at seconds 10 to 25.
func testLogsFixture(t *testing.T) *fixtures.LogFixtureBuilder {
	t.Helper()

	lr := fixtures.NewLogsFixtureBuilder(t)
	lr.ForStream(`{cluster="test",app="foo",env="prod"}`).
		Entry(10, `{trace_id="123", span_id="456"}`, "hello from foo").
		Entry(15, `{trace_id="789"}`, "another message from foo")
	lr.ForStream(`{cluster="test",app="bar",env="dev"}`).
		Entry(20, `{trace_id="abc",user_id="user123"}`, "hello from bar").
		Entry(25, `{trace_id="def",level="error"}`, "error message from bar")
	return lr
}

// createTestLogObject creates a test data object with a logs and a streams
// section for each of the tenants "tenant-0" to "tenant-<tenants-1>", in that
// order.
func createTestLogObject(t *testing.T, tenants int) *dataobj.Object {
	t.Helper()

	lr := testLogsFixture(t)

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

	logBuilder, err := logsobj.NewBuilder(logsobj.BuilderBaseConfig{
		TargetPageSize:          2048,
		TargetObjectSize:        1 << 22,
		TargetSectionSize:       1 << 21,
		BufferSize:              2048 * 8,
		SectionStripeMergeLimit: 2,
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

	indexBuilder, err := indexobj.NewBuilder("tenant-1", testCalculatorConfig, nil, indexobj.NewBuilderMetrics(nil))
	require.NoError(t, err)
	calculator := NewCalculator(indexBuilder, NewCalculatorMetrics(nil))
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
	const (
		tenant  = "tenant-0"
		objects = 10
	)

	t.Run("indexes several objects from readerAt into one range", func(t *testing.T) {
		indexBuilder, err := indexobj.NewBuilder(tenant, testCalculatorConfig, nil, indexobj.NewBuilderMetrics(nil))
		require.NoError(t, err)

		calculator := NewCalculator(indexBuilder, NewCalculatorMetrics(nil))
		for i := 0; i < objects; i++ {
			obj := createTestLogObject(t, 1)

			path := fmt.Sprintf("test/path-%d", i)
			err = calculator.Calculate(context.Background(), logger, obj, path)
			require.NoError(t, err)
		}

		obj, closer, timeRange, err := calculator.Flush()
		require.NoError(t, err)
		defer closer.Close()

		require.Greater(t, obj.Size(), int64(0))
		require.Equal(t, tenant, timeRange.Tenant)
		require.Equal(t, time.Unix(10, 0).UTC(), timeRange.MinTime)
		require.Equal(t, time.Unix(25, 0).UTC(), timeRange.MaxTime)
		require.Equal(t, []string{tenant}, obj.Tenants())

		require.GreaterOrEqual(t, obj.Sections().Count(pointers.CheckSection), 1)
		requireValidPointers(t, obj)
	})

	t.Run("indexes several objects from an FS bucket into one range", func(t *testing.T) {
		indexBuilder, err := indexobj.NewBuilder(tenant, testCalculatorConfig, nil, indexobj.NewBuilderMetrics(nil))
		require.NoError(t, err)

		bucket, err := filesystem.NewBucket(t.TempDir())
		require.NoError(t, err)

		calculator := NewCalculator(indexBuilder, NewCalculatorMetrics(nil))
		for i := 0; i < objects; i++ {
			obj := createTestLogObject(t, 1)

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

		obj, closer, timeRange, err := calculator.Flush()
		require.NoError(t, err)
		defer closer.Close()

		require.Greater(t, obj.Size(), int64(0))
		require.Equal(t, tenant, timeRange.Tenant)
		require.Equal(t, time.Unix(10, 0).UTC(), timeRange.MinTime)
		require.Equal(t, time.Unix(25, 0).UTC(), timeRange.MaxTime)

		require.GreaterOrEqual(t, obj.Sections().Count(pointers.CheckSection), 1)
		requireValidPointers(t, obj)
	})

	t.Run("returns ErrNotSingleTenant and leaves the builder empty when the object holds several tenants", func(t *testing.T) {
		indexBuilder, err := indexobj.NewBuilder(tenant, testCalculatorConfig, nil, indexobj.NewBuilderMetrics(nil))
		require.NoError(t, err)
		calculator := NewCalculator(indexBuilder, NewCalculatorMetrics(nil))

		err = calculator.Calculate(context.Background(), logger, createTestLogObject(t, 2), "test/path")
		require.ErrorIs(t, err, dataobj.ErrNotSingleTenant)
		requireEmptyCalculator(t, calculator, indexBuilder)
	})

	t.Run("returns an error and leaves the builder empty when the object holds another tenant", func(t *testing.T) {
		indexBuilder, err := indexobj.NewBuilder("other-tenant", testCalculatorConfig, nil, indexobj.NewBuilderMetrics(nil))
		require.NoError(t, err)
		calculator := NewCalculator(indexBuilder, NewCalculatorMetrics(nil))

		err = calculator.Calculate(context.Background(), logger, createTestLogObject(t, 1), "test/path")
		require.ErrorContains(t, err, "tenant mismatch")
		requireEmptyCalculator(t, calculator, indexBuilder)
	})

	t.Run("returns ErrUnprocessableObject and leaves the builder empty when the object holds two streams sections", func(t *testing.T) {
		indexBuilder, err := indexobj.NewBuilder(tenant, testCalculatorConfig, nil, indexobj.NewBuilderMetrics(nil))
		require.NoError(t, err)
		calculator := NewCalculator(indexBuilder, NewCalculatorMetrics(nil))

		lr := testLogsFixture(t)
		obj, closer := fixtures.DataObject(t,
			fixtures.StreamsSection(t, tenant, lr.Streams()),
			fixtures.StreamsSection(t, tenant, lr.Streams()),
			fixtures.LogsSection(t, tenant, lr.Logs()),
		)
		t.Cleanup(func() { require.NoError(t, closer.Close()) })

		err = calculator.Calculate(context.Background(), logger, obj, "test/path")
		require.ErrorIs(t, err, ErrUnprocessableObject)
		require.ErrorContains(t, err, "more than one streams section")
		requireEmptyCalculator(t, calculator, indexBuilder)
	})

	t.Run("returns ErrUnprocessableObject and leaves the builder empty when the object holds logs sections without a streams section", func(t *testing.T) {
		indexBuilder, err := indexobj.NewBuilder(tenant, testCalculatorConfig, nil, indexobj.NewBuilderMetrics(nil))
		require.NoError(t, err)
		calculator := NewCalculator(indexBuilder, NewCalculatorMetrics(nil))

		obj, closer := fixtures.DataObject(t, fixtures.LogsSection(t, tenant, testLogsFixture(t).Logs()))
		t.Cleanup(func() { require.NoError(t, closer.Close()) })

		err = calculator.Calculate(context.Background(), logger, obj, "test/path")
		require.ErrorIs(t, err, ErrUnprocessableObject)
		require.ErrorContains(t, err, "no streams section")
		requireEmptyCalculator(t, calculator, indexBuilder)
	})

	t.Run("returns ErrUnprocessableObject and leaves the builder empty when the object holds neither streams nor logs sections", func(t *testing.T) {
		indexBuilder, err := indexobj.NewBuilder(tenant, testCalculatorConfig, nil, indexobj.NewBuilderMetrics(nil))
		require.NoError(t, err)
		calculator := NewCalculator(indexBuilder, NewCalculatorMetrics(nil))

		postingsBuilder := postings.NewBuilder(nil, 0, 0, 1<<20)
		postingsBuilder.SetTenant(tenant)
		postingsBuilder.ObserveLabelPosting(postings.LabelObservation{
			ObjectPath: "src-obj", ColumnName: "app", LabelValue: "foo", StreamID: 1, Timestamp: time.Unix(10, 0).UTC(),
		})
		obj, closer := fixtures.DataObject(t, postingsBuilder)
		t.Cleanup(func() { require.NoError(t, closer.Close()) })

		err = calculator.Calculate(context.Background(), logger, obj, "test/path")
		require.ErrorIs(t, err, ErrUnprocessableObject)
		require.ErrorContains(t, err, "no streams section")
		requireEmptyCalculator(t, calculator, indexBuilder)
	})
}

// requireEmptyCalculator checks that calculator and its builder hold no data.
func requireEmptyCalculator(t *testing.T, calculator *Calculator, builder *indexobj.Builder) {
	t.Helper()

	require.Equal(t, dataobj.TimeRange{Tenant: builder.Tenant()}, builder.TimeRange())
	_, _, _, err := calculator.Flush()
	require.ErrorIs(t, err, indexobj.ErrBuilderEmpty)
}

func TestCalculator_Calculate_SectionIndexesCountOnlyLogs(t *testing.T) {
	ctx := context.Background()
	const (
		path   = "objects/section-index-test"
		tenant = "A"
	)
	sourceBuilder := dataobj.NewBuilder(nil)
	ts := time.Unix(10, 0).UTC()
	streamBuilder := streams.NewBuilder(nil, 2048, 10000)
	streamBuilder.SetTenant(tenant)
	id := streamBuilder.Record(labels.FromStrings("service_name", tenant), ts, 10)
	appendLogsSection := func() {
		logBuilder := logs.NewBuilder(nil, logs.BuilderOptions{
			PageSizeHint: 2048, BufferSize: 2048, StripeMergeLimit: 2, SortOrder: logs.SortStreamASC,
		})
		logBuilder.SetTenant(tenant)
		logBuilder.Append(logs.Record{StreamID: id, Timestamp: ts, Line: []byte("line"), Metadata: labels.FromStrings("trace_id", "trace")})
		require.NoError(t, sourceBuilder.Append(logBuilder))
	}
	// Physical sections: logs, streams, logs. References must be 0 and 1,
	// rather than physical indexes 0 and 2.
	appendLogsSection()
	require.NoError(t, sourceBuilder.Append(streamBuilder))
	appendLogsSection()

	source, sourceCloser, err := sourceBuilder.Flush()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sourceCloser.Close()) })
	require.Len(t, source.Sections(), 3)
	require.True(t, logs.CheckSection(source.Sections()[0]))
	require.True(t, streams.CheckSection(source.Sections()[1]))
	require.True(t, logs.CheckSection(source.Sections()[2]))

	builder, err := indexobj.NewBuilder(tenant, testCalculatorConfig, nil, indexobj.NewBuilderMetrics(nil))
	require.NoError(t, err)
	calculator := NewCalculator(builder, NewCalculatorMetrics(nil))
	require.NoError(t, calculator.Calculate(ctx, log.NewNopLogger(), source, path))
	obj, closer, _, err := calculator.Flush()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, closer.Close()) })

	want := map[int64]bool{0: true, 1: true}
	postingIndexes := map[int64]bool{}
	for _, section := range obj.Sections().Filter(postings.CheckSection) {
		sec, err := postings.Open(ctx, section)
		require.NoError(t, err)
		inner := postings.NewReader(postings.ReaderOptions{Columns: sec.Columns()})
		require.NoError(t, inner.Open(ctx))
		reader := postings.NewRowReader(ctx, inner)
		for reader.Next() {
			row := reader.At()
			require.Equal(t, path, row.ObjectPath)
			require.True(t, want[row.SectionIndex], "unexpected posting reference: %d", row.SectionIndex)
			postingIndexes[row.SectionIndex] = true
		}
		require.NoError(t, reader.Err())
		require.NoError(t, reader.Close())
	}
	require.Equal(t, want, postingIndexes)

	statsIndexes := map[int64]bool{}
	for _, section := range obj.Sections().Filter(stats.CheckSection) {
		sec, err := stats.Open(ctx, section)
		require.NoError(t, err)
		reader := stats.NewRowReader(ctx, sec)
		for reader.Next() {
			row := reader.At()
			require.Equal(t, path, row.ObjectPath)
			statsIndexes[row.SectionIndex] = true
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

	// Expect two pointers for each object section, because every log object holds two streams.
	for _, count := range pointersByTenant {
		require.Equal(t, 2, count)
	}
}
