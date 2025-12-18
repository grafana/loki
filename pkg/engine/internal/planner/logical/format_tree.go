package logical

import (
	"fmt"
	"io"

	"github.com/grafana/loki/v3/pkg/engine/internal/util"
	"github.com/grafana/loki/v3/pkg/engine/internal/util/tree"
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
	case *TopK:
		return t.convertTopK(value)
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

	case *LogQLCompat:
		return tree.NewNode("LOGQL_COMPAT", value.Name())

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

func (t *treeFormatter) convertTopK(ast *TopK) *tree.Node {
	direction := "asc"
	if !ast.Ascending {
		direction = "desc"
	}

	nullsPosition := "last"
	if ast.NullsFirst {
		nullsPosition = "first"
	}

	node := tree.NewNode("TOPK", ast.Name(),
		tree.NewProperty("table", false, ast.Table.Name()),
		tree.NewProperty("sort_by", false, ast.SortBy.Name()),
		tree.NewProperty("k", false, ast.K),
		tree.NewProperty("direction", false, direction),
		tree.NewProperty("nulls", false, nullsPosition),
	)
	node.Comments = append(node.Comments, t.convert(ast.SortBy))
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

	grouping := make([]any, len(r.Grouping.Columns))
	if len(r.Grouping.Columns) > 0 {
		for i := range r.Grouping.Columns {
			grouping[i] = r.Grouping.Columns[i].Name()
		}
	}
	if r.Grouping.Without {
		properties = append(properties, tree.NewProperty("group_without", true, grouping...))
	} else {
		properties = append(properties, tree.NewProperty("group_by", true, grouping...))
	}

	node := tree.NewNode("RangeAggregation", r.Name(), properties...)
	for _, columnRef := range r.Grouping.Columns {
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

	grouping := make([]any, len(v.Grouping.Columns))
	if len(v.Grouping.Columns) > 0 {
		for i := range v.Grouping.Columns {
			grouping[i] = v.Grouping.Columns[i].Name()
		}
	}
	if v.Grouping.Without {
		properties = append(properties, tree.NewProperty("group_without", true, grouping...))
	} else {
		properties = append(properties, tree.NewProperty("group_by", true, grouping...))
	}

	node := tree.NewNode("VectorAggregation", v.Name(), properties...)
	for _, columnRef := range v.Grouping.Columns {
		node.Comments = append(node.Comments, t.convert(&columnRef))
	}
	node.Children = append(node.Children, t.convert(v.Table))

	return node
}
