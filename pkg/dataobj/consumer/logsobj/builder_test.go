package logsobj

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

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
		builder, err := NewBuilder(testBuilderConfig, nil)
		require.NoError(t, err)

		for _, entry := range testStreams {
			require.NoError(t, builder.Append("tenant", entry))
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

	builder, err := NewBuilder(testBuilderConfig, nil)
	require.NoError(t, err)

	tenant := "test"

	for {
		require.NoError(t, ctx.Err())

		err := builder.Append(tenant, logproto.Stream{
			Labels: `{cluster="test",app="foo"}`,
			Entries: []push.Entry{{
				Timestamp: time.Now().UTC(),
				Line:      strings.Repeat("a", 1024),
			}},
		})
		if errors.Is(err, ErrBuilderFull) {
			break
		}
		require.NoError(t, err)
	}

	obj, closer, err := builder.Flush()
	require.NoError(t, err)
	defer closer.Close()

	// When a section builder is reset, which happens on ErrBuilderFull, the
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

func TestBuilder_CopyAndSort(t *testing.T) {
	builder, _ := NewBuilder(testBuilderConfig, nil)

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
			})
			require.NoError(t, err)
		}
	}

	obj1, closer1, err := builder.Flush()
	require.NoError(t, err)
	defer closer1.Close()

	newBuilder, _ := NewBuilder(testBuilderConfig, nil)

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
		b, err := NewBuilder(cfg, nil)
		require.NoError(t, err)
		if overrides != nil {
			b.SetOverrides(overrides)
		}
		for _, app := range appVals {
			for i := range 4 {
				require.NoError(t, b.Append(tenant, logproto.Stream{
					Labels: fmt.Sprintf(`{app=%q}`, app),
					Entries: []push.Entry{{
						Timestamp: now.Add(time.Duration(i) * time.Second),
						Line:      fmt.Sprintf("%s-%d", app, i),
					}},
				}))
			}
		}
		obj, closer, err := b.Flush()
		require.NoError(t, err)
		t.Cleanup(func() { closer.Close() })
		return obj
	}

	copyAndSort := func(t *testing.T, cfg BuilderConfig, src *dataobj.Object, overrides TenantOverrides) *dataobj.Object {
		t.Helper()
		b, err := NewBuilder(cfg, nil)
		require.NoError(t, err)
		if overrides != nil {
			b.SetOverrides(overrides)
		}
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

		b, err := NewBuilder(cfg, nil)
		require.NoError(t, err)
		b.SetOverrides(overrides)
		for _, app := range []string{"zoo", "alpha", "middle"} {
			for i := range 4 {
				require.NoError(t, b.Append("schema-tenant", logproto.Stream{
					Labels:  fmt.Sprintf(`{app=%q}`, app),
					Entries: []push.Entry{{Timestamp: now.Add(time.Duration(i) * time.Second), Line: app}},
				}))
				require.NoError(t, b.Append("plain-tenant", logproto.Stream{
					Labels:  fmt.Sprintf(`{app=%q}`, app),
					Entries: []push.Entry{{Timestamp: now.Add(time.Duration(i) * time.Second), Line: app}},
				}))
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
		b, err := NewBuilder(cfg, nil)
		require.NoError(t, err)
		b.SetOverrides(overrides)

		type entry struct{ ns, app string }
		for _, e := range []entry{{"ns-b", "app-z"}, {"ns-a", "app-z"}, {"ns-a", "app-a"}, {"ns-b", "app-a"}} {
			require.NoError(t, b.Append("t1", logproto.Stream{
				Labels:  fmt.Sprintf(`{namespace=%q,app=%q}`, e.ns, e.app),
				Entries: []push.Entry{{Timestamp: now, Line: e.ns + "/" + e.app}},
			}))
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
