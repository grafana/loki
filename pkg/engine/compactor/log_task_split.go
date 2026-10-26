package compactor

import (
	"fmt"
	"slices"
	"strings"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"

	v2 "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2"
	compactionv2pb "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2/proto"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
)

// logTaskSplitter splits LogMerge tasks into tasks of about maxSize bytes of
// uncompressed input each. It cuts by sort prefix, so the key ranges of the
// tasks are disjoint.
//
// A section can hold sort prefixes on both sides of a cut. Each task that
// shares the section reads all of it and keeps only its own range.
type logTaskSplitter struct {
	logger  log.Logger
	maxSize uint64
	keys    map[sectionKey][]v2.KeySize[logSortPrefix]
}

// newLogTaskSplitter returns a splitter for tasks built from sections. It
// panics if maxSize is 0.
func newLogTaskSplitter(logger log.Logger, maxSize uint64, sections []v2.Section[logSortPrefix]) logTaskSplitter {
	if maxSize == 0 {
		panic("log task splitter needs a maximum task size above 0")
	}
	keys := make(map[sectionKey][]v2.KeySize[logSortPrefix], len(sections))
	for _, section := range sections {
		keys[sectionKeyOf(section.Ref)] = section.Keys
	}
	return logTaskSplitter{logger: logger, maxSize: maxSize, keys: keys}
}

// fullLogSortPrefixRange is the range of every sort prefix, from
// [streams.MinSortPrefix] to [streams.SortPrefixLimit].
var fullLogSortPrefixRange = func() v2.KeyRange[logSortPrefix] {
	r, err := v2.NewKeyRange(logSortPrefix{prefix: streams.MinSortPrefix()}, logSortPrefix{prefix: streams.SortPrefixLimit()}, compareLogSortPrefix)
	if err != nil {
		panic(fmt.Sprintf("full sort prefix range: %v", err))
	}
	return r
}()

// split returns the tasks that together do the work of spec. Every task gets
// an explicit key range, and the ranges together cover every sort prefix.
//
// If spec fits in one task, split returns spec alone with the full range.
// Otherwise each returned task holds only the sections with a sort prefix in
// its key range. A section without per-key sizes goes to every task.
func (s logTaskSplitter) split(spec *compactionv2pb.TaskSpec) []*compactionv2pb.TaskSpec {
	var keys []v2.KeySize[logSortPrefix]
	for _, run := range spec.Runs {
		for _, section := range run.Sections {
			keys = append(keys, s.keys[sectionKeyOf(section)]...)
		}
	}
	splits := v2.SplitKeyRanges(keys, compareLogSortPrefix, s.maxSize, fullLogSortPrefixRange)
	if len(splits) == 1 {
		spec.KeyRange = v2.FullKeyRangeProto()
		return []*compactionv2pb.TaskSpec{spec}
	}

	tasks := make([]*compactionv2pb.TaskSpec, 0, len(splits))
	for _, split := range splits {
		if split.Oversized {
			level.Warn(s.logger).Log(
				"msg", "log-compaction: sort prefix is larger than the maximum task size, giving it a task of its own",
				"shard_bucket", split.First.prefix.ShardBucket,
				"sort_prefix", strings.Join(split.First.labels, ","),
				"size", split.Size,
				"max_task_size", s.maxSize,
			)
		}
		tasks = append(tasks, &compactionv2pb.TaskSpec{
			Tenant:   spec.Tenant,
			Runs:     s.runsInRange(spec.Runs, split.Range),
			KeyRange: keyRangeProto(split.Range),
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
	keys := s.keys[sectionKeyOf(section)]
	return len(keys) == 0 || slices.ContainsFunc(keys, func(key v2.KeySize[logSortPrefix]) bool {
		return keyRange.Contains(key.Key, compareLogSortPrefix)
	})
}

func keyRangeProto(r v2.KeyRange[logSortPrefix]) *compactionv2pb.KeyRange {
	return &compactionv2pb.KeyRange{Start: sortPrefixProto(r.Start()), End: sortPrefixProto(r.End())}
}

func sortPrefixProto(key logSortPrefix) *compactionv2pb.SortPrefix {
	return &compactionv2pb.SortPrefix{ShardBucket: key.prefix.ShardBucket, SchemaValues: key.labels}
}
