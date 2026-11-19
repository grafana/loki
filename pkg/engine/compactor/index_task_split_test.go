package compactor

import (
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/stretchr/testify/require"

	v2 "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2"
	compactionv2pb "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2/proto"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
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

func TestCheckInKeySpan(t *testing.T) {
	keyRange := func(t *testing.T, minColumn, maxColumn string) postings.KeyRange {
		t.Helper()
		r, err := postings.NewKeyRange(labelPostingKey(minColumn), labelPostingKey(maxColumn))
		require.NoError(t, err)
		return r
	}
	start, end := labelPostingKey("b"), labelPostingKey("d")
	span, err := postings.NewKeySpan(&start, &end)
	require.NoError(t, err)
	task := indexMergeTask{keySpan: span}

	t.Run("accepts artifacts inside the key span of the task", func(t *testing.T) {
		require.NoError(t, checkInKeySpan(task, []v2.ResultArtifact{
			{Path: "index/0", PostingKeys: keyRange(t, "b", "bb")},
			{Path: "index/1", PostingKeys: keyRange(t, "c", "cz")},
		}))
	})

	t.Run("accepts any artifact when the task holds every key", func(t *testing.T) {
		require.NoError(t, checkInKeySpan(indexMergeTask{}, []v2.ResultArtifact{
			{Path: "index/0", PostingKeys: keyRange(t, "a", "z")},
			{Path: "index/1"},
		}))
	})

	t.Run("returns an error when an artifact holds keys outside the key span of the task", func(t *testing.T) {
		err := checkInKeySpan(task, []v2.ResultArtifact{
			{Path: "index/0", PostingKeys: keyRange(t, "b", "c")},
			{Path: "index/1", PostingKeys: keyRange(t, "c", "d")},
		})
		require.ErrorContains(t, err, "index/1")
	})

	t.Run("returns an error when an artifact has no posting key bounds", func(t *testing.T) {
		require.Error(t, checkInKeySpan(task, []v2.ResultArtifact{{Path: "index/0"}}))
	})
}

func TestSplitIndexMerge_Plans(t *testing.T) {
	t.Run("returns one plan for each task in task order with the key span and stats sources of the task", func(t *testing.T) {
		envKey := labelPostingKey("env")
		below, err := postings.NewKeySpan(nil, &envKey)
		require.NoError(t, err)
		from, err := postings.NewKeySpan(&envKey, nil)
		require.NoError(t, err)
		spec := &compactionv2pb.TaskSpec{Tenant: "acme"}
		merge := splitIndexMerge{tasks: []indexMergeTask{
			{spec: spec, keySpan: below, statsSources: []string{"index/a"}},
			{spec: spec, keySpan: from, statsSources: []string{"index/b"}},
		}}

		plans := merge.plans("acme", time.Unix(0, 0))
		require.Len(t, plans, 2)
		var nodes []*physical.IndexMerge
		for _, plan := range plans {
			root, err := plan.Root()
			require.NoError(t, err)
			node, ok := root.(*physical.IndexMerge)
			require.True(t, ok, "root is %T", root)
			nodes = append(nodes, node)
		}
		require.Equal(t, below, nodes[0].KeySpan)
		require.Equal(t, []string{"index/a"}, nodes[0].StatsSources)
		require.Equal(t, from, nodes[1].KeySpan)
		require.Equal(t, []string{"index/b"}, nodes[1].StatsSources)
	})
}

func TestSplitIndexMerge_Join(t *testing.T) {
	envKey := labelPostingKey("env")
	below, err := postings.NewKeySpan(nil, &envKey)
	require.NoError(t, err)
	from, err := postings.NewKeySpan(&envKey, nil)
	require.NoError(t, err)
	merge := splitIndexMerge{tasks: []indexMergeTask{{keySpan: below}, {keySpan: from}}}
	artifact := func(t *testing.T, path, minColumn, maxColumn string) v2.ResultArtifact {
		t.Helper()
		keys, err := postings.NewKeyRange(labelPostingKey(minColumn), labelPostingKey(maxColumn))
		require.NoError(t, err)
		return v2.ResultArtifact{Path: path, PostingKeys: keys}
	}

	t.Run("returns the artifacts of every task in task order", func(t *testing.T) {
		first, second, third := artifact(t, "index/0", "app", "cz"), artifact(t, "index/1", "d", "da"), artifact(t, "index/2", "env", "zone")
		joined, err := merge.join([][]v2.ResultArtifact{{first, second}, {third}})
		require.NoError(t, err)
		require.Equal(t, []v2.ResultArtifact{first, second, third}, joined)
	})

	t.Run("returns an error when it gets the artifacts of a different number of tasks", func(t *testing.T) {
		_, err := merge.join([][]v2.ResultArtifact{{artifact(t, "index/0", "app", "cz")}})
		require.ErrorContains(t, err, "got the artifacts of 1 tasks for 2 tasks")
	})

	t.Run("returns an error when an artifact holds keys outside the key span of its task", func(t *testing.T) {
		_, err := merge.join([][]v2.ResultArtifact{{artifact(t, "index/0", "app", "zone")}, {artifact(t, "index/1", "env", "zone")}})
		require.ErrorContains(t, err, "task 0: artifact index/0 holds posting keys")
	})
}

func TestReusedPaths(t *testing.T) {
	t.Run("returns the paths of new runs that are also old paths in new run order", func(t *testing.T) {
		newRuns := []metastore.IndexRunEntries{
			{{Path: "index/new-0"}, {Path: "index/old-1"}},
			{{Path: "index/old-0"}},
		}
		require.Equal(t, []string{"index/old-1", "index/old-0"}, reusedPaths([]string{"index/old-0", "index/old-1"}, newRuns))
	})

	t.Run("returns no paths when every path of the new runs is new", func(t *testing.T) {
		require.Empty(t, reusedPaths([]string{"index/old-0"}, []metastore.IndexRunEntries{{{Path: "index/new-0"}}}))
	})
}
