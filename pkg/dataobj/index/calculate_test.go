package index

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
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
	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/stats"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
	"github.com/grafana/loki/v3/pkg/logproto"

	"github.com/grafana/loki/pkg/push"
)

// fakeLimits exposes SortSchemaLabels and DataObjShardLayout to logs builders.
type fakeLimits struct{}

func (fakeLimits) CompactionPhases(_ string) (runIndex, runLog bool) {
	return true, true
}
func (fakeLimits) SortSchemaLabels(string) []string {
	return fakeSchema
}
func (fakeLimits) DataObjShardLayout(string) streams.ShardLayout {
	return streams.DefaultShardLayout()
}

var testCalculatorConfig = logsobj.BuilderBaseConfig{
	TargetPageSize:          2048,
	TargetObjectSize:        1 << 22, // 4 MiB
	BufferSize:              2048 * 8,
	SectionStripeMergeLimit: 2,

	// TargetSectionSize is 1 byte, so the tests cover index objects with many
	// sections. Each AppendStat call cuts a new stats section, and the postings
	// encoder writes one postings section per entry.
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
	firstShard := streams.DefaultShardLayout().BucketFromLabels(first)
	var second labels.Labels
	for i := 1; i < 256; i++ {
		candidate := labels.FromStrings("service_name", "api", "instance", strconv.Itoa(i))
		if streams.DefaultShardLayout().BucketFromLabels(candidate) != firstShard {
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

	logObj, logCloser, err := logBuilder.FlushAndSort(t.Context())
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
		int64(firstShard): {},
		int64(streams.DefaultShardLayout().BucketFromLabels(second)): {},
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

		require.GreaterOrEqual(t, obj.Sections().Count(postings.CheckSection), 1)
		require.GreaterOrEqual(t, obj.Sections().Count(stats.CheckSection), 1)
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

		require.GreaterOrEqual(t, obj.Sections().Count(postings.CheckSection), 1)
		require.GreaterOrEqual(t, obj.Sections().Count(stats.CheckSection), 1)
	})

	t.Run("builds the same index object key on each rebuild of a multi-section object", func(t *testing.T) {
		source := newMultiSectionSource(t)

		wantKey := ""
		for i := range 11 {
			obj := calculateConcurrently(t, source)
			if i == 0 {
				// The index must hold more than one stats section. With one
				// section, the builder sorts all rows before it encodes them,
				// so the flush order cannot change the bytes and the test
				// cannot detect an unordered flush.
				require.Greater(t, obj.Sections().Count(stats.CheckSection), 1)
			}

			key, err := ObjectKey(context.Background(), obj)
			require.NoError(t, err)
			if i == 0 {
				wantKey = key
				continue
			}
			require.Equal(t, wantKey, key, "object key differs on rebuild %d", i)
		}
	})

	t.Run("writes the stats rows of a multi-section object in ascending section order", func(t *testing.T) {
		obj := calculateConcurrently(t, newMultiSectionSource(t))

		rows := readAllStatsRows(t, obj)
		require.NotEmpty(t, rows)
		sectionIndexes := make([]int64, 0, len(rows))
		for _, row := range rows {
			sectionIndexes = append(sectionIndexes, row["section_index.int64"].(int64))
		}
		require.True(t, slices.IsSorted(sectionIndexes), "section indexes are not sorted: %v", sectionIndexes)
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

	t.Run("returns ErrUnprocessableObject and leaves the builder empty when the object holds a streams section without logs sections", func(t *testing.T) {
		indexBuilder, err := indexobj.NewBuilder(tenant, testCalculatorConfig, nil, indexobj.NewBuilderMetrics(nil))
		require.NoError(t, err)
		calculator := NewCalculator(indexBuilder, NewCalculatorMetrics(nil))

		obj, closer := fixtures.DataObject(t, fixtures.StreamsSection(t, tenant, testLogsFixture(t).Streams()))
		t.Cleanup(func() { require.NoError(t, closer.Close()) })

		err = calculator.Calculate(context.Background(), logger, obj, "test/path")
		require.ErrorIs(t, err, ErrUnprocessableObject)
		require.ErrorContains(t, err, "no logs section")
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

// newMultiSectionSource returns a data object with at least two logs sections.
func newMultiSectionSource(t *testing.T) *dataobj.Object {
	t.Helper()

	source, cleanup := buildSyntheticDataobj(t, 64<<10, 100, 50)
	t.Cleanup(cleanup)
	return source
}

// calculateConcurrently builds the index object of source with 4 workers.
//
// A fixed GOMAXPROCS gives Calculate the same worker limit on each machine.
// With 1, the errgroup in Calculate runs one section at a time, so the order of
// builder writes cannot change.
//
// calculateConcurrently sets the target section size to 1 byte itself, so the
// test does not depend on that value in the shared test config. Each
// AppendStat call then cuts a stats section, and the flush order decides which
// rows each stats section holds.
func calculateConcurrently(t *testing.T, source *dataobj.Object) *dataobj.Object {
	t.Helper()

	prevProcs := runtime.GOMAXPROCS(4)
	defer runtime.GOMAXPROCS(prevProcs)

	cfg := testCalculatorConfig
	cfg.TargetSectionSize = 1
	indexBuilder, err := indexobj.NewBuilder(benchTenant, cfg, nil, indexobj.NewBuilderMetrics(nil))
	require.NoError(t, err)
	calculator := NewCalculator(indexBuilder, NewCalculatorMetrics(nil))
	require.NoError(t, calculator.Calculate(context.Background(), log.NewNopLogger(), source, "test/path"))

	obj, closer, _, err := calculator.Flush()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, closer.Close()) })
	return obj
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
	fixture := fixtures.NewLogsFixtureBuilder(t)
	fixture.ForStream(`{service_name="A"}`).Entry(10, `{trace_id="trace"}`, "line")
	streamsSection := fixtures.StreamsSection(t, tenant, fixture.Streams(), fixtures.WithStreamsLayout(streams.NewLayout(streams.DefaultShardLayout(), nil)))
	// Physical sections: logs, streams, logs. References must be 0 and 1,
	// rather than physical indexes 0 and 2.
	source, sourceCloser := fixtures.DataObject(t,
		fixtures.LogsSection(t, tenant, fixture.Logs()),
		streamsSection,
		fixtures.LogsSection(t, tenant, fixture.Logs()),
	)
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

func TestCalculator_Calculate_StreamsLayout(t *testing.T) {
	const tenant = "A"

	// buildSource builds an object with 16 streams in layout, each with one
	// record that has structured metadata. The streams section records
	// layout if record is true.
	buildSource := func(t *testing.T, layout streams.Layout, record bool) *dataobj.Object {
		t.Helper()
		fixture := fixtures.NewLogsFixtureBuilder(t, fixtures.WithLayout(layout))
		for i := range 16 {
			fixture.ForStream(fmt.Sprintf(`{app="%d",service_name="svc"}`, i)).Entry(10, fmt.Sprintf(`{trace_id="%d"}`, i), "line")
		}
		opts := []fixtures.StreamsSectionOpt{fixtures.WithStreamsLayout(layout)}
		if !record {
			opts = []fixtures.StreamsSectionOpt{fixtures.WithoutStreamsLayout()}
		}
		source, closer := fixtures.DataObject(t,
			fixtures.StreamsSection(t, tenant, fixture.Streams(), opts...),
			fixtures.LogsSection(t, tenant, fixture.Logs()),
		)
		t.Cleanup(func() { require.NoError(t, closer.Close()) })
		return source
	}
	calculate := func(t *testing.T, source *dataobj.Object) (*dataobj.Object, error) {
		t.Helper()
		builder, err := indexobj.NewBuilder(tenant, testCalculatorConfig, nil, indexobj.NewBuilderMetrics(nil))
		require.NoError(t, err)
		calculator := NewCalculator(builder, NewCalculatorMetrics(nil))
		if err := calculator.Calculate(t.Context(), log.NewNopLogger(), source, "objects/source"); err != nil {
			return nil, err
		}
		obj, closer, _, err := calculator.Flush()
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, closer.Close()) })
		return obj, nil
	}

	t.Run("indexes labels and structured metadata with the schema labels and shard layout of the recorded layout", func(t *testing.T) {
		four, err := streams.ShardLayoutFromBuckets(4)
		require.NoError(t, err)
		layout := streams.NewLayout(four, []string{"label:app"})
		indexObj, err := calculate(t, buildSource(t, layout, true))
		require.NoError(t, err)

		statsRows := readAllStatsRows(t, indexObj)
		require.NotEmpty(t, statsRows)
		for _, row := range statsRows {
			require.Equal(t, "label:app", row["sort_schema.utf8"])
			require.Less(t, row["__shard_bucket__.int64"].(int64), int64(4))
		}

		var labelRows, bloomRows int
		for _, section := range indexObj.Sections().Filter(postings.CheckSection) {
			sec, err := postings.Open(t.Context(), section)
			require.NoError(t, err)
			inner := postings.NewReader(postings.ReaderOptions{Columns: sec.Columns()})
			require.NoError(t, inner.Open(t.Context()))
			reader := postings.NewRowReader(t.Context(), inner)
			for reader.Next() {
				row := reader.At()
				require.Equal(t, int64(4), row.ShardBuckets)
				switch row.Kind {
				case postings.KindLabel:
					labelRows++
				case postings.KindBloom:
					require.Equal(t, "trace_id", row.ColumnName)
					require.Less(t, row.MaxShardBucket, uint32(4))
					bloomRows++
				}
			}
			require.NoError(t, reader.Err())
			require.NoError(t, reader.Close())
		}
		require.Positive(t, labelRows)
		require.Positive(t, bloomRows)
	})

	t.Run("returns an unprocessable object error for a legacy object", func(t *testing.T) {
		data, err := os.ReadFile(filepath.Join("..", "testdata", "legacy", "legacy-cluster-a.dataobj"))
		require.NoError(t, err)
		source, err := dataobj.FromReaderAt(bytes.NewReader(data), int64(len(data)))
		require.NoError(t, err)

		builder, err := indexobj.NewBuilder("T", testCalculatorConfig, nil, indexobj.NewBuilderMetrics(nil))
		require.NoError(t, err)
		err = NewCalculator(builder, NewCalculatorMetrics(nil)).Calculate(t.Context(), log.NewNopLogger(), source, "objects/legacy")
		require.ErrorIs(t, err, ErrUnprocessableObject)
	})

	t.Run("returns an error for an object with another layout than the objects already in the index", func(t *testing.T) {
		builder, err := indexobj.NewBuilder(tenant, testCalculatorConfig, nil, indexobj.NewBuilderMetrics(nil))
		require.NoError(t, err)
		calculator := NewCalculator(builder, NewCalculatorMetrics(nil))
		appSource := buildSource(t, streams.NewLayout(streams.DefaultShardLayout(), []string{"label:app"}), true)
		require.NoError(t, calculator.Calculate(t.Context(), log.NewNopLogger(), appSource, "objects/app"))

		serviceSource := buildSource(t, streams.NewLayout(streams.DefaultShardLayout(), []string{"label:service_name"}), true)
		err = calculator.Calculate(t.Context(), log.NewNopLogger(), serviceSource, "objects/service")
		require.ErrorContains(t, err, "already has stream layout")
		require.NotErrorIs(t, err, ErrUnprocessableObject)
	})

	t.Run("returns an unprocessable object error when the streams section records no layout", func(t *testing.T) {
		_, err := calculate(t, buildSource(t, streams.NewLayout(streams.DefaultShardLayout(), nil), false))
		require.ErrorIs(t, err, ErrUnprocessableObject)
		require.ErrorContains(t, err, "records no layout")
	})
}
