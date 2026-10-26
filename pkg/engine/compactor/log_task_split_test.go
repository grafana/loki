package compactor

import (
	"testing"

	"github.com/go-kit/log"
	"github.com/stretchr/testify/require"

	v2 "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2"
	compactionv2pb "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2/proto"
)

// splitTestSection returns a section of object path that holds the given
// sizes of service_name values in shard bucket 0.
func splitTestSection(path string, sizes map[string]uint64) v2.Section[logSortPrefix] {
	section := v2.Section[logSortPrefix]{Ref: &compactionv2pb.SectionRef{ObjectPath: path}}
	for service, size := range sizes {
		section.Keys = append(section.Keys, v2.KeySize[logSortPrefix]{Key: newLogSortPrefix(0, []string{service}), Size: size})
		section.Ref.UncompressedSize += int64(size)
	}
	return section
}

func splitTestSpec(runs ...[]v2.Section[logSortPrefix]) *compactionv2pb.TaskSpec {
	spec := &compactionv2pb.TaskSpec{Tenant: "acme"}
	for _, run := range runs {
		ref := &compactionv2pb.RunRef{}
		for _, section := range run {
			ref.Sections = append(ref.Sections, section.Ref)
		}
		spec.Runs = append(spec.Runs, ref)
	}
	return spec
}

func sortPrefixOf(service string) *compactionv2pb.SortPrefix {
	return &compactionv2pb.SortPrefix{ShardBucket: 0, SchemaValues: []string{service}}
}

func TestLogTaskSplitter(t *testing.T) {
	a := splitTestSection("a", map[string]uint64{"auth": 60, "billing": 30})
	b := splitTestSection("b", map[string]uint64{"billing": 30, "cart": 40})
	c := splitTestSection("c", map[string]uint64{"cart": 30, "dns": 70})
	sections := []v2.Section[logSortPrefix]{a, b, c}

	t.Run("returns the task unchanged when the split is disabled", func(t *testing.T) {
		spec := splitTestSpec([]v2.Section[logSortPrefix]{a, b, c})
		got := newLogTaskSplitter(log.NewNopLogger(), 0, sections).split(spec)
		require.Equal(t, []*compactionv2pb.TaskSpec{spec}, got)
	})

	t.Run("returns the task unchanged without a key range when it fits in one task", func(t *testing.T) {
		spec := splitTestSpec([]v2.Section[logSortPrefix]{a, b, c})
		got := newLogTaskSplitter(log.NewNopLogger(), 1000, sections).split(spec)
		require.Equal(t, []*compactionv2pb.TaskSpec{spec}, got)
		require.Nil(t, got[0].KeyRange)
	})

	t.Run("gives a section that crosses a cut to both tasks", func(t *testing.T) {
		spec := splitTestSpec([]v2.Section[logSortPrefix]{a}, []v2.Section[logSortPrefix]{b, c})
		got := newLogTaskSplitter(log.NewNopLogger(), 125, sections).split(spec)

		// auth 60 + billing 60 = 120 fits. cart 70 would exceed 125, so the
		// first cut is at cart. cart 70 + dns 70 = 140 is above 110% of 125,
		// so the second cut is at dns.
		require.Equal(t, []*compactionv2pb.TaskSpec{
			{
				Tenant:   "acme",
				Runs:     []*compactionv2pb.RunRef{{Sections: []*compactionv2pb.SectionRef{a.Ref}}, {Sections: []*compactionv2pb.SectionRef{b.Ref}}},
				KeyRange: &compactionv2pb.KeyRange{End: sortPrefixOf("cart")},
			},
			{
				Tenant:   "acme",
				Runs:     []*compactionv2pb.RunRef{{Sections: []*compactionv2pb.SectionRef{b.Ref, c.Ref}}},
				KeyRange: &compactionv2pb.KeyRange{Start: sortPrefixOf("cart"), End: sortPrefixOf("dns")},
			},
			{
				Tenant:   "acme",
				Runs:     []*compactionv2pb.RunRef{{Sections: []*compactionv2pb.SectionRef{c.Ref}}},
				KeyRange: &compactionv2pb.KeyRange{Start: sortPrefixOf("dns")},
			},
		}, got)
	})

	t.Run("gives a section without per-key sizes to every task", func(t *testing.T) {
		unknown := v2.Section[logSortPrefix]{Ref: &compactionv2pb.SectionRef{ObjectPath: "unknown"}}
		spec := splitTestSpec([]v2.Section[logSortPrefix]{a, c}, []v2.Section[logSortPrefix]{unknown})
		got := newLogTaskSplitter(log.NewNopLogger(), 100, append(sections, unknown)).split(spec)

		require.Len(t, got, 2)
		for _, task := range got {
			require.Contains(t, task.Runs, &compactionv2pb.RunRef{Sections: []*compactionv2pb.SectionRef{unknown.Ref}})
		}
	})
}
