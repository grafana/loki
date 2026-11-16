package compactor

import (
	"fmt"
	"strings"

	"github.com/grafana/loki/v3/pkg/dataobj/sections/indexpointers"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
)

// indexRun is the index objects of one run in a ToC, which compaction plans,
// skips and replaces as one unit. An index object of no run is a run of its
// own.
//
// A run splits the postings of one logs section across its index objects, and
// a query reads them as one index. If compaction replaced only some of them,
// the remaining index objects would miss the postings of the replaced ones.
//
// Every index object of a run holds postings rows of its tenant, and a stats
// section that records the layout of the run, even if the section has no
// rows. Planning places an index object by its postings rows, so it keeps
// every index object of a run in the unit of the run.
type indexRun struct {
	// entries is never empty. Its entries share RunID.
	entries []indexpointers.IndexPointer
}

// maxLoggedRunPaths is the largest number of index object paths that
// [indexRun.String] lists, so a log line of a large run stays short.
const maxLoggedRunPaths = 8

// groupIndexRuns groups the entries of one ToC into index runs. Each entry of
// no run is a run of its own. The runs and their entries keep the order of
// entries.
func groupIndexRuns(entries []indexpointers.IndexPointer) []indexRun {
	type runKey struct {
		runID indexpointers.RunID
		path  string
	}

	var (
		out  []indexRun
		runs = make(map[runKey]int)
	)
	for _, entry := range entries {
		key := runKey{runID: entry.RunID}
		if entry.RunID == 0 {
			key.path = entry.Path
		}
		if i, ok := runs[key]; ok {
			out[i].entries = append(out[i].entries, entry)
			continue
		}
		runs[key] = len(out)
		out = append(out, indexRun{entries: []indexpointers.IndexPointer{entry}})
	}
	return out
}

// planningUnit returns the name of r for the PlanningUnit field of the planner
// sections. It is the path of the first index object of r. An index object
// belongs to one run only, so the name is unique, and it can't equal the path
// of an index object of another run.
func (r indexRun) planningUnit() string { return r.entries[0].Path }

// paths returns the paths of the index objects of r, in the order of r.
func (r indexRun) paths() []string {
	paths := make([]string, len(r.entries))
	for i, entry := range r.entries {
		paths[i] = entry.Path
	}
	return paths
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

// layoutGroup holds the index runs of one tenant and window that share a
// stream layout, so they can merge with each other. It knows the index run of
// each of its index objects.
//
// The zero value is not usable. Use [newLayoutGroup].
type layoutGroup struct {
	// layout is the stream layout of the index runs. It has no meaning when
	// hasLayout is false, because the index runs have no stats sections.
	layout    streams.Layout
	hasLayout bool

	runs []indexRun

	// runOfPath maps the path of each index object of runs to its index in
	// runs.
	runOfPath map[string]int
}

// newLayoutGroup returns an empty group of the index runs with layout.
// hasLayout is false for the group of the index runs without stats sections.
func newLayoutGroup(layout streams.Layout, hasLayout bool) *layoutGroup {
	return &layoutGroup{layout: layout, hasLayout: hasLayout, runOfPath: make(map[string]int)}
}

// shardBuckets returns the shard layout of the index runs of g, as a ToC entry
// records it. It is the zero ShardBuckets if g has no layout.
func (g *layoutGroup) shardBuckets() indexpointers.ShardBuckets {
	if !g.hasLayout {
		return indexpointers.ShardBuckets{}
	}
	return indexpointers.NewShardBuckets(g.layout.ShardLayout())
}

// add adds run to g. run must not share an index object with a run of g.
func (g *layoutGroup) add(run indexRun) {
	for _, entry := range run.entries {
		g.runOfPath[entry.Path] = len(g.runs)
	}
	g.runs = append(g.runs, run)
}

// entries returns the index objects of every run of g.
func (g *layoutGroup) entries() []indexpointers.IndexPointer {
	var out []indexpointers.IndexPointer
	for _, run := range g.runs {
		out = append(out, run.entries...)
	}
	return out
}

// runOf returns the index run of the index object at path. It returns an
// error if g holds no index object at path.
func (g *layoutGroup) runOf(path string) (indexRun, error) {
	i, ok := g.runOfPath[path]
	if !ok {
		return indexRun{}, fmt.Errorf("index %q is not in the layout group", path)
	}
	return g.runs[i], nil
}

// countRuns returns the number of index runs that the index objects at paths
// belong to. It returns an error if g doesn't hold an index object of paths.
func (g *layoutGroup) countRuns(paths []string) (int, error) {
	runs := make(map[int]struct{})
	for _, path := range paths {
		i, ok := g.runOfPath[path]
		if !ok {
			return 0, fmt.Errorf("index %q is not in the layout group", path)
		}
		runs[i] = struct{}{}
	}
	return len(runs), nil
}
