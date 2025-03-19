package logical

import (
	"fmt"
	"strings"

	"github.com/grafana/loki/v3/pkg/engine/planner/internal/tree"
)

// TreeFormatter formats a logical plan as a tree structure.
type TreeFormatter struct{}

// Format formats a logical plan as a tree structure. It takes a [Value] as
// input and returns a tree representation of that Value and all Values it
// depends on.
func (t *TreeFormatter) Format(value Value) string {
	var sb strings.Builder
	p := tree.NewPrinter(&sb)
	p.Print(t.convert(value))
	return sb.String()
}

// convert dispatches to the appropriate method based on the Instruction and
// returns the newly created [tree.Node].
func (t *TreeFormatter) convert(value Value) *tree.Node {
	switch value := value.(type) {
	case *MakeTable:
		return t.convertMakeTable(value)
	case *Select:
		return t.convertSelect(value)
	case *Limit:
		return t.convertLimit(value)
	case *Sort:
		return t.convertSort(value)
	default:
		panic(fmt.Sprintf("unknown value type %T", value))
	}
}

func (t *TreeFormatter) convertMakeTable(ast *MakeTable) *tree.Node {
	return tree.NewNode("MakeTable", "", tree.Property{Key: "name", Values: []any{ast.TableName}})
}

func (t *TreeFormatter) convertSelect(ast *Select) *tree.Node {
	node := tree.NewNode("Select", "", tree.NewProperty("expr", false, ast.Expr.ToField(ast.Input).Name))
	node.Comments = append(node.Comments, t.convertExpr(ast.Expr))
	node.Children = append(node.Children, t.convert(unwrapTreeNode(ast.Input)))
	return node
}

func (t *TreeFormatter) convertLimit(ast *Limit) *tree.Node {
	node := tree.NewNode("Limit", "",
		tree.NewProperty("offset", false, ast.Skip),
		tree.NewProperty("fetch", false, ast.Fetch),
	)
	node.Children = append(node.Children, t.convert(unwrapTreeNode(ast.Input)))
	return node
}

func (t *TreeFormatter) convertSort(ast *Sort) *tree.Node {
	direction := "asc"
	if !ast.Expr.Ascending {
		direction = "desc"
	}

	nullsPosition := "last"
	if ast.Expr.NullsFirst {
		nullsPosition = "first"
	}

	node := tree.NewNode("Sort", "",
		tree.NewProperty("expr", false, ast.Expr.Name),
		tree.NewProperty("direction", false, direction),
		tree.NewProperty("nulls", false, nullsPosition),
	)
	node.Comments = append(node.Comments, t.convertExpr(ast.Expr.Expr))
	node.Children = append(node.Children, t.convert(unwrapTreeNode(ast.Input)))
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
	return tree.NewNode("Column", expr.Name)
}

func (t *TreeFormatter) convertLiteralExpr(expr *LiteralExpr) *tree.Node {
	return tree.NewNode("Literal", "",
		tree.NewProperty("value", false, expr.ValueString()),
		tree.NewProperty("type", false, expr.ValueType()),
	)
}

func (t *TreeFormatter) convertBinaryOpExpr(expr *BinOpExpr) *tree.Node {
	node := tree.NewNode(ExprTypeBinaryOp.String(), "",
		tree.NewProperty("type", false, expr.Type.String()),
		tree.NewProperty("op", false, fmt.Sprintf(`"%s"`, expr.OpStringer())),
		tree.NewProperty("name", false, expr.Name),
	)
	node.Children = append(node.Children, t.convertExpr(expr.Left))
	node.Children = append(node.Children, t.convertExpr(expr.Right))
	return node
}

func unwrapTreeNode(tn TreeNode) Value {
	switch tn.Type() {
	case TreeNodeTypeMakeTable:
		return tn.MakeTable()
	case TreeNodeTypeSelect:
		return tn.Select()
	case TreeNodeTypeLimit:
		return tn.Limit()
	case TreeNodeTypeSort:
		return tn.Sort()
	default:
		panic(fmt.Sprintf("unknown TreeNode type: %v", tn.Type()))
	}
}
