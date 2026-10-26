package compactor

import (
	"bytes"
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

var (
	fullKeyRange         = v2.FullKeyRangeProto()
	sortPrefixLimitProto = fullKeyRange.End
)

func TestLogTaskSplitter(t *testing.T) {
	a := splitTestSection("a", map[string]uint64{"auth": 60, "billing": 30})
	b := splitTestSection("b", map[string]uint64{"billing": 30, "cart": 40})
	c := splitTestSection("c", map[string]uint64{"cart": 30, "dns": 70})
	sections := []v2.Section[logSortPrefix]{a, b, c}

	t.Run("panics when the maximum task size is 0", func(t *testing.T) {
		require.Panics(t, func() { newLogTaskSplitter(log.NewNopLogger(), 0, sections) })
	})

	t.Run("returns the task with the full key range when it fits in one task", func(t *testing.T) {
		spec := splitTestSpec([]v2.Section[logSortPrefix]{a, b, c})
		got := newLogTaskSplitter(log.NewNopLogger(), 1000, sections).split(spec)
		require.Equal(t, []*compactionv2pb.TaskSpec{spec}, got)
		require.Equal(t, fullKeyRange, got[0].KeyRange)
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
				KeyRange: &compactionv2pb.KeyRange{Start: sortPrefixOf("auth"), End: sortPrefixOf("cart")},
			},
			{
				Tenant:   "acme",
				Runs:     []*compactionv2pb.RunRef{{Sections: []*compactionv2pb.SectionRef{b.Ref, c.Ref}}},
				KeyRange: &compactionv2pb.KeyRange{Start: sortPrefixOf("cart"), End: sortPrefixOf("dns")},
			},
			{
				Tenant:   "acme",
				Runs:     []*compactionv2pb.RunRef{{Sections: []*compactionv2pb.SectionRef{c.Ref}}},
				KeyRange: &compactionv2pb.KeyRange{Start: sortPrefixOf("dns"), End: sortPrefixLimitProto},
			},
		}, got)
	})

	t.Run("finds the sizes of sections that the task refers to by copies of their refs", func(t *testing.T) {
		spec := splitTestSpec([]v2.Section[logSortPrefix]{a}, []v2.Section[logSortPrefix]{b, c})
		for _, run := range spec.Runs {
			for i, ref := range run.Sections {
				clone := *ref
				run.Sections[i] = &clone
			}
		}
		got := newLogTaskSplitter(log.NewNopLogger(), 125, sections).split(spec)
		require.Len(t, got, 3)
	})

	t.Run("warns about a sort prefix above the maximum task size", func(t *testing.T) {
		huge := splitTestSection("huge", map[string]uint64{"auth": 500})
		small := splitTestSection("small", map[string]uint64{"billing": 10})
		var buf bytes.Buffer
		newLogTaskSplitter(log.NewLogfmtLogger(&buf), 100, []v2.Section[logSortPrefix]{huge, small}).
			split(splitTestSpec([]v2.Section[logSortPrefix]{huge}, []v2.Section[logSortPrefix]{small}))

		require.Contains(t, buf.String(), "sort prefix is larger than the maximum task size")
		require.Contains(t, buf.String(), "sort_prefix=auth")
	})

	t.Run("does not warn when the last range exceeds the maximum task size after a merge", func(t *testing.T) {
		x := splitTestSection("x", map[string]uint64{"auth": 60, "billing": 40})
		y := splitTestSection("y", map[string]uint64{"cart": 5})
		var buf bytes.Buffer
		got := newLogTaskSplitter(log.NewLogfmtLogger(&buf), 100, []v2.Section[logSortPrefix]{x, y}).
			split(splitTestSpec([]v2.Section[logSortPrefix]{x}, []v2.Section[logSortPrefix]{y}))

		require.Len(t, got, 1)
		require.Empty(t, buf.String())
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
