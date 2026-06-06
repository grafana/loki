package engine

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/engine/internal/planner/logical"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/logql/log"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
)

// logqlShape summarizes the shape of an incoming LogQL query for observability.
// It is observed once per query.
type logqlShape struct {
	// lengthChars is the length of the query string in characters.
	lengthChars int
	// labelMatchers is the number of stream-selector label matchers.
	labelMatchers int
	// pipelineStages is the number of pipeline stages across all selectors.
	pipelineStages int
	// hasRegex reports whether the query uses a regex stream-selector matcher
	// or a regex line filter (|~, !~).
	hasRegex bool
}

// logqlShapeOf computes the [logqlShape] of the parsed expr. queryString is the
// raw query text used for the character-length stat.
//
// hasRegex is an approximation: it covers regex stream-selector matchers and
// regex line filters, which are the common cases. It does not inspect regex
// label filters, whose underlying filterer is unexported in the log package.
func logqlShapeOf(queryString string, expr syntax.Expr) logqlShape {
	shape := logqlShape{lengthChars: len(queryString)}
	if expr == nil {
		return shape
	}

	if sel := logSelectorOf(expr); sel != nil {
		matchers := sel.Matchers()
		shape.labelMatchers = len(matchers)
		for _, m := range matchers {
			if m.Type == labels.MatchRegexp || m.Type == labels.MatchNotRegexp {
				shape.hasRegex = true
			}
		}
	}

	expr.Walk(func(e syntax.Expr) bool {
		switch n := e.(type) {
		case *syntax.PipelineExpr:
			shape.pipelineStages += len(n.MultiStages)
		case *syntax.LineFilterExpr:
			if n.Ty == log.LineMatchRegexp || n.Ty == log.LineMatchNotRegexp {
				shape.hasRegex = true
			}
		}
		return true
	})

	return shape
}

// logSelectorOf returns the log selector expression backing expr, unwrapping a
// metric query's selector when necessary. It returns nil if no selector can be
// resolved.
func logSelectorOf(expr syntax.Expr) syntax.LogSelectorExpr {
	switch e := expr.(type) {
	case syntax.SampleExpr:
		sel, err := e.Selector()
		if err != nil {
			return nil
		}
		return sel
	case syntax.LogSelectorExpr:
		return e
	default:
		return nil
	}
}

// physicalShape summarizes the shape of a physical plan for observability. It
// is observed once per query after physical planning succeeds.
type physicalShape struct {
	// depth is the longest root-to-leaf path, counted in nodes (a single-node
	// plan has depth 1).
	depth int
	// nodeCount is the total number of nodes in the plan.
	nodeCount int
	// maxFanout is the largest number of children of any node.
	maxFanout int
	// partitionableSubtrees is the number of Parallelize nodes.
	partitionableSubtrees int
}

// physicalPlanShapeOf computes the [physicalShape] of plan by walking it.
func physicalPlanShapeOf(plan *physical.Plan) physicalShape {
	var shape physicalShape
	if plan == nil {
		return shape
	}
	shape.nodeCount = plan.Len()

	visited := make(map[physical.Node]struct{})
	depths := make(map[physical.Node]int)
	for _, root := range plan.Roots() {
		collectPhysicalShape(plan, root, visited, &shape)
		if d := physicalNodeDepth(plan, root, depths); d > shape.depth {
			shape.depth = d
		}
	}
	return shape
}

// collectPhysicalShape walks the subtree rooted at n, accumulating fanout and
// Parallelize counts into shape. visited dedupes nodes reachable by multiple
// paths.
func collectPhysicalShape(plan *physical.Plan, n physical.Node, visited map[physical.Node]struct{}, shape *physicalShape) {
	if _, ok := visited[n]; ok {
		return
	}
	visited[n] = struct{}{}

	children := plan.Children(n)
	if len(children) > shape.maxFanout {
		shape.maxFanout = len(children)
	}
	if n.Type() == physical.NodeTypeParallelize {
		shape.partitionableSubtrees++
	}

	for _, c := range children {
		collectPhysicalShape(plan, c, visited, shape)
	}
}

// physicalNodeDepth returns the depth (in nodes) of the longest path from n to a
// leaf, memoizing results in depths.
func physicalNodeDepth(plan *physical.Plan, n physical.Node, depths map[physical.Node]int) int {
	if d, ok := depths[n]; ok {
		return d
	}

	best := 0
	for _, c := range plan.Children(n) {
		if d := physicalNodeDepth(plan, c, depths); d > best {
			best = d
		}
	}
	depth := best + 1
	depths[n] = depth
	return depth
}

// errorClass classifies err into a bounded label value derived from its Go
// type. The set of types is fixed by the optimizer code, so cardinality stays
// bounded. It returns the empty string for a nil error.
func errorClass(err error) string {
	if err == nil {
		return ""
	}
	return fmt.Sprintf("%T", err)
}

// logicalPassEntry is the compact JSON representation of a single logical pass
// firing, used in the logical-plan-summary log line.
type logicalPassEntry struct {
	Name       string `json:"name"`
	Applicable bool   `json:"applicable"`
	Succeeded  bool   `json:"succeeded"`
}

// physicalRuleEntry is the compact JSON representation of a single physical rule
// firing, used in the physical-plan-summary log line. Physical rules have no
// error path, so there is no succeeded dimension.
type physicalRuleEntry struct {
	Name       string `json:"name"`
	Applicable bool   `json:"applicable"`
}

// encodeLogicalPasses encodes logical pass firings as a compact JSON array
// string suitable for a logfmt field value.
func encodeLogicalPasses(firings []logical.PassFiring) string {
	entries := make([]logicalPassEntry, 0, len(firings))
	for _, f := range firings {
		entries = append(entries, logicalPassEntry{Name: f.Name, Applicable: f.Applicable, Succeeded: f.Succeeded})
	}
	return encodeFirings(entries)
}

// encodePhysicalRules encodes physical rule firings as a compact JSON array
// string suitable for a logfmt field value. Rules are emitted in
// deterministic (sorted) name order.
func encodePhysicalRules(rules map[string]bool) string {
	names := make([]string, 0, len(rules))
	for name := range rules {
		names = append(names, name)
	}
	sort.Strings(names)

	entries := make([]physicalRuleEntry, 0, len(rules))
	for _, name := range names {
		entries = append(entries, physicalRuleEntry{Name: name, Applicable: rules[name]})
	}
	return encodeFirings(entries)
}

func encodeFirings(entries any) string {
	b, err := json.Marshal(entries)
	if err != nil {
		return "[]"
	}
	return string(b)
}
