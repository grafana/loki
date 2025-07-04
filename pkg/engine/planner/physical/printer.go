package physical

import (
	"fmt"
	"io"
	"strings"

	"github.com/grafana/loki/v3/pkg/engine/planner/internal/tree"
)

// BuildTree converts a physical plan node and its children into a tree structure
// that can be used for visualization and debugging purposes.
func BuildTree(p *Plan, n Node) *tree.Node {
	return toTree(p, n)
}

func toTree(p *Plan, n Node) *tree.Node {
	root := toTreeNode(n)
	for _, child := range p.Children(n) {
		if ch := toTree(p, child); ch != nil {
			root.Children = append(root.Children, ch)
		}
	}
	return root
}

func toTreeNode(n Node) *tree.Node {
	treeNode := tree.NewNode(n.Type().String(), n.ID())
	switch node := n.(type) {
	case *DataObjScan:
		treeNode.Properties = []tree.Property{
			tree.NewProperty("location", false, node.Location),
			tree.NewProperty("stream_ids", true, toAnySlice(node.StreamIDs)...),
			tree.NewProperty("section_ids", true, toAnySlice(node.Sections)...),
			tree.NewProperty("projections", true, toAnySlice(node.Projections)...),
			tree.NewProperty("direction", false, node.Direction),
			tree.NewProperty("limit", false, node.Limit),
		}
		for i := range node.Predicates {
			treeNode.Properties = append(treeNode.Properties, tree.NewProperty(fmt.Sprintf("predicate[%d]", i), false, node.Predicates[i].String()))
		}
	case *SortMerge:
		treeNode.Properties = []tree.Property{
			tree.NewProperty("column", false, node.Column),
			tree.NewProperty("order", false, node.Order),
		}
	case *Projection:
		treeNode.Properties = []tree.Property{
			tree.NewProperty("columns", true, toAnySlice(node.Columns)...),
		}
	case *Filter:
		for i := range node.Predicates {
			treeNode.Properties = append(treeNode.Properties, tree.NewProperty(fmt.Sprintf("predicate[%d]", i), false, node.Predicates[i].String()))
		}
	case *Limit:
		treeNode.Properties = []tree.Property{
			tree.NewProperty("offset", false, node.Skip),
			tree.NewProperty("limit", false, node.Fetch),
		}
	case *RangeAggregation:
		properties := []tree.Property{
			tree.NewProperty("operation", false, node.Operation),
			tree.NewProperty("start", false, node.Start),
			tree.NewProperty("end", false, node.End),
			tree.NewProperty("step", false, node.Step),
			tree.NewProperty("range", false, node.Range),
		}

		if len(node.PartitionBy) > 0 {
			properties = append(properties, tree.NewProperty("partition_by", true, toAnySlice(node.PartitionBy)...))
		}

		treeNode.Properties = properties
	}
	return treeNode
}

func toAnySlice[T any](s []T) []any {
	ret := make([]any, len(s))
	for i := range s {
		ret[i] = s[i]
	}
	return ret
}

// PrintAsTree converts a physical [Plan] into a human-readable tree representation.
// It processes each root node in the plan graph, and returns the combined
// string output of all trees joined by newlines.
func PrintAsTree(p *Plan) string {
	results := make([]string, 0, len(p.Roots()))

	for _, root := range p.Roots() {
		sb := &strings.Builder{}
		printer := tree.NewPrinter(sb)
		node := BuildTree(p, root)
		printer.Print(node)
		results = append(results, sb.String())
	}

	return strings.Join(results, "\n")
}

func WriteMermaidFormat(w io.Writer, p *Plan) {
	for _, root := range p.Roots() {
		node := BuildTree(p, root)
		printer := tree.NewMermaid(w)
		_ = printer.Write(node)

		fmt.Fprint(w, "\n\n")
	}
}
