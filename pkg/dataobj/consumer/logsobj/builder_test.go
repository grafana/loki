package logsobj

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"strings"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/push"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/result"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
	"github.com/grafana/loki/v3/pkg/logproto"
)

var testBuilderConfig = BuilderConfig{
	BuilderBaseConfig: BuilderBaseConfig{
		TargetPageSize:          2048,
		TargetObjectSize:        1 << 20, // 1 MiB
		TargetSectionSize:       8 << 10, // 8 KiB
		BufferSize:              2048 * 8,
		SectionStripeMergeLimit: 2,
	},
	DataobjSortOrder: sortTimestampDESC,
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
		builder, err := NewBuilder(testBuilderConfig, nil, NewBuilderMetrics(), log.NewNopLogger(), nil)
		require.NoError(t, err)

		for _, entry := range testStreams {
			require.NoError(t, builder.Append("tenant", entry, time.Now()))
		}
		obj, closer, err := builder.Flush()
		require.NoError(t, err)
		defer closer.Close()

		require.Equal(t, 1, obj.Sections().Count(streams.CheckSection))
		require.Equal(t, 1, obj.Sections().Count(logs.CheckSection))
	})
}

// TestBuilder_Append ensures that appending to the buffer eventually reports
// that the buffer is full.
func TestBuilder_Append(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	builder, err := NewBuilder(testBuilderConfig, nil, NewBuilderMetrics(), log.NewNopLogger(), nil)
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

	obj, closer, err := builder.Flush()
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
		builder, err := NewBuilder(testBuilderConfig, nil, NewBuilderMetrics(), log.NewNopLogger(), nil)
		require.NoError(t, err)
		require.True(t, builder.GetEarliestRecordTime().IsZero())
	})

	t.Run("set on first append", func(t *testing.T) {
		builder, err := NewBuilder(testBuilderConfig, nil, NewBuilderMetrics(), log.NewNopLogger(), nil)
		require.NoError(t, err)

		recTime := time.Unix(100, 0).UTC()
		require.NoError(t, builder.Append("tenant", newStream(), recTime))
		require.Equal(t, recTime, builder.GetEarliestRecordTime())
	})

	t.Run("tracks the minimum across appends", func(t *testing.T) {
		builder, err := NewBuilder(testBuilderConfig, nil, NewBuilderMetrics(), log.NewNopLogger(), nil)
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
		builder, err := NewBuilder(testBuilderConfig, nil, NewBuilderMetrics(), log.NewNopLogger(), nil)
		require.NoError(t, err)

		require.NoError(t, builder.Append("tenant", newStream(), time.Unix(100, 0).UTC()))
		builder.Reset()
		require.True(t, builder.GetEarliestRecordTime().IsZero())
	})

	t.Run("reset on Flush", func(t *testing.T) {
		builder, err := NewBuilder(testBuilderConfig, nil, NewBuilderMetrics(), log.NewNopLogger(), nil)
		require.NoError(t, err)

		require.NoError(t, builder.Append("tenant", newStream(), time.Unix(100, 0).UTC()))
		_, closer, err := builder.Flush()
		require.NoError(t, err)
		defer closer.Close()
		require.True(t, builder.GetEarliestRecordTime().IsZero())
	})
}

func TestBuilder_CopyAndSort(t *testing.T) {
	builder, _ := NewBuilder(testBuilderConfig, nil, NewBuilderMetrics(), log.NewNopLogger(), nil)

	now := time.Date(2025, time.September, 17, 0, 0, 0, 0, time.UTC)
	numRows := 16 // 16 rows with 1KiB each line and 8KiB section size ~> 2 logs sections per tenant

	for _, tenant := range []string{"tenant-a", "tenant-b", "tenant-c"} {
		for i := range numRows {
			err := builder.Append(tenant, logproto.Stream{
				Labels: `{cluster="test",app="foo"}`,
				Entries: []push.Entry{{
					Timestamp: now.Add(time.Duration(i%8) * time.Second),
					Line:      strings.Repeat("a", 1024), // 1KiB log line
				}},
			}, now.Add(time.Duration(i%8)*time.Second))
			require.NoError(t, err)
		}
	}

	obj1, closer1, err := builder.Flush()
	require.NoError(t, err)
	defer closer1.Close()

	newBuilder, _ := NewBuilder(testBuilderConfig, nil, NewBuilderMetrics(), log.NewNopLogger(), nil)

	obj2, closer2, err := newBuilder.CopyAndSort(t.Context(), obj1)
	require.NoError(t, err)
	defer closer2.Close()

	for i, obj := range []*dataobj.Object{obj1, obj2} {
		t.Log(" === dataobj", i)
		t.Log("Size:   ", obj.Size())
		t.Log("Tenants:", obj.Tenants())
		for i, section := range obj.Sections() {
			t.Log("Section:", i, section.Tenant, section.Type.String())
		}
	}

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
	for _, tenant := range []string{"tenant-a", "tenant-b", "tenant-c"} {
		prevTs := time.Unix(0, math.MaxInt64)
		for _, sec := range obj2.Sections().Filter(func(s *dataobj.Section) bool {
			return logs.CheckSection(s) && s.Tenant == tenant
		}) {
			for res := range iterLogsSection(t, sec) {
				val, _ := res.Value()
				require.LessOrEqual(t, val.Timestamp, prevTs)
				prevTs = val.Timestamp
			}
		}
	}
}

func BenchmarkBuilder_CopyAndSort(b *testing.B) {
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
	// CopyAndSort is only tested when we are merging multiple input sections.
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
		cfg       BuilderConfig
		overrides TenantOverrides
		labels    func(tenant string, i int) string
	}{
		{
			name: "default",
			cfg: BuilderConfig{
				BuilderBaseConfig: benchBaseCfg,
				DataobjSortOrder:  sortTimestampDESC,
			},
			labels: func(_ string, i int) string {
				app := apps[i%len(apps)]
				inst := instances[i%len(instances)]
				return fmt.Sprintf(`{cluster="test",app=%q,instance=%q}`, app, inst)
			},
		},
		{
			name: "sort_schema",
			cfg: BuilderConfig{
				BuilderBaseConfig:    benchBaseCfg,
				DataobjSortOrder:     sortStreamASC,
				DataobjUseSortSchema: true,
				AppendOrderedEnabled: true,
			},
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

			dataSize := numRows * sizeOfRow * len(tenants)
			b.SetBytes(int64(dataSize))

			obj1, closer1, err := builder.Flush()
			require.NoError(b, err)
			defer closer1.Close()

			newBuilder, _ := NewBuilder(bc.cfg, nil, NewBuilderMetrics(), log.NewNopLogger(), bc.overrides)

			b.Logf("sections=%d, size=%d", obj1.Sections().Count(logs.CheckSection), obj1.Size())
			b.ResetTimer()
			for b.Loop() {
				newBuilder.Reset()
				_, closer2, err := newBuilder.CopyAndSort(b.Context(), obj1)
				require.NoError(b, err)
				defer closer2.Close()
			}
		})
	}
}

// tenantOverrides maps tenant IDs to their schema labels.
type tenantOverrides map[string][]string

func (m tenantOverrides) SortSchemaLabels(tenant string) []string { return m[tenant] }

func TestBuilder_CopyAndSort_SortSchema(t *testing.T) {
	now := time.Date(2025, time.September, 17, 0, 0, 0, 0, time.UTC)

	makeCfg := func(useSortSchema bool) BuilderConfig {
		return BuilderConfig{
			BuilderBaseConfig: BuilderBaseConfig{
				TargetPageSize:          2048,
				TargetObjectSize:        1 << 20,
				TargetSectionSize:       8 << 10,
				BufferSize:              2048 * 8,
				SectionStripeMergeLimit: 2,
			},
			DataobjSortOrder:     sortTimestampDESC,
			DataobjUseSortSchema: useSortSchema,
		}
	}

	buildObj := func(t *testing.T, cfg BuilderConfig, tenant string, appVals []string, overrides TenantOverrides) *dataobj.Object {
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
		obj, closer, err := b.Flush()
		require.NoError(t, err)
		t.Cleanup(func() { closer.Close() })
		return obj
	}

	copyAndSort := func(t *testing.T, cfg BuilderConfig, src *dataobj.Object, overrides TenantOverrides) *dataobj.Object {
		t.Helper()
		b, err := NewBuilder(cfg, nil, NewBuilderMetrics(), log.NewNopLogger(), overrides)
		require.NoError(t, err)
		obj, closer, err := b.CopyAndSort(t.Context(), src)
		require.NoError(t, err)
		t.Cleanup(func() { closer.Close() })
		return obj
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

	t.Run("schema tenant sorted by label, plain tenant by DataobjSortOrder", func(t *testing.T) {
		cfg := makeCfg(true)
		overrides := tenantOverrides{"schema-tenant": {"label:app"}}

		b, err := NewBuilder(cfg, nil, NewBuilderMetrics(), log.NewNopLogger(), overrides)
		require.NoError(t, err)
		for _, app := range []string{"zoo", "alpha", "middle"} {
			for i := range 4 {
				require.NoError(t, b.Append("schema-tenant", logproto.Stream{
					Labels:  fmt.Sprintf(`{app=%q}`, app),
					Entries: []push.Entry{{Timestamp: now.Add(time.Duration(i) * time.Second), Line: app}},
				}, now.Add(time.Duration(i)*time.Second)))
				require.NoError(t, b.Append("plain-tenant", logproto.Stream{
					Labels:  fmt.Sprintf(`{app=%q}`, app),
					Entries: []push.Entry{{Timestamp: now.Add(time.Duration(i) * time.Second), Line: app}},
				}, now.Add(time.Duration(i)*time.Second)))
			}
		}
		obj1, closer1, err := b.Flush()
		require.NoError(t, err)
		defer closer1.Close()

		obj2 := copyAndSort(t, cfg, obj1, overrides)

		require.Equal(t, []string{"alpha", "middle", "zoo"}, appOrder(t, obj2, "schema-tenant"))
		timestampsDescWithinGroups(t, obj2, "schema-tenant")

		var allTS []time.Time
		for _, sec := range obj2.Sections().Filter(func(s *dataobj.Section) bool {
			return logs.CheckSection(s) && s.Tenant == "plain-tenant"
		}) {
			for res := range iterLogsSection(t, sec) {
				val, err := res.Value()
				require.NoError(t, err)
				allTS = append(allTS, val.Timestamp)
			}
		}
		for i := 1; i < len(allTS); i++ {
			require.LessOrEqual(t, allTS[i].UnixNano(), allTS[i-1].UnixNano(),
				"plain-tenant must be timestamp DESC at index %d", i)
		}
	})

	t.Run("schema labels persisted in section metadata", func(t *testing.T) {
		cfg := makeCfg(true)
		overrides := tenantOverrides{"t1": {"label:app"}}
		obj1 := buildObj(t, cfg, "t1", []string{"b", "a"}, overrides)
		obj2 := copyAndSort(t, cfg, obj1, overrides)

		for _, sec := range obj2.Sections().Filter(func(s *dataobj.Section) bool {
			return logs.CheckSection(s) && s.Tenant == "t1"
		}) {
			logsSection, err := logs.Open(t.Context(), sec)
			require.NoError(t, err)
			labels, err := logsSection.SchemaLabels()
			require.NoError(t, err)
			require.Equal(t, []string{"label:app"}, labels, "SchemaLabels must be persisted in section metadata")
		}

		require.Equal(t, []string{"a", "b"}, appOrder(t, obj2, "t1"))
		timestampsDescWithinGroups(t, obj2, "t1")
	})

	t.Run("initial flush emits schema-ordered logs sections", func(t *testing.T) {
		cfg := makeCfg(true)
		overrides := tenantOverrides{"t1": {"label:app"}}
		obj := buildObj(t, cfg, "t1", []string{"zoo", "alpha", "middle"}, overrides)

		streamToApp := make(map[int64]string)
		for _, sec := range obj.Sections().Filter(func(s *dataobj.Section) bool {
			return streams.CheckSection(s) && s.Tenant == "t1"
		}) {
			streamSec, err := streams.Open(t.Context(), sec)
			require.NoError(t, err)
			for res := range streams.IterSection(t.Context(), streamSec) {
				val, err := res.Value()
				require.NoError(t, err)
				streamToApp[val.ID] = val.Labels.Get("app")
			}
		}

		for _, sec := range obj.Sections().Filter(func(s *dataobj.Section) bool {
			return logs.CheckSection(s) && s.Tenant == "t1"
		}) {
			logsSection, err := logs.Open(t.Context(), sec)
			require.NoError(t, err)
			schemaLabels, err := logsSection.SchemaLabels()
			require.NoError(t, err)
			require.Equal(t, []string{"label:app"}, schemaLabels)

			var (
				prevApp      string
				prevStreamID int64
				prevTS       time.Time
				firstRecord  = true
			)
			for res := range iterLogsSection(t, sec) {
				val, err := res.Value()
				require.NoError(t, err)
				app := streamToApp[val.StreamID]
				if !firstRecord {
					switch {
					case app != prevApp:
						require.GreaterOrEqual(t, app, prevApp)
					case val.StreamID != prevStreamID:
						require.GreaterOrEqual(t, val.StreamID, prevStreamID)
					default:
						require.LessOrEqual(t, val.Timestamp.UnixNano(), prevTS.UnixNano())
					}
				}
				prevApp = app
				prevStreamID = val.StreamID
				prevTS = val.Timestamp
				firstRecord = false
			}
		}
	})

	t.Run("idempotent: second CopyAndSort reads schema from metadata, not overrides", func(t *testing.T) {
		cfg := makeCfg(true)
		overrides := tenantOverrides{"t1": {"label:app"}}
		obj1 := buildObj(t, cfg, "t1", []string{"zoo", "alpha", "middle"}, overrides)
		obj2 := copyAndSort(t, cfg, obj1, overrides)
		obj3 := copyAndSort(t, cfg, obj2, nil)

		require.Equal(t, []string{"alpha", "middle", "zoo"}, appOrder(t, obj3, "t1"),
			"second CopyAndSort must preserve schema sort using persisted metadata")
		timestampsDescWithinGroups(t, obj3, "t1")
	})

	t.Run("multi-label compound key", func(t *testing.T) {
		cfg := makeCfg(true)
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
		obj1, closer1, err := b.Flush()
		require.NoError(t, err)
		defer closer1.Close()

		obj2 := copyAndSort(t, cfg, obj1, overrides)

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
		require.Equal(t, []pair{{"ns-a", "app-a"}, {"ns-a", "app-z"}, {"ns-b", "app-a"}, {"ns-b", "app-z"}}, got)
	})

	t.Run("stream IDs remapped to sort key order", func(t *testing.T) {
		cfg := makeCfg(true)
		overrides := tenantOverrides{"t1": {"label:app"}}
		schemaLabels := []string{"label:app"}

		b, err := NewBuilder(cfg, nil, NewBuilderMetrics(), log.NewNopLogger(), overrides)
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
			require.NoError(t, b.Append("t1", logproto.Stream{
				Labels:  fmt.Sprintf(`{app=%q,instance=%q}`, stream.app, stream.instance),
				Entries: entries,
			}, now))
		}

		obj1, closer1, err := b.Flush()
		require.NoError(t, err)
		defer closer1.Close()

		obj2 := copyAndSort(t, cfg, obj1, overrides)

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
				prevKey, err := computeSortKey(streamsAfter[i-1].labels, schemaLabels)
				require.NoError(t, err)
				currKey, err := computeSortKey(stream.labels, schemaLabels)
				require.NoError(t, err)
				require.LessOrEqual(t, prevKey, currKey)
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
			prevKey, err := computeSortKey(prev.labels, schemaLabels)
			require.NoError(t, err)
			currKey, err := computeSortKey(record.labels, schemaLabels)
			require.NoError(t, err)

			switch {
			case prevKey != currKey:
				require.LessOrEqual(t, prevKey, currKey)
			case prev.streamID != record.streamID:
				require.LessOrEqual(t, prev.streamID, record.streamID)
			default:
				require.LessOrEqual(t, record.ts, prev.ts)
			}
		}
	})

	t.Run("flag off: schema config ignored, DataobjSortOrder used", func(t *testing.T) {
		cfg := makeCfg(false)
		overrides := tenantOverrides{"t1": {"label:app"}}
		obj1 := buildObj(t, cfg, "t1", []string{"zoo", "alpha", "middle"}, overrides)
		obj2 := copyAndSort(t, cfg, obj1, overrides)

		var allTS []time.Time
		for _, sec := range obj2.Sections().Filter(func(s *dataobj.Section) bool {
			return logs.CheckSection(s) && s.Tenant == "t1"
		}) {
			for res := range iterLogsSection(t, sec) {
				val, err := res.Value()
				require.NoError(t, err)
				allTS = append(allTS, val.Timestamp)
			}
		}
		for i := 1; i < len(allTS); i++ {
			require.LessOrEqual(t, allTS[i].UnixNano(), allTS[i-1].UnixNano(),
				"with flag off, order must be timestamp DESC at index %d", i)
		}

		for _, sec := range obj2.Sections().Filter(func(s *dataobj.Section) bool {
			return logs.CheckSection(s) && s.Tenant == "t1"
		}) {
			logsSection, err := logs.Open(t.Context(), sec)
			require.NoError(t, err)
			schemaLabels, err := logsSection.SchemaLabels()
			require.NoError(t, err)
			require.Empty(t, schemaLabels, "SchemaLabels must be absent when flag is off")
		}
	})
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
			got, err := computeSortKey(tt.labels, tt.schemaLabels)
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
