package physical

import (
	"strings"

	"github.com/grafana/loki/v3/pkg/dataobj/planner/internal/tree"
)

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
		treeNode.Attributes = []tree.Attribute{
			tree.NewAttribute("location", false, node.Location),
			tree.NewAttribute("stream_ids", true, toAnySlice(node.StreamIDs)...),
			tree.NewAttribute("projections", true, toAnySlice(node.Projections)...),
			tree.NewAttribute("predicates", true, toAnySlice(node.Predicates)...),
			tree.NewAttribute("direction", false, node.Direction),
			tree.NewAttribute("limit", false, node.Limit),
		}
	case *SortMerge:
		treeNode.Attributes = []tree.Attribute{
			tree.NewAttribute("column", false, node.Column),
			tree.NewAttribute("order", false, node.Order),
		}
	case *Projection:
		treeNode.Attributes = []tree.Attribute{
			tree.NewAttribute("columns", true, toAnySlice(node.Columns)...),
		}
	case *Filter:
		treeNode.Attributes = []tree.Attribute{
			tree.NewAttribute("predicates", true, toAnySlice(node.Predicates)...),
		}
	case *Limit:
		treeNode.Attributes = []tree.Attribute{
			tree.NewAttribute("offset", false, node.Offset),
			tree.NewAttribute("limit", false, node.Limit),
		}
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
