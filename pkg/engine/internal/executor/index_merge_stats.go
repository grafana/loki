package executor

import (
	"strings"

	"github.com/grafana/loki/v3/pkg/dataobj/sections/stats"
)

// compareStatsRow returns the lexicographic order of two stats.Stat records
// under the merge sort, which is:
//
//	(Labels in SortSchema order, MinTimestamp, MaxTimestamp, ObjectPath, SectionIndex)
//
// Assumes all input rows share the same SortSchema; this is validated upstream
// in classifyRuns.
func compareStatsRow(a, b stats.Stat) int {
	// The first three components (labels, minT, maxT) must match the stats
	// builder sort order (pkg/dataobj/sections/stats/builder.go:compareStats).
	for labelName := range strings.SplitSeq(a.SortSchema, ",") {
		va := a.Labels[labelName]
		vb := b.Labels[labelName]
		if va != vb {
			if va < vb {
				return -1
			}
			return 1
		}
	}

	if a.MinTimestamp != b.MinTimestamp {
		if a.MinTimestamp < b.MinTimestamp {
			return -1
		}
		return 1
	}

	if a.MaxTimestamp != b.MaxTimestamp {
		if a.MaxTimestamp < b.MaxTimestamp {
			return -1
		}
		return 1
	}

	if a.ObjectPath != b.ObjectPath {
		if a.ObjectPath < b.ObjectPath {
			return -1
		}
		return 1
	}
	if a.SectionIndex != b.SectionIndex {
		if a.SectionIndex < b.SectionIndex {
			return -1
		}
		return 1
	}

	return 0
}
