package compactor

import (
	"testing"

	"github.com/go-kit/log"
	"github.com/stretchr/testify/require"

	v2 "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2"
	compactionv2pb "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2/proto"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
)

func labelSortKey(column string) indexSortKey {
	return indexSortKey{kind: postings.KindLabel, columnName: column}
}

func labelPostingKey(column string) postings.Key {
	return postings.Key{Kind: postings.KindLabel, ColumnName: column}
}

// testIndexSection returns a planner section at path whose samples are keys,
// each of size bytes. Its bounds are the first and last of keys.
func testIndexSection(t *testing.T, path string, size uint64, keys ...string) v2.Section[indexSortKey] {
	t.Helper()
	require.NotEmpty(t, keys)
	section := v2.Section[indexSortKey]{
		Ref: &compactionv2pb.SectionRef{ObjectPath: path},
		Min: labelSortKey(keys[0]),
		Max: labelSortKey(keys[len(keys)-1]),
	}
	for _, key := range keys {
		section.Keys = append(section.Keys, v2.KeySize[indexSortKey]{Key: labelSortKey(key), UncompressedSize: size})
	}
	return section
}

func TestNoopIndexTaskSplitter(t *testing.T) {
	t.Run("returns the task whole over every key", func(t *testing.T) {
		spec := &compactionv2pb.TaskSpec{Tenant: "acme"}
		tasks, err := noopIndexTaskSplitter{}.split(spec)
		require.NoError(t, err)
		require.Equal(t, []indexMergeTask{{spec: spec}}, tasks)
	})
}

func TestSizeIndexTaskSplitter(t *testing.T) {
	a1 := testIndexSection(t, "index/a1", 100, "app", "env")
	a2 := testIndexSection(t, "index/a2", 100, "zone")
	b1 := testIndexSection(t, "index/b1", 100, "app", "zone")
	c1 := testIndexSection(t, "index/c1", 100, "zone")
	d1 := testIndexSection(t, "index/d1", 100, "app")
	all := []v2.Section[indexSortKey]{a1, a2, b1, c1, d1}
	spec := &compactionv2pb.TaskSpec{Tenant: "acme", Runs: []*compactionv2pb.RunRef{
		{Sections: []*compactionv2pb.SectionRef{a1.Ref, a2.Ref}},
		{Sections: []*compactionv2pb.SectionRef{b1.Ref}},
		{Sections: []*compactionv2pb.SectionRef{c1.Ref}},
		{Sections: []*compactionv2pb.SectionRef{d1.Ref}},
	}}
	splitter := func(sections []v2.Section[indexSortKey], maxSize uint64) sizeIndexTaskSplitter {
		return newSizeIndexTaskSplitter(log.NewNopLogger(), maxSize, sections)
	}
	runsOf := func(refs ...*compactionv2pb.SectionRef) *compactionv2pb.TaskSpec {
		pruned := &compactionv2pb.TaskSpec{Tenant: "acme"}
		for _, ref := range refs {
			pruned.Runs = append(pruned.Runs, &compactionv2pb.RunRef{Sections: []*compactionv2pb.SectionRef{ref}})
		}
		return pruned
	}

	t.Run("panics when the maximum task size is 0", func(t *testing.T) {
		require.Panics(t, func() { splitter(all, 0) })
	})

	t.Run("returns one task over every key when the merge fits in the maximum task size", func(t *testing.T) {
		tasks, err := splitter(all, 1000).split(spec)
		require.NoError(t, err)
		require.Equal(t, []indexMergeTask{{spec: spec}}, tasks)
	})

	t.Run("splits at sampled keys, keeps the overlapping sections of each task and gives each source's stats to one reading task", func(t *testing.T) {
		tasks, err := splitter(all, 200).split(spec)
		require.NoError(t, err)

		envKey, zoneKey := labelPostingKey("env"), labelPostingKey("zone")
		span := func(start, end *postings.Key) postings.KeySpan {
			keySpan, err := postings.NewKeySpan(start, end)
			require.NoError(t, err)
			return keySpan
		}
		require.Equal(t, []indexMergeTask{
			{spec: runsOf(a1.Ref, b1.Ref, d1.Ref), keySpan: span(nil, &envKey), statsSources: []string{"index/a1", "index/d1"}},
			{spec: runsOf(a1.Ref, b1.Ref), keySpan: span(&envKey, &zoneKey), statsSources: []string{"index/b1"}},
			{spec: &compactionv2pb.TaskSpec{Tenant: "acme", Runs: []*compactionv2pb.RunRef{
				{Sections: []*compactionv2pb.SectionRef{a2.Ref}},
				{Sections: []*compactionv2pb.SectionRef{b1.Ref}},
				{Sections: []*compactionv2pb.SectionRef{c1.Ref}},
			}}, keySpan: span(&zoneKey, nil), statsSources: []string{"index/a2", "index/c1"}},
		}, tasks)
	})

	t.Run("returns an error when the task references a section without planner data", func(t *testing.T) {
		_, err := splitter([]v2.Section[indexSortKey]{a1}, 200).split(spec)
		require.ErrorContains(t, err, "unknown index section")
	})

	t.Run("returns an error when a section holds a kind at the end of the split", func(t *testing.T) {
		bad := a1
		bad.Keys = []v2.KeySize[indexSortKey]{{Key: indexSortKey{kind: 255}, UncompressedSize: 100}}
		_, err := splitter([]v2.Section[indexSortKey]{bad, a2, b1, c1, d1}, 200).split(spec)
		require.ErrorContains(t, err, "posting kind 255")
	})
}

func TestSplitIndexMerge_Validate(t *testing.T) {
	envKey := labelPostingKey("env")
	span := func(t *testing.T, start, end *postings.Key) postings.KeySpan {
		t.Helper()
		keySpan, err := postings.NewKeySpan(start, end)
		require.NoError(t, err)
		return keySpan
	}

	t.Run("accepts tasks whose key spans hold every key once", func(t *testing.T) {
		merge := splitIndexMerge{tasks: []indexMergeTask{{keySpan: span(t, nil, &envKey)}, {keySpan: span(t, &envKey, nil)}}}
		require.NoError(t, merge.validate())
	})

	t.Run("returns an error when the key spans of the tasks leave a gap", func(t *testing.T) {
		appKey := labelPostingKey("app")
		merge := splitIndexMerge{tasks: []indexMergeTask{{keySpan: span(t, nil, &appKey)}, {keySpan: span(t, &envKey, nil)}}}
		require.ErrorContains(t, merge.validate(), "do not hold every posting key once")
	})
}
