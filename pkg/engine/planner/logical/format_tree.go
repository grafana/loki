package logical

import (
	"fmt"
	"strings"

	"github.com/grafana/loki/v3/pkg/engine/planner/internal/tree"
)

// TreeFormatter formats a logical plan as a tree structure.
type TreeFormatter struct{}

// Format formats a logical plan as a tree structure, similar to the Unix 'tree' command.
// It takes a [Plan] as input and returns a string representation of the plan tree.
func (t *TreeFormatter) Format(ast Plan) string {
	var sb strings.Builder
	p := tree.NewPrinter(&sb)
	p.Print(t.convert(ast))
	return sb.String()
}

// convert dispatches to the appropriate method based on the plan type and
// returns the newly created [tree.Node].
func (t *TreeFormatter) convert(ast Plan) *tree.Node {
	switch ast.Type() {
	case PlanTypeMakeTable:
		return t.convertMakeTable(ast.MakeTable())
	case PlanTypeFilter:
		return t.convertFilter(ast.Filter())
	case PlanTypeLimit:
		return t.convertLimit(ast.Limit())
	case PlanTypeSort:
		return t.convertSort(ast.Sort())
	default:
		panic(fmt.Sprintf("unknown plan type: %v", ast.Type()))
	}
}

func (t *TreeFormatter) convertMakeTable(ast *MakeTable) *tree.Node {
	return tree.NewNode("MakeTable", "", tree.Property{Key: "name", Values: []any{ast.TableName()}})
}

func (t *TreeFormatter) convertFilter(ast *Filter) *tree.Node {
	node := tree.NewNode("Filter", "", tree.NewProperty("expr", false, ast.FilterExpr().ToField(ast.Child()).Name))
	node.Comments = append(node.Comments, t.convertExpr(ast.FilterExpr()))
	node.Children = append(node.Children, t.convert(ast.Child()))
	return node
}

func (t *TreeFormatter) convertLimit(ast *Limit) *tree.Node {
	node := tree.NewNode("Limit", "",
		tree.NewProperty("offset", false, ast.Skip()),
		tree.NewProperty("fetch", false, ast.Fetch()),
	)
	node.Children = append(node.Children, t.convert(ast.Child()))
	return node
}

func (t *TreeFormatter) convertSort(ast *Sort) *tree.Node {
	direction := "asc"
	if !ast.Expr().Asc() {
		direction = "desc"
	}

	nullsPosition := "last"
	if ast.Expr().NullsFirst() {
		nullsPosition = "first"
	}

	node := tree.NewNode("Sort", "",
		tree.NewProperty("expr", false, ast.Expr().Name()),
		tree.NewProperty("direction", false, direction),
		tree.NewProperty("nulls", false, nullsPosition),
	)
	node.Comments = append(node.Comments, t.convertExpr(ast.Expr().Expr()))
	node.Children = append(node.Children, t.convert(ast.Child()))
	return node
}

// convert dispatches to the appropriate method based on the expression type and
// returns the newly created [tree.Node], which can be used as comment for the
// parent node.
func (t *TreeFormatter) convertExpr(expr Expr) *tree.Node {
	switch expr.Type() {
	case ExprTypeColumn:
		return t.convertColumnExpr(expr.Column())
	case ExprTypeLiteral:
		return t.convertLiteralExpr(expr.Literal())
	case ExprTypeBinaryOp:
		return t.convertBinaryOpExpr(expr.BinaryOp())
	default:
		panic(fmt.Sprintf("unknown expr type: (named: %v, type: %T)", expr.Type(), expr))
	}
}

func (t *TreeFormatter) convertColumnExpr(expr *ColumnExpr) *tree.Node {
	return tree.NewNode("Column", expr.ColumnName())
}

func (t *TreeFormatter) convertLiteralExpr(expr *LiteralExpr) *tree.Node {
	return tree.NewNode("Literal", "",
		tree.NewProperty("value", false, expr.ValueString()),
		tree.NewProperty("type", false, expr.ValueType()),
	)
}

func (t *TreeFormatter) convertBinaryOpExpr(expr *BinOpExpr) *tree.Node {
	node := tree.NewNode(ExprTypeBinaryOp.String(), "",
		tree.NewProperty("type", false, expr.Type().String()),
		tree.NewProperty("op", false, fmt.Sprintf(`"%s"`, expr.Op())),
		tree.NewProperty("name", false, expr.Name()),
	)
	node.Children = append(node.Children, t.convertExpr(expr.Left()))
	node.Children = append(node.Children, t.convertExpr(expr.Right()))
	return node
}
