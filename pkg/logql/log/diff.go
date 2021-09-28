package log

import (
	"github.com/sergi/go-diff/diffmatchpatch"
)

// DiffFilter filters lines that are not different to the previous line.
type DiffFilter struct {
	// dmp is used to perform the diffing.
	dmp *diffmatchpatch.DiffMatchPatch
	// diff stores the diffs between the previous and current line.
	diff []diffmatchpatch.Diff
	// pretty determines whether the output is colored and prettified.
	pretty bool
	// prev is the previous line.
	prev []byte
}

// Filter implements Filterer.
// The line is only kept if there is a difference between the current line and the
// previous line.
// The first line passed to the expression is necessarily always kept.
func (df *DiffFilter) Filter(line []byte) (keep bool) {
	if len(df.prev) == 0 {
		df.prev = line
		return false
	}
	// It's ok to use unsafe here because the use of the strings is limited to this function
	df.diff = df.dmp.DiffMain(unsafeGetString(df.prev), unsafeGetString(line), false)
	df.prev = line
	return df.dmp.DiffLevenshtein(df.diff) != 0
}

// ToStage implements Filterer.
func (df *DiffFilter) ToStage() Stage {
	return StageFunc{
		process: func(line []byte, _ *LabelsBuilder) ([]byte, bool) {
			if keep := df.Filter(line); !keep {
				return line, false
			}

			if df.pretty {
				return []byte(df.dmp.DiffPrettyText(df.diff)), true
			}

			patches := df.dmp.PatchMake(df.diff)
			return []byte(df.dmp.PatchToText(patches)), true
		},
	}
}

// NewDiffFilter returns a DiffFilter, optionally configured for pretty output.
func NewDiffFilter(pretty bool) *DiffFilter {
	return &DiffFilter{
		dmp:    diffmatchpatch.New(),
		pretty: pretty,
	}
}
