package physical

import (
	"time"

	compactionv2pb "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2/proto"
)

// LogFields returns the log key-value pairs that describe the input of node:
// window, input_objects, and the input size. LogFields returns nil for other
// node types.
//
// The input size of a LogMerge is input_bytes, the uncompressed size of its
// log records. The input size of an IndexMerge is input_postings_bytes, the
// uncompressed size of the postings sections in its runs. It is 0 if the
// planner did not record the section sizes.
func LogFields(node Node) []any {
	switch n := node.(type) {
	case *IndexMerge:
		return []any{
			"window", time.Unix(0, n.ToCWindowStart).UTC(),
			"input_objects", countObjects(n.Runs),
			"input_postings_bytes", sumSectionSizes(n.Runs),
		}
	case *LogMerge:
		return []any{
			"window", time.Unix(0, n.ToCWindowStart).UTC(),
			"input_objects", countObjects(n.Runs),
			"input_bytes", n.InputBytes,
		}
	default:
		return nil
	}
}

func countObjects(runs []*compactionv2pb.RunRef) int {
	paths := make(map[string]struct{})
	for _, run := range runs {
		for _, section := range run.Sections {
			paths[section.ObjectPath] = struct{}{}
		}
	}
	return len(paths)
}

func sumSectionSizes(runs []*compactionv2pb.RunRef) int64 {
	var size int64
	for _, run := range runs {
		for _, section := range run.Sections {
			size += section.UncompressedSize
		}
	}
	return size
}
