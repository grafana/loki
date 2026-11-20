package physical

import (
	"time"

	compactionv2pb "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2/proto"
)

// LogFields returns the log key-value pairs that describe the input of node:
// window, input_objects, and input_bytes. IndexMerge has no input_bytes,
// because its sections do not record a size. LogFields returns nil for other
// node types.
func LogFields(node Node) []any {
	switch n := node.(type) {
	case *IndexMerge:
		return []any{
			"window", time.Unix(0, n.ToCWindowStart).UTC(),
			"input_objects", countObjects(n.Runs),
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
