package logsobj

import (
	"cmp"
	"fmt"
	"math/rand"
	"strings"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/push"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
	"github.com/grafana/loki/v3/pkg/logproto"
)

// TestBuilder_EndToEnd is a sanity check for the full builder pipeline:
// Append -> Flush (intermediate object) -> CopyAndSort (final object).
//
// The dataset is randomly generated with a fixed seed to broadly exercise:
// - intermediate log sections are individually ordered.
// - sort merge of the intermediate log sections and object-wide ordering.
// - stream ID mapping to ensure they are consistent with the sort key order.
//
// TODO(ashwanth): SortSchemaASC does not support AppendUnordered because
// stripe merging cannot use schema-based ordering. Once that is fixed, this
// test should be updated to also run with AppendOrderedEnabled=false for
// schema sort cases.
func TestBuilder_EndToEnd(t *testing.T) {
	tenants := []string{"t1", "t2", "t10"}
	apps := []string{"zoo", "alpha", "middle"}
	instances := []string{"b", "a"}
	schemaLabels := []string{"label:app"}

	// Local config: bigger sections so each holds many records with real
	// internal disorder. AppendOrdered is used because SortSchemaASC does
	// not yet support stripe merging (AppendUnordered).
	baseCfg := BuilderConfig{
		BuilderBaseConfig: BuilderBaseConfig{
			TargetPageSize:          2048,
			TargetObjectSize:        1 << 20,
			TargetSectionSize:       64 << 10, // 64 KiB
			BufferSize:              16 << 10, // 16 KiB
			SectionStripeMergeLimit: 2,
		},
		AppendOrderedEnabled: true,
	}

	makeOverrides := func() TenantOverrides {
		overrides := tenantOverrides{}
		for _, tenant := range tenants {
			overrides[tenant] = schemaLabels
		}
		return overrides
	}

	appendDataset := func(t *testing.T, b *Builder) {
		t.Helper()

		rng := rand.New(rand.NewSource(42))
		refTime := time.Date(2025, time.September, 17, 12, 0, 0, 0, time.UTC)

		// 1000 entries across 3 tenants (~333 per tenant) with ~1 KiB lines
		// and 64 KiB sections gives ~5 sections per tenant.
		const numEntries = 1000

		for i := range numEntries {
			tenant := tenants[rng.Intn(len(tenants))]
			app := apps[rng.Intn(len(apps))]
			instance := instances[rng.Intn(len(instances))]
			// [-64s, +63s] window around refTime; narrow enough to guarantee timestamp ties.
			ts := refTime.Add(time.Duration(rng.Intn(128)-64) * time.Second)

			md := push.LabelsAdapter{
				{Name: "trace_id", Value: fmt.Sprintf("trace-%04d", i)},
			}
			if rng.Intn(4) == 0 {
				md = append(md, push.LabelAdapter{Name: "span_id", Value: fmt.Sprintf("span-%04d", i)})
			}

			require.NoError(t, b.Append(tenant, logproto.Stream{
				Labels: fmt.Sprintf(`{app=%q,instance=%q}`, app, instance),
				Entries: []push.Entry{{
					Timestamp:          ts,
					Line:               builderE2ELine(tenant, app, instance, i),
					StructuredMetadata: md,
				}},
			}, ts))
		}
	}

	for _, tc := range []struct {
		name          string
		sortOrder     string
		useSortSchema bool
	}{
		{name: "SortOrderStreamASC", sortOrder: sortStreamASC},
		{name: "SortOrderTimestampDESC", sortOrder: sortTimestampDESC},
		{name: "UseSchemaSort", sortOrder: sortStreamASC, useSortSchema: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := baseCfg
			cfg.DataobjSortOrder = tc.sortOrder
			cfg.DataobjUseSortSchema = tc.useSortSchema

			overrides := makeOverrides()
			var expectedSchemaLabels []string
			if tc.useSortSchema {
				expectedSchemaLabels = schemaLabels
			}

			b, err := NewBuilder(cfg, nil, NewBuilderMetrics(), log.NewNopLogger(), overrides)
			require.NoError(t, err)
			appendDataset(t, b)

			obj1, closer, err := b.Flush()
			require.NoError(t, err)
			defer closer.Close()

			intermediateByTenant := make(map[string][]builderE2EResolvedRecord, len(tenants))
			for _, tenant := range tenants {
				require.Equal(t, 1, countTenantSections(obj1, tenant, streams.CheckSection))
				require.Greater(t, countTenantSections(obj1, tenant, logs.CheckSection), 1)

				assertBuilderE2ESchemaLabels(t, obj1, tenant, expectedSchemaLabels)

				for _, sec := range obj1.Sections().Filter(func(s *dataobj.Section) bool {
					return logs.CheckSection(s) && s.Tenant == tenant
				}) {
					// logs are ordered within each section.
					assertBuilderE2EOrdered(t, resolveTenantLogsSection(t, obj1, tenant, sec), tc.sortOrder, expectedSchemaLabels)
				}
				intermediateByTenant[tenant] = resolveTenantLogs(t, obj1, tenant)
			}

			mergeBuilder, err := NewBuilder(cfg, nil, NewBuilderMetrics(), log.NewNopLogger(), overrides)
			require.NoError(t, err)
			obj2, closer2, err := mergeBuilder.CopyAndSort(t.Context(), obj1)
			require.NoError(t, err)
			defer closer2.Close()

			require.ElementsMatch(t, obj1.Tenants(), obj2.Tenants())
			for _, tenant := range tenants {
				require.Equal(t, 1, countTenantSections(obj2, tenant, streams.CheckSection))
				require.Greater(t, countTenantSections(obj2, tenant, logs.CheckSection), 1)
				assertBuilderE2ESchemaLabels(t, obj2, tenant, expectedSchemaLabels)

				got := resolveTenantLogs(t, obj2, tenant)
				assertBuilderE2ESameContent(t, intermediateByTenant[tenant], got)
				assertBuilderE2EOrdered(t, got, tc.sortOrder, expectedSchemaLabels)
			}
		})
	}
}

type builderE2EResolvedRecord struct {
	streamID int64
	labels   labels.Labels
	ts       int64
	line     string
	metadata string
}

func builderE2ELine(tenant, app, instance string, i int) string {
	switch {
	case i%7 == 0:
		return ""
	case i%5 == 0:
		return "short"
	default:
		prefix := fmt.Sprintf("%s/%s/%s/%02d/", tenant, app, instance, i)
		return prefix + strings.Repeat("x", 1024-len(prefix))
	}
}

func resolveTenantLogs(t *testing.T, obj *dataobj.Object, tenant string) []builderE2EResolvedRecord {
	t.Helper()
	streamLabels := tenantStreamLabels(t, obj, tenant)

	var records []builderE2EResolvedRecord
	for _, sec := range obj.Sections().Filter(func(s *dataobj.Section) bool {
		return logs.CheckSection(s) && s.Tenant == tenant
	}) {
		records = append(records, resolveLogsSection(t, streamLabels, sec)...)
	}
	return records
}

func resolveTenantLogsSection(t *testing.T, obj *dataobj.Object, tenant string, sec *dataobj.Section) []builderE2EResolvedRecord {
	t.Helper()
	return resolveLogsSection(t, tenantStreamLabels(t, obj, tenant), sec)
}

func tenantStreamLabels(t *testing.T, obj *dataobj.Object, tenant string) map[int64]labels.Labels {
	t.Helper()
	streamLabels := make(map[int64]labels.Labels)
	for _, sec := range obj.Sections().Filter(func(s *dataobj.Section) bool {
		return streams.CheckSection(s) && s.Tenant == tenant
	}) {
		streamSection, err := streams.Open(t.Context(), sec)
		require.NoError(t, err)
		for res := range streams.IterSection(t.Context(), streamSection) {
			stream, err := res.Value()
			require.NoError(t, err)
			streamLabels[stream.ID] = stream.Labels
		}
	}
	return streamLabels
}

func resolveLogsSection(t *testing.T, streamLabels map[int64]labels.Labels, sec *dataobj.Section) []builderE2EResolvedRecord {
	t.Helper()
	var records []builderE2EResolvedRecord
	for res := range iterLogsSection(t, sec) {
		record, err := res.Value()
		require.NoError(t, err)

		ls, ok := streamLabels[record.StreamID]
		require.Truef(t, ok, "record references unknown stream ID %d", record.StreamID)
		records = append(records, builderE2EResolvedRecord{
			streamID: record.StreamID,
			labels:   ls,
			ts:       record.Timestamp.UnixNano(),
			line:     string(record.Line),
			metadata: record.Metadata.String(),
		})
	}
	return records
}

func assertBuilderE2ESameContent(t *testing.T, want, got []builderE2EResolvedRecord) {
	t.Helper()
	require.Equal(t, contentCounts(want), contentCounts(got))
}

func contentCounts(records []builderE2EResolvedRecord) map[string]int {
	counts := make(map[string]int, len(records))
	for _, record := range records {
		key := fmt.Sprintf("%s\x00%d\x00%s\x00%s", record.labels.String(), record.ts, record.line, record.metadata)
		counts[key]++
	}
	return counts
}

func assertBuilderE2EOrdered(t *testing.T, records []builderE2EResolvedRecord, sortOrder string, schemaLabels []string) {
	t.Helper()
	for i := 1; i < len(records); i++ {
		require.LessOrEqualf(t, compareRecords(t, records[i-1], records[i], sortOrder, schemaLabels), 0,
			"records out of order at index %d: %+v then %+v", i, records[i-1], records[i])
	}
}

func compareRecords(t *testing.T, a, b builderE2EResolvedRecord, sortOrder string, schemaLabels []string) int {
	t.Helper()

	if len(schemaLabels) > 0 {
		aKey, err := computeSortKey(a.labels, schemaLabels)
		require.NoError(t, err)

		bKey, err := computeSortKey(b.labels, schemaLabels)
		require.NoError(t, err)

		if res := cmp.Compare(aKey, bKey); res != 0 {
			return res
		}
		if res := cmp.Compare(a.streamID, b.streamID); res != 0 {
			return res
		}
		return cmp.Compare(b.ts, a.ts)
	} else if sortOrder == sortStreamASC {

		if res := cmp.Compare(a.streamID, b.streamID); res != 0 {
			return res
		}
		return cmp.Compare(b.ts, a.ts)
	} else if sortOrder == sortTimestampDESC {
		if res := cmp.Compare(b.ts, a.ts); res != 0 {
			return res
		}
		return cmp.Compare(a.streamID, b.streamID)
	}

	t.Fatalf("unknown sort order: %q", sortOrder)
	return 0
}

func countTenantSections(obj *dataobj.Object, tenant string, check func(*dataobj.Section) bool) int {
	count := 0
	for _, sec := range obj.Sections().Filter(func(s *dataobj.Section) bool {
		return check(s) && s.Tenant == tenant
	}) {
		if sec != nil {
			count++
		}
	}
	return count
}

func assertBuilderE2ESchemaLabels(t *testing.T, obj *dataobj.Object, tenant string, want []string) {
	t.Helper()
	for _, sec := range obj.Sections().Filter(func(s *dataobj.Section) bool {
		return logs.CheckSection(s) && s.Tenant == tenant
	}) {
		logsSection, err := logs.Open(t.Context(), sec)
		require.NoError(t, err)
		got, err := logsSection.SchemaLabels()
		require.NoError(t, err)
		require.Equal(t, want, got)
	}
}
