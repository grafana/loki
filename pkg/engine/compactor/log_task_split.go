package compactor

import (
	"strings"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"

	v2 "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2"
	compactionv2pb "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2/proto"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
)

// logTaskSplitter splits LogMerge tasks into tasks of about maxSize bytes of
// uncompressed input each. It cuts by sort prefix, so every task gets a
// disjoint key range and no task duplicates the records of another.
//
// A section can hold sort prefixes on both sides of a cut. Each task that
// shares the section reads all of it and keeps only its own range.
type logTaskSplitter struct {
	logger  log.Logger
	maxSize uint64
	keys    map[*compactionv2pb.SectionRef][]v2.KeySize[logSortPrefix]
}

// newLogTaskSplitter returns a splitter for tasks built from sections. A
// maxSize of 0 disables the split.
func newLogTaskSplitter(logger log.Logger, maxSize uint64, sections []v2.Section[logSortPrefix]) logTaskSplitter {
	keys := make(map[*compactionv2pb.SectionRef][]v2.KeySize[logSortPrefix], len(sections))
	for _, section := range sections {
		keys[section.Ref] = section.Keys
	}
	return logTaskSplitter{logger: logger, maxSize: maxSize, keys: keys}
}

// split returns the tasks that together do the work of spec. Every task gets
// an explicit key range, and the ranges together cover the full key space,
// from [streams.MinSortPrefix] to [streams.SortPrefixLimit].
//
// If the split is disabled or spec fits in one task, split returns spec alone
// with the full range. Otherwise each returned task holds only the sections
// with a sort prefix in its key range. A section without per-key sizes goes
// to every task.
func (s logTaskSplitter) split(spec *compactionv2pb.TaskSpec) []*compactionv2pb.TaskSpec {
	start := logSortPrefix{prefix: streams.MinSortPrefix()}
	end := logSortPrefix{prefix: streams.SortPrefixLimit()}
	if s.maxSize == 0 {
		spec.KeyRange = keyRangeProto(v2.KeyRange[logSortPrefix]{Start: start, End: end})
		return []*compactionv2pb.TaskSpec{spec}
	}

	var keys []v2.KeySize[logSortPrefix]
	for _, run := range spec.Runs {
		for _, section := range run.Sections {
			keys = append(keys, s.keys[section]...)
		}
	}
	ranges := v2.SplitKeyRanges(keys, compareLogSortPrefix, s.maxSize, start, end)
	if len(ranges) == 1 {
		spec.KeyRange = keyRangeProto(ranges[0])
		return []*compactionv2pb.TaskSpec{spec}
	}

	tasks := make([]*compactionv2pb.TaskSpec, 0, len(ranges))
	for _, keyRange := range ranges {
		if keyRange.Size > s.maxSize {
			level.Warn(s.logger).Log(
				"msg", "log-compaction: sort prefix is larger than the maximum task size, giving it a task of its own",
				"shard_bucket", keyRange.First.prefix.ShardBucket,
				"sort_prefix", strings.Join(keyRange.First.labels, ","),
				"size", keyRange.Size,
				"max_task_size", s.maxSize,
			)
		}
		tasks = append(tasks, &compactionv2pb.TaskSpec{
			Tenant:   spec.Tenant,
			Runs:     s.runsInRange(spec.Runs, keyRange),
			KeyRange: keyRangeProto(keyRange),
		})
	}
	return tasks
}

// runsInRange returns runs without the sections that hold no sort prefix in
// keyRange. It drops runs that become empty.
func (s logTaskSplitter) runsInRange(runs []*compactionv2pb.RunRef, keyRange v2.KeyRange[logSortPrefix]) []*compactionv2pb.RunRef {
	var out []*compactionv2pb.RunRef
	for _, run := range runs {
		var sections []*compactionv2pb.SectionRef
		for _, section := range run.Sections {
			if s.sectionInRange(section, keyRange) {
				sections = append(sections, section)
			}
		}
		if len(sections) > 0 {
			out = append(out, &compactionv2pb.RunRef{Sections: sections})
		}
	}
	return out
}

func (s logTaskSplitter) sectionInRange(section *compactionv2pb.SectionRef, keyRange v2.KeyRange[logSortPrefix]) bool {
	keys := s.keys[section]
	if len(keys) == 0 {
		return true
	}
	for _, key := range keys {
		if keyRange.Contains(key.Key, compareLogSortPrefix) {
			return true
		}
	}
	return false
}

func keyRangeProto(r v2.KeyRange[logSortPrefix]) *compactionv2pb.KeyRange {
	return &compactionv2pb.KeyRange{Start: sortPrefixProto(r.Start), End: sortPrefixProto(r.End)}
}

func sortPrefixProto(key logSortPrefix) *compactionv2pb.SortPrefix {
	return &compactionv2pb.SortPrefix{ShardBucket: key.prefix.ShardBucket, SchemaValues: key.labels}
}
