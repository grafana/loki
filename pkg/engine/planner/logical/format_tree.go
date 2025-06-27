package logical

import (
	"fmt"
	"io"

	"github.com/grafana/loki/v3/pkg/engine/internal/util"
	"github.com/grafana/loki/v3/pkg/engine/planner/internal/tree"
)

// PrintTree prints the given value and its dependencies as a tree structure to
// w.
func PrintTree(w io.StringWriter, value Value) {
	p := tree.NewPrinter(w)

	var t treeFormatter
	p.Print(t.convert(value))
}

type treeFormatter struct{}

func (t *treeFormatter) convert(value Value) *tree.Node {
	switch value := value.(type) {
	case *MakeTable:
		return t.convertMakeTable(value)
	case *Select:
		return t.convertSelect(value)
	case *Limit:
		return t.convertLimit(value)
	case *Sort:
		return t.convertSort(value)
	case *RangeAggregation:
		return t.convertRangeAggregation(value)
	case *VectorAggregation:
		return t.convertVectorAggregation(value)

	case *UnaryOp:
		return t.convertUnaryOp(value)
	case *BinOp:
		return t.convertBinOp(value)
	case *ColumnRef:
		return t.convertColumnRef(value)
	case *Literal:
		return t.convertLiteral(value)

	default:
		panic(fmt.Sprintf("unknown value type %T", value))
	}
}

func (t *treeFormatter) convertMakeTable(ast *MakeTable) *tree.Node {
	node := tree.NewNode("MAKETABLE", ast.Name(),
		tree.NewProperty("selector", false, ast.Selector.String()),
	)
	node.Comments = append(node.Children, t.convert(ast.Selector))
	return node
}

func (t *treeFormatter) convertSelect(ast *Select) *tree.Node {
	node := tree.NewNode("SELECT", ast.Name(),
		tree.NewProperty("table", false, ast.Table.Name()),
		tree.NewProperty("predicate", false, ast.Predicate.Name()),
	)
	node.Comments = append(node.Comments, t.convert(ast.Predicate))
	node.Children = append(node.Children, t.convert(ast.Table))
	return node
}

func (t *treeFormatter) convertLimit(ast *Limit) *tree.Node {
	node := tree.NewNode("LIMIT", ast.Name(),
		tree.NewProperty("table", false, ast.Table.Name()),
		tree.NewProperty("offset", false, ast.Skip),
		tree.NewProperty("fetch", false, ast.Fetch),
	)
	node.Children = append(node.Children, t.convert(ast.Table))
	return node
}

func (t *treeFormatter) convertSort(ast *Sort) *tree.Node {
	direction := "asc"
	if !ast.Ascending {
		direction = "desc"
	}

	nullsPosition := "last"
	if ast.NullsFirst {
		nullsPosition = "first"
	}

	node := tree.NewNode("SORT", ast.Name(),
		tree.NewProperty("table", false, ast.Table.Name()),
		tree.NewProperty("column", false, ast.Column.Name()),
		tree.NewProperty("direction", false, direction),
		tree.NewProperty("nulls", false, nullsPosition),
	)
	node.Comments = append(node.Comments, t.convert(&ast.Column))
	node.Children = append(node.Children, t.convert(ast.Table))
	return node
}

func (t *treeFormatter) convertUnaryOp(expr *UnaryOp) *tree.Node {
	node := tree.NewNode("UnaryOp", expr.Name(),
		tree.NewProperty("op", false, expr.Op.String()),
		tree.NewProperty("left", false, expr.Value.Name()),
	)
	node.Children = append(node.Children, t.convert(expr.Value))
	return node
}

func (t *treeFormatter) convertBinOp(expr *BinOp) *tree.Node {
	node := tree.NewNode("BinOp", expr.Name(),
		tree.NewProperty("op", false, expr.Op.String()),
		tree.NewProperty("left", false, expr.Left.Name()),
		tree.NewProperty("right", false, expr.Right.Name()),
	)
	node.Children = append(node.Children, t.convert(expr.Left))
	node.Children = append(node.Children, t.convert(expr.Right))
	return node
}

func (t *treeFormatter) convertColumnRef(expr *ColumnRef) *tree.Node {
	return tree.NewNode("ColumnRef", "",
		tree.NewProperty("column", false, expr.Ref.Column),
		tree.NewProperty("type", false, expr.Ref.Type),
	)
}

func (t *treeFormatter) convertLiteral(expr *Literal) *tree.Node {
	return tree.NewNode("Literal", "",
		tree.NewProperty("value", false, expr.String()),
		tree.NewProperty("kind", false, expr.Kind()),
	)
}

func (t *treeFormatter) convertRangeAggregation(r *RangeAggregation) *tree.Node {
	properties := []tree.Property{
		tree.NewProperty("table", false, r.Table.Name()),
		tree.NewProperty("operation", false, r.Operation),
		tree.NewProperty("start_ts", false, util.FormatTimeRFC3339Nano(r.Start)),
		tree.NewProperty("end_ts", false, util.FormatTimeRFC3339Nano(r.End)),
		tree.NewProperty("step", false, r.Step),
		tree.NewProperty("range", false, r.RangeInterval),
	}

	if len(r.PartitionBy) > 0 {
		partitionBy := make([]any, len(r.PartitionBy))
		for i := range r.PartitionBy {
			partitionBy[i] = r.PartitionBy[i].Name()
		}

		properties = append(properties, tree.NewProperty("partition_by", true, partitionBy...))
	}

	node := tree.NewNode("RangeAggregation", r.Name(), properties...)
	for _, columnRef := range r.PartitionBy {
		node.Comments = append(node.Comments, t.convert(&columnRef))
	}
	node.Children = append(node.Children, t.convert(r.Table))

	return node
}

func (t *treeFormatter) convertVectorAggregation(v *VectorAggregation) *tree.Node {
	properties := []tree.Property{
		tree.NewProperty("table", false, v.Table.Name()),
		tree.NewProperty("operation", false, v.Operation),
	}

	if len(v.GroupBy) > 0 {
		groupBy := make([]any, len(v.GroupBy))
		for i := range v.GroupBy {
			groupBy[i] = v.GroupBy[i].Name()
		}

		properties = append(properties, tree.NewProperty("group_by", true, groupBy...))
	}

	node := tree.NewNode("VectorAggregation", v.Name(), properties...)
	for _, columnRef := range v.GroupBy {
		node.Comments = append(node.Comments, t.convert(&columnRef))
	}
	node.Children = append(node.Children, t.convert(v.Table))

	return node
}
