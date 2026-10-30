package compactor

import (
	"fmt"
	"slices"
	"strings"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"

	v2 "github.com/grafana/loki/v4/pkg/dataobj/compaction/v2"
	compactionv2pb "github.com/grafana/loki/v4/pkg/dataobj/compaction/v2/proto"
	"github.com/grafana/loki/v4/pkg/dataobj/metastore"
	"github.com/grafana/loki/v4/pkg/dataobj/sections/streams"
)

func sectionKeyOf(ref *compactionv2pb.SectionRef) metastore.SectionKey {
	return metastore.SectionKey{ObjectPath: ref.ObjectPath, SectionIdx: ref.SectionIndex}
}

// sectionKeySizes holds the size of each sort prefix in each logs section,
// from the index stats.
type sectionKeySizes map[metastore.SectionKey][]v2.KeySize[logSortPrefix]

func newSectionKeySizes(sections []v2.Section[logSortPrefix]) sectionKeySizes {
	sizes := make(sectionKeySizes, len(sections))
	for _, section := range sections {
		sizes[sectionKeyOf(section.Ref)] = section.Keys
	}
	return sizes
}

// of returns the sort prefix sizes of the section that ref points to.
//
// It panics if the section has none. The planner builds the sections of a
// task and their sizes from the same stats rows, so every section of a task
// has at least one size, and a missing one is a bug.
func (s sectionKeySizes) of(ref *compactionv2pb.SectionRef) []v2.KeySize[logSortPrefix] {
	keys := s[sectionKeyOf(ref)]
	if len(keys) == 0 {
		panic(fmt.Sprintf("log task splitter has no sort prefix sizes for logs section %s#%d", ref.ObjectPath, ref.SectionIndex))
	}
	return keys
}

// totalBytes returns the size of every sort prefix in the sections of runs.
func (s sectionKeySizes) totalBytes(runs []*compactionv2pb.RunRef) int64 {
	return s.sum(runs, func(logSortPrefix) bool { return true })
}

// bytesIn returns the size of the sort prefixes in keyRange in the sections
// of runs.
func (s sectionKeySizes) bytesIn(runs []*compactionv2pb.RunRef, keyRange v2.KeyRange[logSortPrefix]) int64 {
	return s.sum(runs, func(key logSortPrefix) bool { return keyRange.Contains(key) })
}

// sum adds up the sizes of the kept sort prefixes in the sections of runs.
func (s sectionKeySizes) sum(runs []*compactionv2pb.RunRef, keep func(logSortPrefix) bool) int64 {
	var total uint64
	for _, run := range runs {
		for _, ref := range run.Sections {
			for _, key := range s.of(ref) {
				if keep(key.Key) {
					total += key.UncompressedSize
				}
			}
		}
	}
	return int64(total)
}

// logTaskSplitter turns one LogMerge task of the planning strategy into the
// tasks that together do its work. Every returned task has an explicit key
// range and input bytes.
type logTaskSplitter interface {
	split(spec *compactionv2pb.TaskSpec) []*compactionv2pb.TaskSpec
}

// noopLogTaskSplitter returns every task whole, over the full key range.
type noopLogTaskSplitter struct {
	keys sectionKeySizes
}

func newNoopLogTaskSplitter(keys sectionKeySizes) noopLogTaskSplitter {
	return noopLogTaskSplitter{keys: keys}
}

func (s noopLogTaskSplitter) split(spec *compactionv2pb.TaskSpec) []*compactionv2pb.TaskSpec {
	spec.KeyRange = v2.FullKeyRangeProto()
	spec.InputBytes = s.keys.totalBytes(spec.Runs)
	return []*compactionv2pb.TaskSpec{spec}
}

// sizeLogTaskSplitter splits LogMerge tasks into tasks of about maxSize bytes
// of uncompressed input each. It cuts by sort prefix, so the key ranges of
// the tasks are disjoint.
//
// A section can hold sort prefixes on both sides of a cut. Each task that
// shares the section reads all of it and keeps only its own range.
type sizeLogTaskSplitter struct {
	logger  log.Logger
	maxSize uint64
	keys    sectionKeySizes
}

// newSizeLogTaskSplitter returns a size splitter for tasks built from sections.
// It panics if maxSize is 0.
func newSizeLogTaskSplitter(logger log.Logger, maxSize uint64, keys sectionKeySizes) sizeLogTaskSplitter {
	if maxSize == 0 {
		panic("log task splitter needs a maximum task size above 0")
	}
	return sizeLogTaskSplitter{logger: logger, maxSize: maxSize, keys: keys}
}

// split returns the tasks that together do the work of spec. Every task gets
// an explicit key range.
//
// If spec fits in one task, split returns spec alone with the full range.
// Otherwise the first range starts at the smallest sort prefix of spec, the
// last range ends at [streams.SortPrefixLimit], and each task holds only the
// sections with a sort prefix in its range.
//
// The ranges and section choice rely on the index listing the sort prefix of
// every stream in spec, as logsobj.StreamSortPrefix computes it.
func (s sizeLogTaskSplitter) split(spec *compactionv2pb.TaskSpec) []*compactionv2pb.TaskSpec {
	var keys []v2.KeySize[logSortPrefix]
	for _, run := range spec.Runs {
		for _, section := range run.Sections {
			keys = append(keys, s.keys.of(section)...)
		}
	}
	splits := v2.SplitKeyRanges(keys, compareLogSortPrefix, s.maxSize, logSortPrefix{prefix: streams.SortPrefixLimit()})
	for _, split := range splits {
		if split.Oversized {
			key := split.Range.Start()
			level.Warn(s.logger).Log(
				"msg", "log-compaction: sort prefix is larger than the maximum task size, giving it a task of its own",
				"shard_bucket", key.prefix.ShardBucket,
				"sort_prefix", strings.Join(key.labels, ","),
				"size", split.UncompressedSize,
				"max_task_size", s.maxSize,
			)
		}
	}
	if len(splits) <= 1 {
		return newNoopLogTaskSplitter(s.keys).split(spec)
	}

	tasks := make([]*compactionv2pb.TaskSpec, 0, len(splits))
	for _, split := range splits {
		runs := s.runsInRange(spec.Runs, split.Range)
		tasks = append(tasks, &compactionv2pb.TaskSpec{
			Tenant:     spec.Tenant,
			Runs:       runs,
			KeyRange:   &compactionv2pb.KeyRange{Start: sortPrefixProto(split.Range.Start()), End: sortPrefixProto(split.Range.End())},
			InputBytes: s.keys.bytesIn(runs, split.Range),
		})
	}
	return tasks
}

// runsInRange returns runs without the sections that hold no sort prefix in
// keyRange. It drops runs that become empty.
func (s sizeLogTaskSplitter) runsInRange(runs []*compactionv2pb.RunRef, keyRange v2.KeyRange[logSortPrefix]) []*compactionv2pb.RunRef {
	var out []*compactionv2pb.RunRef
	for _, run := range runs {
		var sections []*compactionv2pb.SectionRef
		for _, section := range run.Sections {
			if slices.ContainsFunc(s.keys.of(section), func(key v2.KeySize[logSortPrefix]) bool {
				return keyRange.Contains(key.Key)
			}) {
				sections = append(sections, section)
			}
		}
		if len(sections) > 0 {
			out = append(out, &compactionv2pb.RunRef{Sections: sections})
		}
	}
	return out
}

func sortPrefixProto(key logSortPrefix) *compactionv2pb.KeyRangePrefix {
	return &compactionv2pb.KeyRangePrefix{ShardBucket: key.prefix.ShardBucket, SchemaValues: key.labels}
}
