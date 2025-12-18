package physical

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/grafana/loki/v3/pkg/engine/internal/util/tree"
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
	treeNode := tree.NewNode(n.Type().String(), "")
	treeNode.Context = n

	switch node := n.(type) {
	case *DataObjScan:
		treeNode.Properties = []tree.Property{
			tree.NewProperty("location", false, node.Location),
			tree.NewProperty("streams", false, len(node.StreamIDs)),
			tree.NewProperty("section_id", false, node.Section),
			tree.NewProperty("projections", true, toAnySlice(node.Projections)...),
		}
		for i := range node.Predicates {
			treeNode.Properties = append(treeNode.Properties, tree.NewProperty(fmt.Sprintf("predicate[%d]", i), false, node.Predicates[i].String()))
		}
		treeNode.AddComment("@max_time_range", "", []tree.Property{
			tree.NewProperty("start", false, node.MaxTimeRange.Start.Format(time.RFC3339Nano)),
			tree.NewProperty("end", false, node.MaxTimeRange.End.Format(time.RFC3339Nano)),
		})
	case *Projection:
		treeNode.Properties = []tree.Property{
			tree.NewProperty("all", false, node.All),
		}
		if node.Expand {
			treeNode.Properties = append(treeNode.Properties, tree.NewProperty("expand", true, toAnySlice(node.Expressions)...))
		} else if node.Drop {
			treeNode.Properties = append(treeNode.Properties, tree.NewProperty("drop", true, toAnySlice(node.Expressions)...))
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
		treeNode.Properties = []tree.Property{
			tree.NewProperty("operation", false, node.Operation),
			tree.NewProperty("start", false, node.Start.Format(time.RFC3339Nano)),
			tree.NewProperty("end", false, node.End.Format(time.RFC3339Nano)),
			tree.NewProperty("step", false, node.Step),
			tree.NewProperty("range", false, node.Range),
		}

		if node.Grouping.Without {
			if len(node.Grouping.Columns) > 0 {
				treeNode.Properties = append(treeNode.Properties, tree.NewProperty("group_without", true, toAnySlice(node.Grouping.Columns)...))
			}
		} else {
			treeNode.Properties = append(treeNode.Properties, tree.NewProperty("group_by", true, toAnySlice(node.Grouping.Columns)...))
		}
	case *VectorAggregation:
		properties := []tree.Property{
			tree.NewProperty("operation", false, node.Operation),
		}

		if node.Grouping.Without {
			if len(node.Grouping.Columns) > 0 {
				properties = append(properties, tree.NewProperty("group_without", true, toAnySlice(node.Grouping.Columns)...))
			}
		} else {
			properties = append(properties, tree.NewProperty("group_by", true, toAnySlice(node.Grouping.Columns)...))
		}

		treeNode.Properties = properties
	case *ColumnCompat:
		treeNode.Properties = []tree.Property{
			tree.NewProperty("src", false, node.Source),
			tree.NewProperty("dst", false, node.Destination),
			tree.NewProperty("collisions", true, toAnySlice(node.Collisions)...),
		}
	case *TopK:
		treeNode.Properties = []tree.Property{
			tree.NewProperty("sort_by", false, node.SortBy.String()),
			tree.NewProperty("ascending", false, node.Ascending),
			tree.NewProperty("nulls_first", false, node.NullsFirst),
			tree.NewProperty("k", false, node.K),
		}
	case *Parallelize:
		// Nothing to add
	case *ScanSet:
		treeNode.Properties = []tree.Property{
			tree.NewProperty("num_targets", false, len(node.Targets)),
		}

		if len(node.Projections) > 0 {
			treeNode.Properties = append(treeNode.Properties, tree.NewProperty("projections", true, toAnySlice(node.Projections)...))
		}
		for i := range node.Predicates {
			treeNode.Properties = append(treeNode.Properties, tree.NewProperty(fmt.Sprintf("predicate[%d]", i), false, node.Predicates[i].String()))
		}

		for _, target := range node.Targets {
			properties := []tree.Property{
				tree.NewProperty("type", false, target.Type.String()),
			}

			switch target.Type {
			case ScanTypeDataObject:
				// Create a child node to extract the properties of the target.
				childNode := toTreeNode(target.DataObject)
				properties = append(properties, childNode.Properties...)
			}

			treeNode.AddComment("@target", "", properties)
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
