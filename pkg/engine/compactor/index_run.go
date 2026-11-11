package compactor

import (
	"fmt"
	"strings"

	"github.com/grafana/loki/v3/pkg/dataobj/sections/indexpointers"
)

// indexRun is the index objects of one run in a ToC, which compaction plans,
// skips and replaces as one unit. An index object of no run is a run of its
// own.
//
// A run splits the postings of one logs section across its index objects. A
// query reads the index objects of a run as one index, so replacing only some
// of them would lose the postings of the others.
type indexRun struct {
	// entries is never empty. Its entries share RunID.
	entries []indexEntry
}

// maxLoggedRunPaths is the largest number of index object paths that
// [indexRun.String] lists, so a log line of a large run stays short.
const maxLoggedRunPaths = 8

// groupIndexRuns groups the entries of one ToC into runs. Each entry of no run
// is a run of its own. The runs and their entries keep the order of entries.
func groupIndexRuns(entries []indexEntry) []indexRun {
	var (
		out  []indexRun
		runs = make(map[indexRunKey]int)
	)
	for _, entry := range entries {
		key := runKeyOf(entry)
		if i, ok := runs[key]; ok {
			out[i].entries = append(out[i].entries, entry)
			continue
		}
		runs[key] = len(out)
		out = append(out, indexRun{entries: []indexEntry{entry}})
	}
	return out
}

// indexRunKey identifies a run within one ToC. A run ID identifies a run, and
// the path identifies an index object of no run.
type indexRunKey struct {
	runID indexpointers.RunID
	path  string
}

// key returns the key of r.
func (r indexRun) key() indexRunKey { return runKeyOf(r.entries[0]) }

// unit returns the name of r for [compactionv2.Section.Unit]. Planning keeps
// the sections of one unit in one run and one task.
func (r indexRun) unit() string {
	key := r.key()
	if key.runID == 0 {
		return key.path
	}
	return fmt.Sprintf("run %d", key.runID)
}

// String describes r for logs and errors. It names the paths of up to
// maxLoggedRunPaths index objects of r.
func (r indexRun) String() string {
	first := r.entries[0]
	if first.RunID == 0 {
		return first.Path
	}

	var paths strings.Builder
	for i, entry := range r.entries[:min(len(r.entries), maxLoggedRunPaths)] {
		if i > 0 {
			paths.WriteString(" ")
		}
		paths.WriteString(entry.Path)
	}
	if more := len(r.entries) - maxLoggedRunPaths; more > 0 {
		fmt.Fprintf(&paths, " and %d more", more)
	}
	return fmt.Sprintf("run %d [%s]", first.RunID, paths.String())
}

func runKeyOf(entry indexEntry) indexRunKey {
	if entry.RunID == 0 {
		return indexRunKey{path: entry.Path}
	}
	return indexRunKey{runID: entry.RunID}
}
