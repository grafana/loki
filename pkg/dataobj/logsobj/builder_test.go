package logsobj

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"math/rand"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/push"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/result"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/scratch"
	"github.com/grafana/loki/v3/pkg/validation"
)

var testBuilderConfig = BuilderBaseConfig{
	TargetPageSize:          2048,
	TargetObjectSize:        1 << 20, // 1 MiB
	TargetSectionSize:       8 << 10, // 8 KiB
	BufferSize:              2048 * 8,
	SectionStripeMergeLimit: 2,
}

func TestBuilder(t *testing.T) {
	testStreams := []logproto.Stream{
		{
			Labels: `{cluster="test",app="foo"}`,
			Entries: []push.Entry{
				{
					Timestamp: time.Unix(10, 0).UTC(),
					Line:      "hello",
					StructuredMetadata: push.LabelsAdapter{
						{Name: "trace_id", Value: "123"},
					},
				},
				{
					Timestamp: time.Unix(5, 0).UTC(),
					Line:      "hello again",
					StructuredMetadata: push.LabelsAdapter{
						{Name: "trace_id", Value: "456"},
						{Name: "span_id", Value: "789"},
					},
				},
			},
		},
		{
			Labels: `{cluster="test",app="bar"}`,
			Entries: []push.Entry{
				{
					Timestamp: time.Unix(15, 0).UTC(),
					Line:      "world",
					StructuredMetadata: push.LabelsAdapter{
						{Name: "trace_id", Value: "abc"},
					},
				},
				{
					Timestamp: time.Unix(20, 0).UTC(),
					Line:      "world again",
					StructuredMetadata: push.LabelsAdapter{
						{Name: "trace_id", Value: "def"},
						{Name: "span_id", Value: "ghi"},
					},
				},
			},
		},
	}

	t.Run("Build", func(t *testing.T) {
		builder, err := NewBuilder(testBuilderConfig, nil, NewBuilderMetrics(), log.NewNopLogger(), NoTenantOverrides{})
		require.NoError(t, err)

		for _, entry := range testStreams {
			require.NoError(t, builder.Append("tenant", entry, time.Now()))
		}
		obj, closer, err := builder.flushWithoutLayout()
		require.NoError(t, err)
		defer closer.Close()

		require.Equal(t, 1, obj.Sections().Count(streams.CheckSection))
		require.Equal(t, 1, obj.Sections().Count(logs.CheckSection))

		for _, sec := range obj.Sections().Filter(streams.CheckSection) {
			streamSec, err := streams.Open(t.Context(), sec)
			require.NoError(t, err)
			for res := range streams.IterSection(t.Context(), streamSec) {
				stream, err := res.Value()
				require.NoError(t, err)
				require.Equal(t, int64(streams.DefaultShardLayout().BucketFromLabels(stream.Labels)), stream.ShardBucket)
				require.GreaterOrEqual(t, stream.ShardBucket, int64(0))
				require.Less(t, stream.ShardBucket, int64(streams.DefaultShardLayout().Buckets()))
			}
		}
	})
}

func TestNewBuilder(t *testing.T) {
	t.Run("returns an error when the overrides are nil", func(t *testing.T) {
		builder, err := NewBuilder(testBuilderConfig, nil, NewBuilderMetrics(), log.NewNopLogger(), nil)
		require.EqualError(t, err, "tenant overrides must not be nil")
		require.Nil(t, builder)
	})
}

// TestBuilder_Append ensures that appending to the buffer eventually reports
// that the buffer is full.
func TestBuilder_Append(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	builder, err := NewBuilder(testBuilderConfig, nil, NewBuilderMetrics(), log.NewNopLogger(), NoTenantOverrides{})
	require.NoError(t, err)

	tenant := "test"

	for {
		require.NoError(t, ctx.Err())

		// Append until the builder reports that it is full.
		if builder.IsFull() {
			break
		}

		require.NoError(t, builder.Append(tenant, logproto.Stream{
			Labels: `{cluster="test",app="foo"}`,
			Entries: []push.Entry{{
				Timestamp: time.Now().UTC(),
				Line:      strings.Repeat("a", 1024),
			}},
		}, time.Now()))
	}

	obj, closer, err := builder.flushWithoutLayout()
	require.NoError(t, err)
	defer closer.Close()

	// When a section builder is reset, which happens on flush, the
	// tenant is reset too. We must check that the tenant is added back
	// to the section builder otherwise tenant will be absent from successive
	// sections.
	secs := obj.Sections()
	require.Equal(t, 1, secs.Count(streams.CheckSection))
	require.Greater(t, secs.Count(logs.CheckSection), 1)
	for _, section := range secs.Filter(logs.CheckSection) {
		require.Equal(t, tenant, section.Tenant)
	}
}

// TestBuilder_AppendRecord ensures that appending to the buffer eventually reports
// that the buffer is full via the AppendRecord call.
func TestBuilder_AppendRecord(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	builder, err := NewBuilder(testBuilderConfig, nil, NewBuilderMetrics(), log.NewNopLogger(), NoTenantOverrides{})
	require.NoError(t, err)

	tenant := "test"

	lbs, err := syntax.ParseLabels(`{cluster="test",app="foo"}`)
	require.NoError(t, err)

	for {
		require.NoError(t, ctx.Err())

		// Append until the builder reports that it is full.
		if builder.IsFull() {
			break
		}

		require.NoError(t, builder.AppendRecord(tenant, lbs, logs.Record{
			Timestamp: time.Now().UTC(),
			Line:      bytes.Repeat([]byte("a"), 1024),
		}, time.Now()))
	}

	obj, closer, err := builder.flushWithoutLayout()
	require.NoError(t, err)
	defer closer.Close()

	// When a section builder is reset, which happens on flush, the
	// tenant is reset too. We must check that the tenant is added back
	// to the section builder otherwise tenant will be absent from successive
	// sections.
	secs := obj.Sections()
	require.Equal(t, 1, secs.Count(streams.CheckSection))
	require.Greater(t, secs.Count(logs.CheckSection), 1)
	for _, section := range secs.Filter(logs.CheckSection) {
		require.Equal(t, tenant, section.Tenant)
	}
}

// TestBuilder_EarliestRecordTime ensures the builder tracks the earliest
// record time seen across appends and resets it when the builder is reset.
func TestBuilder_EarliestRecordTime(t *testing.T) {
	newStream := func() logproto.Stream {
		return logproto.Stream{
			Labels:  `{cluster="test",app="foo"}`,
			Entries: []push.Entry{{Timestamp: time.Unix(0, 0).UTC(), Line: "hello"}},
		}
	}

	t.Run("zero on an empty builder", func(t *testing.T) {
		builder, err := NewBuilder(testBuilderConfig, nil, NewBuilderMetrics(), log.NewNopLogger(), NoTenantOverrides{})
		require.NoError(t, err)
		require.True(t, builder.GetEarliestRecordTime().IsZero())
	})

	t.Run("set on first append", func(t *testing.T) {
		builder, err := NewBuilder(testBuilderConfig, nil, NewBuilderMetrics(), log.NewNopLogger(), NoTenantOverrides{})
		require.NoError(t, err)

		recTime := time.Unix(100, 0).UTC()
		require.NoError(t, builder.Append("tenant", newStream(), recTime))
		require.Equal(t, recTime, builder.GetEarliestRecordTime())
	})

	t.Run("tracks the minimum across appends", func(t *testing.T) {
		builder, err := NewBuilder(testBuilderConfig, nil, NewBuilderMetrics(), log.NewNopLogger(), NoTenantOverrides{})
		require.NoError(t, err)

		require.NoError(t, builder.Append("tenant", newStream(), time.Unix(100, 0).UTC()))
		// A later record must not move the earliest time forward.
		require.NoError(t, builder.Append("tenant", newStream(), time.Unix(200, 0).UTC()))
		require.Equal(t, time.Unix(100, 0).UTC(), builder.GetEarliestRecordTime())
		// An earlier record must move the earliest time backward.
		require.NoError(t, builder.Append("tenant", newStream(), time.Unix(50, 0).UTC()))
		require.Equal(t, time.Unix(50, 0).UTC(), builder.GetEarliestRecordTime())
	})

	t.Run("reset on Reset", func(t *testing.T) {
		builder, err := NewBuilder(testBuilderConfig, nil, NewBuilderMetrics(), log.NewNopLogger(), NoTenantOverrides{})
		require.NoError(t, err)

		require.NoError(t, builder.Append("tenant", newStream(), time.Unix(100, 0).UTC()))
		builder.Reset()
		require.True(t, builder.GetEarliestRecordTime().IsZero())
	})

	t.Run("reset on Flush", func(t *testing.T) {
		builder, err := NewBuilder(testBuilderConfig, nil, NewBuilderMetrics(), log.NewNopLogger(), NoTenantOverrides{})
		require.NoError(t, err)

		require.NoError(t, builder.Append("tenant", newStream(), time.Unix(100, 0).UTC()))
		_, closer, err := builder.flushWithoutLayout()
		require.NoError(t, err)
		defer closer.Close()
		require.True(t, builder.GetEarliestRecordTime().IsZero())
	})
}

func testFlushAndSortOrder(t *testing.T) {
	builder, _ := NewBuilder(testBuilderConfig, nil, NewBuilderMetrics(), log.NewNopLogger(), NoTenantOverrides{})
	sortBuilder, _ := NewBuilder(testBuilderConfig, nil, NewBuilderMetrics(), log.NewNopLogger(), NoTenantOverrides{})

	type copiedRecord struct {
		tenant    string
		timestamp int64
		line      string
		metadata  string
	}

	now := time.Date(2025, time.September, 17, 0, 0, 0, 0, time.UTC)
	numRows := 16 // 16 rows with 1KiB each line and 8KiB section size ~> 2 logs sections per tenant
	expectedRecords := make(map[copiedRecord]int)

	for _, tenant := range []string{"tenant-a", "tenant-b", "tenant-c"} {
		appendRecord := func(i int) {
			timestamp := now.Add(time.Duration(i%8) * time.Second)
			line := fmt.Sprintf("%s/%02d/", tenant, i) + strings.Repeat("a", 1024)
			metadata := fmt.Sprintf("metadata-%02d", i)
			record := copiedRecord{
				tenant:    tenant,
				timestamp: timestamp.UnixNano(),
				line:      line,
				metadata:  metadata,
			}
			for _, b := range []*Builder{builder, sortBuilder} {
				err := b.Append(tenant, logproto.Stream{
					Labels: `{cluster="test",app="foo"}`,
					Entries: []push.Entry{{
						Timestamp: timestamp,
						Line:      line,
						StructuredMetadata: []push.LabelAdapter{{
							Name:  "sequence",
							Value: metadata,
						}},
					}},
				}, timestamp)
				require.NoError(t, err)
			}
			expectedRecords[record]++
		}

		for i := range numRows {
			appendRecord(i)
		}

		// Repeat a row after enough data has been appended to place the two
		// copies in different source sections.
		appendRecord(0)
	}

	obj1, closer1, err := builder.flushWithoutLayout()
	require.NoError(t, err)
	defer closer1.Close()

	obj2, closer2, err := sortBuilder.FlushAndSort(t.Context())
	require.NoError(t, err)
	defer closer2.Close()

	require.Equal(
		t,
		obj1.Sections().Count(streams.CheckSection),
		obj2.Sections().Count(streams.CheckSection),
		"objects have different amount of streams sections",
	)
	require.Equal(
		t,
		obj1.Sections().Count(logs.CheckSection),
		obj2.Sections().Count(logs.CheckSection),
		"objects have different amount of logs sections",
	)

	// Assert DESC timestamp ordering across sections of a tenant
	actualRecords := make(map[copiedRecord]int)
	for _, tenant := range []string{"tenant-a", "tenant-b", "tenant-c"} {
		prevTs := time.Unix(0, math.MaxInt64)
		for _, sec := range obj2.Sections().Filter(func(s *dataobj.Section) bool {
			return logs.CheckSection(s) && s.Tenant == tenant
		}) {
			for res := range iterLogsSection(t, sec) {
				val, err := res.Value()
				require.NoError(t, err)
				require.LessOrEqual(t, val.Timestamp, prevTs)
				prevTs = val.Timestamp
				actualRecords[copiedRecord{
					tenant:    tenant,
					timestamp: val.Timestamp.UnixNano(),
					line:      string(val.Line),
					metadata:  val.Metadata.Get("sequence"),
				}]++
			}
		}
	}
	require.Equal(t, expectedRecords, actualRecords)
}

func testFlushAndSortShardBuckets(t *testing.T) {
	const (
		tenant      = "t1"
		streamCount = 16
	)
	now := time.Date(2025, time.September, 17, 0, 0, 0, 0, time.UTC)
	overrides := tenantOverrides{tenant: {"label:app"}}

	newFilledBuilder := func(t *testing.T, builderOverrides TenantOverrides) *Builder {
		t.Helper()
		builder, err := NewBuilder(testBuilderConfig, nil, NewBuilderMetrics(), log.NewNopLogger(), builderOverrides)
		require.NoError(t, err)
		for i := range streamCount {
			require.NoError(t, builder.Append(tenant, logproto.Stream{
				Labels:  fmt.Sprintf(`{app="app-%d", cluster="test"}`, i),
				Entries: []push.Entry{{Timestamp: now, Line: "line"}},
			}, now))
		}
		return builder
	}

	bucketsByLabels := func(t *testing.T, obj *dataobj.Object) map[string]int64 {
		t.Helper()
		buckets := map[string]int64{}
		for _, sec := range obj.Sections().Filter(streams.CheckSection) {
			streamsSection, err := streams.Open(t.Context(), sec)
			require.NoError(t, err)
			for res := range streams.IterSection(t.Context(), streamsSection) {
				stream, err := res.Value()
				require.NoError(t, err)
				buckets[stream.Labels.String()] = stream.ShardBucket
			}
		}
		return buckets
	}

	t.Run("keeps the bucket that the build gave every stream, without computing it again", func(t *testing.T) {
		built, builtCloser, err := newFilledBuilder(t, overrides).flushWithoutLayout()
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, builtCloser.Close()) })
		sorted, sortedCloser, err := newFilledBuilder(t, overrides).FlushAndSort(t.Context())
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, sortedCloser.Close()) })

		builtBuckets := bucketsByLabels(t, built)
		require.Len(t, builtBuckets, streamCount)
		require.Equal(t, builtBuckets, bucketsByLabels(t, sorted))

		distinctBuckets := map[int64]struct{}{}
		for _, bucket := range builtBuckets {
			distinctBuckets[bucket] = struct{}{}
		}
		require.Greater(t, len(distinctBuckets), 1, "the streams must span several buckets, or a wrong bucket could go unseen")
	})

	t.Run("gives every stream the bucket of the shard layout of the tenant and records that layout", func(t *testing.T) {
		shardLayout := mustShardLayout(t, 8)
		shardOverrides := &mutableOverrides{schema: []string{"label:app"}, shardLayout: &shardLayout}
		obj, closer, err := newFilledBuilder(t, shardOverrides).FlushAndSort(t.Context())
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, closer.Close()) })

		requireStreamsLayout(t, obj, tenant, streams.NewLayout(shardLayout, []string{"label:app"}))

		got := bucketsByLabels(t, obj)
		require.Len(t, got, streamCount)
		distinctBuckets := map[int64]struct{}{}
		for i := range streamCount {
			streamLabels := labels.FromStrings("app", fmt.Sprintf("app-%d", i), "cluster", "test")
			want := int64(shardLayout.BucketFromLabels(streamLabels))
			require.Equal(t, want, got[streamLabels.String()], "bucket of stream %s", streamLabels)
			distinctBuckets[want] = struct{}{}
		}
		require.Greater(t, len(distinctBuckets), 1, "the streams must span several buckets, or a wrong bucket could go unseen")
	})

	t.Run("orders stream IDs by the sort key of the shard layout of the tenant", func(t *testing.T) {
		shardLayout := mustShardLayout(t, 4)
		schema := []string{"label:app"}
		obj, closer, err := newFilledBuilder(t, &mutableOverrides{schema: schema, shardLayout: &shardLayout}).FlushAndSort(t.Context())
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, closer.Close()) })

		var sorted []streams.Stream
		for _, sec := range obj.Sections().Filter(streams.CheckSection) {
			streamsSection, err := streams.Open(t.Context(), sec)
			require.NoError(t, err)
			for res := range streams.IterSection(t.Context(), streamsSection) {
				stream, err := res.Value()
				require.NoError(t, err)
				sorted = append(sorted, stream)
			}
		}
		require.Len(t, sorted, streamCount)
		slices.SortFunc(sorted, func(a, b streams.Stream) int { return cmp.Compare(a.ID, b.ID) })

		sortKey := func(streamLabels labels.Labels) streams.SortKey {
			schemaKey, err := ComputeSchemaKey(streamLabels, schema)
			require.NoError(t, err)
			return shardLayout.NewSortKey(streamLabels, schemaKey)
		}
		for i := 1; i < len(sorted); i++ {
			require.LessOrEqual(t, streams.CompareSortKey(sortKey(sorted[i-1].Labels), sortKey(sorted[i].Labels)), 0,
				"stream %d must not sort after stream %d", sorted[i-1].ID, sorted[i].ID)
		}
	})
}

func BenchmarkBuilder_FlushAndSort(b *testing.B) {
	now := time.Date(2025, time.September, 17, 0, 0, 0, 0, time.UTC)
	numRows := 768 << 10 // 786432 rows per tenant
	sizeOfRow := 1024
	tenants := []string{"tenant-a", "tenant-b", "tenant-c"}
	apps := []string{"alpha", "bravo", "charlie", "delta"}
	instances := []string{"i-0", "i-1", "i-2"}

	// Pre-generate random lines so they don't compress away.
	// 256 unique lines reused round-robin keeps setup cheap
	// while defeating columnar compression.
	//
	// This is done to avoid all lines from ending up in a single section.
	// The sort is only exercised when it merges multiple input sections.
	const numLines = 256
	rng := rand.New(rand.NewSource(42))
	linePool := make([]string, numLines)
	for i := range linePool {
		buf := make([]byte, sizeOfRow)
		rng.Read(buf)
		linePool[i] = string(buf)
	}

	// Need ~128K rows per section to reach target size of 128 MiB.
	// ~6 logs sections per tenant would require ~768K rows per tenant.
	benchBaseCfg := BuilderBaseConfig{
		TargetPageSize:          2 << 20,   // 2 MiB
		TargetObjectSize:        128 << 20, // 128 MiB (compressed)
		TargetSectionSize:       128 << 20, // 128 MiB (uncompressed)
		BufferSize:              16 << 20,  // 16 MiB
		SectionStripeMergeLimit: 2,
	}

	benchCases := []struct {
		name      string
		cfg       BuilderBaseConfig
		overrides TenantOverrides
		labels    func(tenant string, i int) string
	}{
		{
			name:      "default_sort_schema",
			cfg:       benchBaseCfg,
			overrides: NoTenantOverrides{},
			labels: func(_ string, i int) string {
				app := apps[i%len(apps)]
				inst := instances[i%len(instances)]
				return fmt.Sprintf(`{cluster="test",app=%q,instance=%q}`, app, inst)
			},
		},
		{
			name: "custom_sort_schema",
			cfg:  benchBaseCfg,
			overrides: tenantOverrides{
				"tenant-a": {"label:app"},
				"tenant-b": {"label:app"},
				"tenant-c": {"label:app"},
			},
			labels: func(_ string, i int) string {
				app := apps[i%len(apps)]
				inst := instances[i%len(instances)]
				return fmt.Sprintf(`{cluster="test",app=%q,instance=%q}`, app, inst)
			},
		},
	}

	for _, bc := range benchCases {
		b.Run(bc.name, func(b *testing.B) {
			builder, _ := NewBuilder(bc.cfg, nil, NewBuilderMetrics(), log.NewNopLogger(), bc.overrides)
			fill := func() {
				for _, tenant := range tenants {
					for i := range numRows {
						err := builder.Append(tenant, logproto.Stream{
							Labels: bc.labels(tenant, i),
							Entries: []push.Entry{{
								Timestamp: now.Add(time.Duration(i%8) * time.Second),
								Line:      linePool[i%numLines],
							}},
						}, now.Add(time.Duration(i%8)*time.Second))
						require.NoError(b, err)
					}
				}
			}

			dataSize := numRows * sizeOfRow * len(tenants)
			b.SetBytes(int64(dataSize))

			fill()
			obj, closer, err := builder.flushWithoutLayout()
			require.NoError(b, err)
			b.Logf("logs sections before the sort: %d", obj.Sections().Count(logs.CheckSection))
			require.NoError(b, closer.Close())

			for b.Loop() {
				b.StopTimer()
				fill()
				b.StartTimer()
				_, closer, err := builder.FlushAndSort(b.Context())
				require.NoError(b, err)
				require.NoError(b, closer.Close())
			}
		})
	}
}

// tenantOverrides maps tenant IDs to their schema labels. Every tenant has the
// default shard layout.
type tenantOverrides map[string][]string

func (m tenantOverrides) SortSchemaLabels(tenant string) []string { return m[tenant] }

func (m tenantOverrides) DataObjShardLayout(string) streams.ShardLayout {
	return streams.DefaultShardLayout()
}

// limitsByTenant implements validation.TenantLimits for builder tests that
// need the real Overrides defaulting path.
type limitsByTenant map[string]*validation.Limits

func (m limitsByTenant) TenantLimits(userID string) *validation.Limits { return m[userID] }
func (m limitsByTenant) AllByUserID() map[string]*validation.Limits    { return m }

func testFlushAndSortSchema(t *testing.T) {
	now := time.Date(2025, time.September, 17, 0, 0, 0, 0, time.UTC)

	makeCfg := func() BuilderBaseConfig {
		return BuilderBaseConfig{
			TargetPageSize:          2048,
			TargetObjectSize:        1 << 20,
			TargetSectionSize:       8 << 10,
			BufferSize:              2048 * 8,
			SectionStripeMergeLimit: 2,
		}
	}

	build := func(t *testing.T, cfg BuilderBaseConfig, tenant string, appVals []string, overrides TenantOverrides, sorted bool) *dataobj.Object {
		t.Helper()
		b, err := NewBuilder(cfg, nil, NewBuilderMetrics(), log.NewNopLogger(), overrides)
		require.NoError(t, err)
		for _, app := range appVals {
			for i := range 4 {
				require.NoError(t, b.Append(tenant, logproto.Stream{
					Labels: fmt.Sprintf(`{app=%q}`, app),
					Entries: []push.Entry{{
						Timestamp: now.Add(time.Duration(i) * time.Second),
						Line:      fmt.Sprintf("%s-%d", app, i),
					}},
				}, now.Add(time.Duration(i)*time.Second)))
			}
		}
		flush := b.flushWithoutLayout
		if sorted {
			flush = func() (*dataobj.Object, io.Closer, error) { return b.FlushAndSort(t.Context()) }
		}
		obj, closer, err := flush()
		require.NoError(t, err)
		t.Cleanup(func() { closer.Close() })
		return obj
	}
	buildObj := func(t *testing.T, cfg BuilderBaseConfig, tenant string, appVals []string, overrides TenantOverrides) *dataobj.Object {
		t.Helper()
		return build(t, cfg, tenant, appVals, overrides, false)
	}
	buildSorted := func(t *testing.T, cfg BuilderBaseConfig, tenant string, appVals []string, overrides TenantOverrides) *dataobj.Object {
		t.Helper()
		return build(t, cfg, tenant, appVals, overrides, true)
	}

	appOrder := func(t *testing.T, obj *dataobj.Object, tenant string) []string {
		t.Helper()
		streamToApp := make(map[int64]string)
		for _, sec := range obj.Sections().Filter(func(s *dataobj.Section) bool {
			return streams.CheckSection(s) && s.Tenant == tenant
		}) {
			streamSec, err := streams.Open(t.Context(), sec)
			require.NoError(t, err)
			for res := range streams.IterSection(t.Context(), streamSec) {
				val, err := res.Value()
				require.NoError(t, err)
				streamToApp[val.ID] = val.Labels.Get("app")
			}
		}
		seen := make(map[string]bool)
		var order []string
		for _, sec := range obj.Sections().Filter(func(s *dataobj.Section) bool {
			return logs.CheckSection(s) && s.Tenant == tenant
		}) {
			for res := range iterLogsSection(t, sec) {
				val, err := res.Value()
				require.NoError(t, err)
				app := streamToApp[val.StreamID]
				if !seen[app] {
					seen[app] = true
					order = append(order, app)
				}
			}
		}
		return order
	}

	timestampsDescWithinGroups := func(t *testing.T, obj *dataobj.Object, tenant string) {
		t.Helper()
		streamToApp := make(map[int64]string)
		for _, sec := range obj.Sections().Filter(func(s *dataobj.Section) bool {
			return streams.CheckSection(s) && s.Tenant == tenant
		}) {
			streamSec, err := streams.Open(t.Context(), sec)
			require.NoError(t, err)
			for res := range streams.IterSection(t.Context(), streamSec) {
				val, err := res.Value()
				require.NoError(t, err)
				streamToApp[val.ID] = val.Labels.Get("app")
			}
		}
		prevTS := make(map[string]time.Time)
		for _, sec := range obj.Sections().Filter(func(s *dataobj.Section) bool {
			return logs.CheckSection(s) && s.Tenant == tenant
		}) {
			for res := range iterLogsSection(t, sec) {
				val, err := res.Value()
				require.NoError(t, err)
				app := streamToApp[val.StreamID]
				if prev, ok := prevTS[app]; ok {
					require.LessOrEqual(t, val.Timestamp.UnixNano(), prev.UnixNano(),
						"timestamps not DESC within app=%q", app)
				}
				prevTS[app] = val.Timestamp
			}
		}
	}

	t.Run("sorts a tenant with a custom schema by app and a tenant without one by service_name", func(t *testing.T) {
		cfg := makeCfg()
		var defaults validation.Limits
		defaults.RegisterFlags(flag.NewFlagSet("test", flag.PanicOnError))
		overrides, err := validation.NewOverrides(defaults, limitsByTenant{
			"schema-tenant": {SortSchema: validation.SortSchema{"label:app"}},
		})
		require.NoError(t, err)

		b, err := NewBuilder(cfg, nil, NewBuilderMetrics(), log.NewNopLogger(), overrides)
		require.NoError(t, err)
		for _, name := range []string{"zoo", "alpha", "middle"} {
			for i := range 4 {
				ts := now.Add(time.Duration(i) * time.Second)
				require.NoError(t, b.Append("schema-tenant", logproto.Stream{
					Labels:  fmt.Sprintf(`{app=%q}`, name),
					Entries: []push.Entry{{Timestamp: ts, Line: name}},
				}, ts))
				require.NoError(t, b.Append("default-tenant", logproto.Stream{
					Labels:  fmt.Sprintf(`{service_name=%q}`, name),
					Entries: []push.Entry{{Timestamp: ts, Line: name}},
				}, ts))
			}
		}
		obj2, closer2, err := b.FlushAndSort(t.Context())
		require.NoError(t, err)
		defer closer2.Close()

		requireStreamsLayout(t, obj2, "schema-tenant", streams.NewLayout(streams.DefaultShardLayout(), []string{"label:app"}))
		requireStreamsLayout(t, obj2, "default-tenant", streams.NewLayout(streams.DefaultShardLayout(), []string{"label:service_name"}))
		require.Equal(t, expectedLabelOrder(t, []string{"zoo", "alpha", "middle"}, "app", []string{"label:app"}), appOrder(t, obj2, "schema-tenant"))
		timestampsDescWithinGroups(t, obj2, "schema-tenant")

		// default-tenant has no per-tenant override, so Overrides.SortSchemaLabels
		// returns the limits default (label:service_name).
		streamToSvc := make(map[int64]string)
		for _, sec := range obj2.Sections().Filter(func(s *dataobj.Section) bool {
			return streams.CheckSection(s) && s.Tenant == "default-tenant"
		}) {
			streamSec, err := streams.Open(t.Context(), sec)
			require.NoError(t, err)
			for res := range streams.IterSection(t.Context(), streamSec) {
				val, err := res.Value()
				require.NoError(t, err)
				streamToSvc[val.ID] = val.Labels.Get("service_name")
			}
		}
		seen := make(map[string]bool)
		var svcOrder []string
		for _, sec := range obj2.Sections().Filter(func(s *dataobj.Section) bool {
			return logs.CheckSection(s) && s.Tenant == "default-tenant"
		}) {
			for res := range iterLogsSection(t, sec) {
				val, err := res.Value()
				require.NoError(t, err)
				svc := streamToSvc[val.StreamID]
				if !seen[svc] {
					seen[svc] = true
					svcOrder = append(svcOrder, svc)
				}
			}
		}
		require.Equal(t, expectedLabelOrder(t, []string{"alpha", "middle", "zoo"}, "service_name", []string{"label:service_name"}), svcOrder)
	})

	t.Run("records the layout of the tenant in the streams section", func(t *testing.T) {
		cfg := makeCfg()
		overrides := tenantOverrides{"t1": {"label:app"}}
		obj2 := buildSorted(t, cfg, "t1", []string{"b", "a"}, overrides)

		requireStreamsLayout(t, obj2, "t1", streams.NewLayout(streams.DefaultShardLayout(), []string{"label:app"}))

		require.Equal(t, expectedLabelOrder(t, []string{"b", "a"}, "app", []string{"label:app"}), appOrder(t, obj2, "t1"))
		timestampsDescWithinGroups(t, obj2, "t1")
	})

	t.Run("flush without sort writes logs sections that are each in schema order", func(t *testing.T) {
		cfg := makeCfg()
		overrides := tenantOverrides{"t1": {"label:app"}}
		obj := buildObj(t, cfg, "t1", []string{"zoo", "alpha", "middle"}, overrides)

		streamToApp := make(map[int64]string)
		streamToShard := make(map[int64]uint32)
		for _, sec := range obj.Sections().Filter(func(s *dataobj.Section) bool {
			return streams.CheckSection(s) && s.Tenant == "t1"
		}) {
			streamSec, err := streams.Open(t.Context(), sec)
			require.NoError(t, err)
			for res := range streams.IterSection(t.Context(), streamSec) {
				val, err := res.Value()
				require.NoError(t, err)
				streamToApp[val.ID] = val.Labels.Get("app")
				streamToShard[val.ID] = uint32(val.ShardBucket)
			}
		}

		for _, sec := range obj.Sections().Filter(func(s *dataobj.Section) bool {
			return logs.CheckSection(s) && s.Tenant == "t1"
		}) {
			var (
				prevApp      string
				prevShard    uint32
				prevStreamID int64
				prevTS       time.Time
				firstRecord  = true
			)
			for res := range iterLogsSection(t, sec) {
				val, err := res.Value()
				require.NoError(t, err)
				app := streamToApp[val.StreamID]
				shard := streamToShard[val.StreamID]
				if !firstRecord {
					switch {
					case shard != prevShard:
						require.GreaterOrEqual(t, shard, prevShard)
					case app != prevApp:
						require.GreaterOrEqual(t, app, prevApp)
					case val.StreamID != prevStreamID:
						require.GreaterOrEqual(t, val.StreamID, prevStreamID)
					default:
						require.LessOrEqual(t, val.Timestamp.UnixNano(), prevTS.UnixNano())
					}
				}
				prevApp = app
				prevShard = shard
				prevStreamID = val.StreamID
				prevTS = val.Timestamp
				firstRecord = false
			}
		}
	})

	t.Run("orders streams by every label of a compound schema in order", func(t *testing.T) {
		cfg := makeCfg()
		overrides := tenantOverrides{"t1": {"label:namespace", "label:app"}}
		b, err := NewBuilder(cfg, nil, NewBuilderMetrics(), log.NewNopLogger(), overrides)
		require.NoError(t, err)

		type entry struct{ ns, app string }
		for _, e := range []entry{{"ns-b", "app-z"}, {"ns-a", "app-z"}, {"ns-a", "app-a"}, {"ns-b", "app-a"}} {
			require.NoError(t, b.Append("t1", logproto.Stream{
				Labels:  fmt.Sprintf(`{namespace=%q,app=%q}`, e.ns, e.app),
				Entries: []push.Entry{{Timestamp: now, Line: e.ns + "/" + e.app}},
			}, now))
		}
		obj2, closer2, err := b.FlushAndSort(t.Context())
		require.NoError(t, err)
		defer closer2.Close()

		streamToLabels := make(map[int64][2]string)
		for _, sec := range obj2.Sections().Filter(func(s *dataobj.Section) bool {
			return streams.CheckSection(s) && s.Tenant == "t1"
		}) {
			streamSec, err := streams.Open(t.Context(), sec)
			require.NoError(t, err)
			for res := range streams.IterSection(t.Context(), streamSec) {
				val, err := res.Value()
				require.NoError(t, err)
				streamToLabels[val.ID] = [2]string{val.Labels.Get("namespace"), val.Labels.Get("app")}
			}
		}

		type pair [2]string
		seen := make(map[pair]bool)
		var got []pair
		for _, sec := range obj2.Sections().Filter(func(s *dataobj.Section) bool {
			return logs.CheckSection(s) && s.Tenant == "t1"
		}) {
			for res := range iterLogsSection(t, sec) {
				val, err := res.Value()
				require.NoError(t, err)
				p := pair(streamToLabels[val.StreamID])
				if !seen[p] {
					seen[p] = true
					got = append(got, p)
				}
			}
		}
		want := expectedPairsOrder(t, [][2]string{
			{"ns-b", "app-z"}, {"ns-a", "app-z"}, {"ns-a", "app-a"}, {"ns-b", "app-a"},
		}, []string{"label:namespace", "label:app"})
		gotPairs := make([][2]string, len(got))
		for i, p := range got {
			gotPairs[i] = p
		}
		require.Equal(t, want, gotPairs)
	})

	t.Run("assigns stream IDs in sort key order and keeps every stream and record", func(t *testing.T) {
		cfg := makeCfg()
		overrides := tenantOverrides{"t1": {"label:app"}}
		schemaLabels := []string{"label:app"}

		b, err := NewBuilder(cfg, nil, NewBuilderMetrics(), log.NewNopLogger(), overrides)
		require.NoError(t, err)
		sortBuilder, err := NewBuilder(cfg, nil, NewBuilderMetrics(), log.NewNopLogger(), overrides)
		require.NoError(t, err)

		streamsToAppend := []struct {
			app      string
			instance string
		}{
			{app: "zoo", instance: "b"},
			{app: "alpha", instance: "b"},
			{app: "middle", instance: "a"},
			{app: "alpha", instance: "a"},
			{app: "zoo", instance: "a"},
			{app: "middle", instance: "b"},
		}

		for streamIdx, stream := range streamsToAppend {
			entries := make([]push.Entry, 0, 3)
			for i := range 3 {
				ts := now.Add(time.Duration(i) * time.Second)
				entries = append(entries, push.Entry{
					Timestamp: ts,
					Line:      fmt.Sprintf("%s/%s/%d/%s", stream.app, stream.instance, i, strings.Repeat("x", 1024)),
					StructuredMetadata: push.LabelsAdapter{
						{Name: "trace_id", Value: fmt.Sprintf("trace-%d-%d", streamIdx, i)},
					},
				})
			}
			for _, builder := range []*Builder{b, sortBuilder} {
				require.NoError(t, builder.Append("t1", logproto.Stream{
					Labels:  fmt.Sprintf(`{app=%q,instance=%q}`, stream.app, stream.instance),
					Entries: entries,
				}, now))
			}
		}

		obj1, closer1, err := b.flushWithoutLayout()
		require.NoError(t, err)
		defer closer1.Close()
		obj2, closer2, err := sortBuilder.FlushAndSort(t.Context())
		require.NoError(t, err)
		defer closer2.Close()

		type streamSnapshot struct {
			id     int64
			labels labels.Labels
		}
		readStreams := func(t *testing.T, obj *dataobj.Object) []streamSnapshot {
			t.Helper()

			var got []streamSnapshot
			for _, sec := range obj.Sections().Filter(func(s *dataobj.Section) bool {
				return streams.CheckSection(s) && s.Tenant == "t1"
			}) {
				streamSec, err := streams.Open(t.Context(), sec)
				require.NoError(t, err)
				for res := range streams.IterSection(t.Context(), streamSec) {
					stream, err := res.Value()
					require.NoError(t, err)
					got = append(got, streamSnapshot{id: stream.ID, labels: stream.Labels})
				}
			}
			return got
		}

		type logSnapshot struct {
			streamID int64
			labels   labels.Labels
			ts       int64
			line     string
			metadata string
		}
		readLogs := func(t *testing.T, obj *dataobj.Object, streamByID map[int64]labels.Labels) []logSnapshot {
			t.Helper()

			var got []logSnapshot
			for _, sec := range obj.Sections().Filter(func(s *dataobj.Section) bool {
				return logs.CheckSection(s) && s.Tenant == "t1"
			}) {
				for res := range iterLogsSection(t, sec) {
					record, err := res.Value()
					require.NoError(t, err)

					ls, ok := streamByID[record.StreamID]
					require.Truef(t, ok, "record references unknown stream ID %d", record.StreamID)
					got = append(got, logSnapshot{
						streamID: record.StreamID,
						labels:   ls,
						ts:       record.Timestamp.UnixNano(),
						line:     string(record.Line),
						metadata: record.Metadata.String(),
					})
				}
			}
			return got
		}

		streamsBefore := readStreams(t, obj1)
		streamsAfter := readStreams(t, obj2)
		require.Len(t, streamsAfter, len(streamsBefore))

		var beforeLabels, afterLabels []string
		for _, stream := range streamsBefore {
			beforeLabels = append(beforeLabels, stream.labels.String())
		}

		streamIDsAfter := make(map[int64]struct{}, len(streamsAfter))
		for i, stream := range streamsAfter {
			require.Equal(t, int64(i+1), stream.id)
			afterLabels = append(afterLabels, stream.labels.String())
			streamIDsAfter[stream.id] = struct{}{}

			if i > 0 {
				prevSchemaKey, err := ComputeSchemaKey(streamsAfter[i-1].labels, schemaLabels)
				require.NoError(t, err)
				prevKey := streams.DefaultShardLayout().NewSortKey(streamsAfter[i-1].labels, prevSchemaKey)

				currSchemaKey, err := ComputeSchemaKey(stream.labels, schemaLabels)
				require.NoError(t, err)
				currKey := streams.DefaultShardLayout().NewSortKey(stream.labels, currSchemaKey)
				require.LessOrEqual(t, streams.CompareSortKey(prevKey, currKey), 0)
			}
		}
		require.ElementsMatch(t, beforeLabels, afterLabels)

		logsBefore := readLogs(t, obj1, func() map[int64]labels.Labels {
			m := make(map[int64]labels.Labels, len(streamsBefore))
			for _, stream := range streamsBefore {
				m[stream.id] = stream.labels
			}
			return m
		}())
		logsAfter := readLogs(t, obj2, func() map[int64]labels.Labels {
			m := make(map[int64]labels.Labels, len(streamsAfter))
			for _, stream := range streamsAfter {
				m[stream.id] = stream.labels
			}
			return m
		}())

		contentCounts := func(records []logSnapshot) map[string]int {
			counts := make(map[string]int, len(records))
			for _, record := range records {
				key := fmt.Sprintf("%s\x00%d\x00%s\x00%s", record.labels.String(), record.ts, record.line, record.metadata)
				counts[key]++
			}
			return counts
		}
		require.Equal(t, contentCounts(logsBefore), contentCounts(logsAfter))

		for i, record := range logsAfter {
			_, ok := streamIDsAfter[record.streamID]
			require.Truef(t, ok, "record references unknown stream ID %d", record.streamID)

			if i == 0 {
				continue
			}
			prev := logsAfter[i-1]
			prevSchemaKey, err := ComputeSchemaKey(prev.labels, schemaLabels)
			require.NoError(t, err)
			prevKey := streams.DefaultShardLayout().NewSortKey(prev.labels, prevSchemaKey)

			currSchemaKey, err := ComputeSchemaKey(record.labels, schemaLabels)
			require.NoError(t, err)
			currKey := streams.DefaultShardLayout().NewSortKey(record.labels, currSchemaKey)

			switch {
			case streams.CompareSortKey(prevKey, currKey) != 0:
				require.LessOrEqual(t, streams.CompareSortKey(prevKey, currKey), 0)
			case prev.streamID != record.streamID:
				require.LessOrEqual(t, prev.streamID, record.streamID)
			default:
				require.LessOrEqual(t, record.ts, prev.ts)
			}
		}
	})

}

func expectedLabelOrder(t *testing.T, values []string, labelName string, schemaLabels []string) []string {
	t.Helper()
	keys := make([]streams.SortKey, 0, len(values))
	for _, v := range values {
		schemaKey, err := ComputeSchemaKey(labels.FromStrings(labelName, v), schemaLabels)
		require.NoError(t, err)
		k := streams.DefaultShardLayout().NewSortKey(labels.FromStrings(labelName, v), schemaKey)
		keys = append(keys, k)
	}
	slices.SortFunc(keys, streams.CompareSortKey)
	out := make([]string, len(keys))
	for i, k := range keys {
		out[i] = k.Labels.Get(labelName)
	}
	return out
}

func expectedPairsOrder(t *testing.T, values [][2]string, schemaLabels []string) [][2]string {
	t.Helper()
	keys := make([]streams.SortKey, 0, len(values))
	for _, v := range values {
		schemaKey, err := ComputeSchemaKey(labels.FromStrings("namespace", v[0], "app", v[1]), schemaLabels)
		require.NoError(t, err)
		k := streams.DefaultShardLayout().NewSortKey(labels.FromStrings("namespace", v[0], "app", v[1]), schemaKey)
		keys = append(keys, k)
	}
	slices.SortFunc(keys, streams.CompareSortKey)
	out := make([][2]string, len(keys))
	for i, k := range keys {
		out[i] = [2]string{k.Labels.Get("namespace"), k.Labels.Get("app")}
	}
	return out
}

func TestComputeSortKey(t *testing.T) {
	tests := []struct {
		name         string
		labels       labels.Labels
		schemaLabels []string
		want         string
		wantErr      bool
	}{
		{
			name:         "single label",
			labels:       labels.FromStrings("app", "foo"),
			schemaLabels: []string{"label:app"},
			want:         "foo",
		},
		{
			name:         "multiple labels",
			labels:       labels.FromStrings("namespace", "ns", "app", "foo"),
			schemaLabels: []string{"label:namespace", "label:app"},
			want:         "ns\x00foo",
		},
		{
			name:         "missing label",
			labels:       labels.FromStrings("app", "foo"),
			schemaLabels: []string{"label:missing"},
			want:         "",
		},
		{
			name:         "invalid fqn without colon",
			labels:       labels.FromStrings("app", "foo"),
			schemaLabels: []string{"app"},
			wantErr:      true,
		},
		{
			name:         "invalid fqn type",
			labels:       labels.FromStrings("app", "foo"),
			schemaLabels: []string{"metadata:app"},
			wantErr:      true,
		},
		{
			name:         "empty schema labels",
			labels:       labels.FromStrings("app", "foo"),
			schemaLabels: nil,
			want:         "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ComputeSchemaKey(tt.labels, tt.schemaLabels)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func iterLogsSection(t *testing.T, section *dataobj.Section) result.Seq[logs.Record] {
	t.Helper()
	ctx := t.Context()

	return result.Iter(func(yield func(logs.Record) bool) error {
		logsSection, err := logs.Open(ctx, section)
		if err != nil {
			return fmt.Errorf("opening section: %w", err)
		}
		for result := range logs.IterSection(ctx, logsSection) {
			if result.Err() != nil || !yield(result.MustValue()) {
				return result.Err()
			}
		}
		return nil
	})
}

// failingReadStore is a scratch store whose reads always fail. It records the
// handles removed from it so tests can tell that buffered sections were
// released.
type failingReadStore struct {
	inner   scratch.Store
	removed []scratch.Handle
}

func (s *failingReadStore) Put(p []byte) scratch.Handle { return s.inner.Put(p) }

func (s *failingReadStore) Read(scratch.Handle) (io.ReadSeekCloser, error) {
	return nil, errors.New("mock read error")
}

func (s *failingReadStore) Remove(h scratch.Handle) error {
	s.removed = append(s.removed, h)
	return s.inner.Remove(h)
}

func TestBuilder_FlushReturnsNoCloserOnError(t *testing.T) {
	store := &failingReadStore{inner: scratch.NewMemory()}
	builder, err := NewBuilder(testBuilderConfig, store, NewBuilderMetrics(), log.NewNopLogger(), NoTenantOverrides{})
	require.NoError(t, err)
	require.NoError(t, builder.Append("tenant", logproto.Stream{
		Labels:  `{cluster="test",app="foo"}`,
		Entries: []push.Entry{{Timestamp: time.Unix(10, 0).UTC(), Line: "hello"}},
	}, time.Now()))

	obj, closer, err := builder.flushWithoutLayout()
	require.Error(t, err)
	require.Nil(t, obj)
	require.Nil(t, closer)
	require.NotEmpty(t, store.removed, "the buffered sections must be released")
}

func TestBuilder_FlushResetsBuilder(t *testing.T) {
	stream := logproto.Stream{
		Labels:  `{cluster="test",app="foo"}`,
		Entries: []push.Entry{{Timestamp: time.Unix(10, 0).UTC(), Line: "hello"}},
	}

	tests := []struct {
		name    string
		store   scratch.Store
		wantErr string
	}{
		{
			name:  "resets the builder after a successful flush",
			store: scratch.NewMemory(),
		},
		{
			name:    "resets the builder after a failed flush",
			store:   &failingReadStore{inner: scratch.NewMemory()},
			wantErr: "building object",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder, err := NewBuilder(testBuilderConfig, tt.store, NewBuilderMetrics(), log.NewNopLogger(), NoTenantOverrides{})
			require.NoError(t, err)
			require.NoError(t, builder.Append("tenant", stream, time.Now()))

			_, closer, err := builder.flushWithoutLayout()
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
				defer closer.Close()
			}

			require.Zero(t, builder.GetEstimatedSize())
			_, _, err = builder.flushWithoutLayout()
			require.ErrorIs(t, err, ErrBuilderEmpty)
		})
	}
}

// mutableOverrides gives every tenant the same sort schema and shard layout.
// A test can change them between appends.
//
// A nil shardLayout means the default shard layout. The field is a pointer
// because the zero ShardLayout has one bucket, not the default count.
type mutableOverrides struct {
	schema      []string
	shardLayout *streams.ShardLayout
	schemaCalls int
	shardCalls  int
}

func (o *mutableOverrides) SortSchemaLabels(string) []string {
	o.schemaCalls++
	return o.schema
}

func (o *mutableOverrides) DataObjShardLayout(string) streams.ShardLayout {
	o.shardCalls++
	if o.shardLayout == nil {
		return streams.DefaultShardLayout()
	}
	return *o.shardLayout
}

func testFlushAndSortLayoutPerObject(t *testing.T) {
	now := time.Date(2025, time.September, 17, 0, 0, 0, 0, time.UTC)
	appendStreams := func(t *testing.T, b *Builder, overrides *mutableOverrides, changeTo []string) {
		t.Helper()
		for i := range 4 {
			require.NoError(t, b.Append("t1", logproto.Stream{
				Labels:  fmt.Sprintf(`{app="app-%d",cluster="c-%d"}`, i, 3-i),
				Entries: []push.Entry{{Timestamp: now, Line: "line"}},
			}, now))
			if i == 1 && changeTo != nil {
				overrides.schema = changeTo
			}
		}
	}
	requireSchema := func(t *testing.T, obj *dataobj.Object, want []string) {
		t.Helper()
		requireStreamsLayout(t, obj, "t1", streams.NewLayout(streams.DefaultShardLayout(), want))
	}

	t.Run("keeps the sort schema of the open object when the overrides change, and applies the change to the next object", func(t *testing.T) {
		overrides := &mutableOverrides{schema: []string{"label:app"}}
		b, err := NewBuilder(testBuilderConfig, nil, NewBuilderMetrics(), log.NewNopLogger(), overrides)
		require.NoError(t, err)

		appendStreams(t, b, overrides, []string{"label:cluster"})
		require.Equal(t, 1, overrides.schemaCalls, "the builder must read the overrides once per tenant per object")
		first, firstCloser, err := b.FlushAndSort(t.Context())
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, firstCloser.Close()) })
		requireSchema(t, first, []string{"label:app"})
		require.Equal(t, 1, overrides.schemaCalls, "the sort must not read the overrides")

		appendStreams(t, b, overrides, []string{"label:cluster"})
		second, secondCloser, err := b.FlushAndSort(t.Context())
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, secondCloser.Close()) })
		requireSchema(t, second, []string{"label:cluster"})
	})

	t.Run("sorts with the layout of the build when the overrides change before the flush", func(t *testing.T) {
		overrides := &mutableOverrides{schema: []string{"label:app"}}
		b, err := NewBuilder(testBuilderConfig, nil, NewBuilderMetrics(), log.NewNopLogger(), overrides)
		require.NoError(t, err)

		appendStreams(t, b, overrides, nil)
		overrides.schema = []string{"label:cluster"}
		obj, closer, err := b.FlushAndSort(t.Context())
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, closer.Close()) })
		requireSchema(t, obj, []string{"label:app"})
	})

	t.Run("keeps the shard layout of the open object when the overrides change, and applies the change to the next object", func(t *testing.T) {
		first, second := mustShardLayout(t, 8), mustShardLayout(t, 16)
		overrides := &mutableOverrides{schema: []string{"label:app"}, shardLayout: &first}
		b, err := NewBuilder(testBuilderConfig, nil, NewBuilderMetrics(), log.NewNopLogger(), overrides)
		require.NoError(t, err)

		for i := range 4 {
			require.NoError(t, b.Append("t1", logproto.Stream{
				Labels:  fmt.Sprintf(`{app="app-%d"}`, i),
				Entries: []push.Entry{{Timestamp: now, Line: "line"}},
			}, now))
			if i == 1 {
				overrides.shardLayout = &second
			}
		}
		require.Equal(t, 1, overrides.shardCalls, "the builder must read the shard layout once per tenant per object")
		firstObj, firstCloser, err := b.FlushAndSort(t.Context())
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, firstCloser.Close()) })
		requireStreamsLayout(t, firstObj, "t1", streams.NewLayout(first, []string{"label:app"}))
		require.Equal(t, 1, overrides.shardCalls, "the sort must not read the shard layout")

		require.NoError(t, b.Append("t1", logproto.Stream{
			Labels:  `{app="app-0"}`,
			Entries: []push.Entry{{Timestamp: now, Line: "line"}},
		}, now))
		secondObj, secondCloser, err := b.FlushAndSort(t.Context())
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, secondCloser.Close()) })
		requireStreamsLayout(t, secondObj, "t1", streams.NewLayout(second, []string{"label:app"}))
	})
}

func testFlushAndSortErrors(t *testing.T) {
	stream := logproto.Stream{
		Labels:  `{cluster="test",app="foo"}`,
		Entries: []push.Entry{{Timestamp: time.Unix(10, 0).UTC(), Line: "hello"}},
	}

	t.Run("returns ErrBuilderEmpty when nothing was appended", func(t *testing.T) {
		b, err := NewBuilder(testBuilderConfig, nil, NewBuilderMetrics(), log.NewNopLogger(), NoTenantOverrides{})
		require.NoError(t, err)

		obj, closer, err := b.FlushAndSort(t.Context())
		require.ErrorIs(t, err, ErrBuilderEmpty)
		require.Nil(t, obj)
		require.Nil(t, closer)
	})

	t.Run("returns nothing, counts the failure, and resets the builder when the sort fails", func(t *testing.T) {
		metrics := NewBuilderMetrics()
		b, err := NewBuilder(testBuilderConfig, nil, metrics, log.NewNopLogger(), tenantOverrides{"tenant": {"label:app"}})
		require.NoError(t, err)
		require.NoError(t, b.Append("tenant", stream, time.Now()))

		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		obj, closer, err := b.FlushAndSort(ctx)
		require.ErrorIs(t, err, context.Canceled)
		require.Nil(t, obj)
		require.Nil(t, closer)
		require.Equal(t, 1.0, testutil.ToFloat64(metrics.flushFailures))
		require.Zero(t, b.GetEstimatedSize())
		require.Empty(t, b.layouts)
		_, _, err = b.FlushAndSort(t.Context())
		require.ErrorIs(t, err, ErrBuilderEmpty)
	})

	t.Run("returns the sorted object and counts a release failure when releasing the built object fails", func(t *testing.T) {
		metrics := NewBuilderMetrics()
		b, err := NewBuilder(testBuilderConfig, &failingRemoveStore{Store: scratch.NewMemory()}, metrics, log.NewNopLogger(), NoTenantOverrides{})
		require.NoError(t, err)
		require.NoError(t, b.Append("tenant", stream, time.Now()))

		obj, closer, err := b.FlushAndSort(t.Context())
		require.NoError(t, err)
		require.NotNil(t, obj)
		t.Cleanup(func() { _ = closer.Close() })
		require.Equal(t, 1.0, testutil.ToFloat64(metrics.releaseFailures))
		require.Zero(t, testutil.ToFloat64(metrics.flushFailures))
	})

	t.Run("returns nothing and releases the buffered sections when the build fails", func(t *testing.T) {
		store := &failingReadStore{inner: scratch.NewMemory()}
		b, err := NewBuilder(testBuilderConfig, store, NewBuilderMetrics(), log.NewNopLogger(), NoTenantOverrides{})
		require.NoError(t, err)
		require.NoError(t, b.Append("tenant", stream, time.Now()))

		obj, closer, err := b.FlushAndSort(t.Context())
		require.ErrorContains(t, err, "building object")
		require.Nil(t, obj)
		require.Nil(t, closer)
		require.NotEmpty(t, store.removed)
		_, _, err = b.FlushAndSort(t.Context())
		require.ErrorIs(t, err, ErrBuilderEmpty)
	})
}

func TestBuilder_FlushAndSort(t *testing.T) {
	t.Run("sorts the records of every tenant object-wide and keeps every record", testFlushAndSortOrder)
	t.Run("keeps the shard bucket of every stream", testFlushAndSortShardBuckets)
	t.Run("orders streams by the sort schema of each tenant", testFlushAndSortSchema)
	t.Run("reads the layout once per object", testFlushAndSortLayoutPerObject)
	t.Run("returns errors and leaves the builder empty on failure", testFlushAndSortErrors)
}

// failingRemoveStore is a scratch store whose removes always fail.
type failingRemoveStore struct {
	scratch.Store
}

func (s *failingRemoveStore) Remove(h scratch.Handle) error {
	_ = s.Store.Remove(h)
	return errors.New("mock remove error")
}

// requireStreamsLayout checks that the streams section of tenant in obj
// records want.
func requireStreamsLayout(t *testing.T, obj *dataobj.Object, tenant string, want streams.Layout) {
	t.Helper()
	var found int
	for _, sec := range obj.Sections().Filter(func(s *dataobj.Section) bool {
		return streams.CheckSection(s) && s.Tenant == tenant
	}) {
		opened, err := streams.Open(t.Context(), sec)
		require.NoError(t, err)
		got, ok, err := opened.Layout()
		require.NoError(t, err)
		require.True(t, ok, "the streams section must record a layout")
		require.Equal(t, want, got)
		found++
	}
	require.Equal(t, 1, found, "tenant %s must have one streams section", tenant)
}

func TestBuilder_FlushRecordsLayout(t *testing.T) {
	now := time.Date(2025, time.September, 17, 0, 0, 0, 0, time.UTC)
	layouts := tenantLayouts{
		"t1": streams.NewLayout(mustShardLayout(t, 8), []string{"label:app"}),
		"t2": streams.NewLayout(mustShardLayout(t, 16), []string{"label:cluster"}),
	}
	newFilledBuilder := func(t *testing.T) *Builder {
		t.Helper()
		b, err := NewBuilder(testBuilderConfig, nil, NewBuilderMetrics(), log.NewNopLogger(), layouts)
		require.NoError(t, err)
		for _, tenant := range []string{"t1", "t2"} {
			require.NoError(t, b.Append(tenant, logproto.Stream{
				Labels:  `{app="a",cluster="c"}`,
				Entries: []push.Entry{{Timestamp: now, Line: "line"}},
			}, now))
		}
		return b
	}

	t.Run("flushWithoutLayout records no layout in any streams section", func(t *testing.T) {
		obj, closer, err := newFilledBuilder(t).flushWithoutLayout()
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, closer.Close()) })

		require.Equal(t, 2, obj.Sections().Count(streams.CheckSection))
		for _, sec := range obj.Sections().Filter(streams.CheckSection) {
			opened, err := streams.Open(t.Context(), sec)
			require.NoError(t, err)
			_, ok, err := opened.Layout()
			require.NoError(t, err)
			require.False(t, ok)
		}
	})

	t.Run("Flush records the shard layout and the schema labels of each tenant", func(t *testing.T) {
		obj, closer, err := newFilledBuilder(t).Flush()
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, closer.Close()) })

		requireStreamsLayout(t, obj, "t1", layouts["t1"])
		requireStreamsLayout(t, obj, "t2", layouts["t2"])
	})

	t.Run("FlushAndSort records the shard layout and the schema labels of each tenant", func(t *testing.T) {
		obj, closer, err := newFilledBuilder(t).FlushAndSort(t.Context())
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, closer.Close()) })

		requireStreamsLayout(t, obj, "t1", layouts["t1"])
		requireStreamsLayout(t, obj, "t2", layouts["t2"])
	})

	t.Run("Flush keeps the rows in stream ID order when the streams arrive in the sort key order of a non-default shard layout", func(t *testing.T) {
		layout := layouts["t1"]
		sortKey := func(streamLabels labels.Labels) streams.SortKey {
			schemaKey, err := ComputeSchemaKey(streamLabels, layout.SchemaLabels())
			require.NoError(t, err)
			return layout.ShardLayout().NewSortKey(streamLabels, schemaKey)
		}
		var appended []labels.Labels
		for i := range 16 {
			appended = append(appended, labels.FromStrings("app", fmt.Sprintf("app-%d", i), "cluster", "test"))
		}
		slices.SortFunc(appended, func(a, b labels.Labels) int { return streams.CompareSortKey(sortKey(a), sortKey(b)) })

		b, err := NewBuilder(testBuilderConfig, nil, NewBuilderMetrics(), log.NewNopLogger(), layouts)
		require.NoError(t, err)
		for _, streamLabels := range appended {
			require.NoError(t, b.AppendRecord("t1", streamLabels, logs.Record{Timestamp: now, Line: []byte("line")}, now))
		}
		obj, closer, err := b.Flush()
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, closer.Close()) })

		var streamIDs []int64
		for _, sec := range obj.Sections().Filter(logs.CheckSection) {
			for res := range iterLogsSection(t, sec) {
				record, err := res.Value()
				require.NoError(t, err)
				streamIDs = append(streamIDs, record.StreamID)
			}
		}
		require.Len(t, streamIDs, len(appended))
		require.True(t, slices.IsSorted(streamIDs), "rows must follow stream ID order: %v", streamIDs)
	})
}

// tenantLayouts maps tenant IDs to their layout. A tenant that is not in the map has the default
// shard layout and no schema labels.
type tenantLayouts map[string]streams.Layout

func (m tenantLayouts) SortSchemaLabels(tenant string) []string { return m[tenant].SchemaLabels() }

func (m tenantLayouts) DataObjShardLayout(tenant string) streams.ShardLayout {
	layout, ok := m[tenant]
	if !ok {
		return streams.DefaultShardLayout()
	}
	return layout.ShardLayout()
}

// mustShardLayout returns the shard layout for buckets. It fails the test if buckets is not a
// power of two from 1 to 65536.
func mustShardLayout(t *testing.T, buckets uint32) streams.ShardLayout {
	t.Helper()
	layout, err := streams.ShardLayoutFromBuckets(buckets)
	require.NoError(t, err)
	return layout
}
