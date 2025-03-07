package logical

import (
	"fmt"
	"strings"
)

const (
	treeNodeSignal     = "├── "
	treeLastNodeSignal = "└── "
	treeContinueSignal = "│   "
	treeSpaceSignal    = "    "
)

// A formatter that formats a tree, similar to the `tree` command in Unix
type treeFormatter struct {
	node     treeNode
	children []*treeFormatter
	parent   *treeFormatter
	isRoot   bool
}

// newTreeFormatter creates a new root treeFormatter
func newTreeFormatter() *treeFormatter {
	return &treeFormatter{isRoot: true}
}

// WriteNode implements Formatter
func (t *treeFormatter) writeNode(node treeNode) *treeFormatter {
	child := &treeFormatter{
		node:   node,
		parent: t,
	}
	t.children = append(t.children, child)
	return child
}

// writePlan writes a plan node to the tree formatter.
// It dispatches to the appropriate write method based on the plan type.
func (t *treeFormatter) writePlan(ast Plan) {
	switch ast.Type() {
	case PlanTypeTable:
		t.writeTablePlan(ast.Table())
	case PlanTypeFilter:
		t.writeFilterPlan(ast.Filter())
	case PlanTypeProjection:
		t.writeProjectionPlan(ast.Projection())
	case PlanTypeAggregate:
		t.writeAggregatePlan(ast.Aggregate())
	case PlanTypeLimit:
		t.writeLimitPlan(ast.Limit())
	case PlanTypeSort:
		t.writeSortPlan(ast.Sort())
	default:
		panic(fmt.Sprintf("unknown plan type: %v", ast.Type()))
	}
}

func (t *treeFormatter) writeTablePlan(ast *MakeTable) {
	n := treeNode{
		Singletons: []string{"MakeTable"},
		Tuples: []treeContentTuple{{
			Key:   "name",
			Value: SingleContent(ast.TableName()),
		}},
	}
	t.writeNode(n)
}

func (t *treeFormatter) writeFilterPlan(ast *Filter) {
	n := treeNode{
		Singletons: []string{"Filter"},
		Tuples: []treeContentTuple{{
			Key:   "expr",
			Value: SingleContent(ast.FilterExpr().ToField(ast.Child()).Name),
		}},
	}

	nextFM := t.writeNode(n)
	nextFM.writeExpr(ast.FilterExpr())
	nextFM.writePlan(ast.Child())
}

func (t *treeFormatter) writeProjectionPlan(ast *Projection) {
	var tuples []treeContentTuple
	for _, expr := range ast.ProjectExprs() {
		field := expr.ToField(ast.Child())
		tuples = append(tuples, treeContentTuple{
			Key:   field.Name,
			Value: SingleContent(field.Type.String()),
		})
	}

	n := treeNode{
		Singletons: []string{"Projection"},
		Tuples:     tuples,
	}

	nextFM := t.writeNode(n)
	for _, expr := range ast.ProjectExprs() {
		nextFM.writeExpr(expr)
	}
	nextFM.writePlan(ast.Child())
}

func (t *treeFormatter) writeAggregatePlan(ast *Aggregate) {
	// Collect grouping names
	var groupNames []string
	for _, expr := range ast.GroupExprs() {
		groupNames = append(groupNames, expr.ToField(ast.Child()).Name)
	}

	// Collect aggregate names
	var aggNames []string
	for _, expr := range ast.AggregateExprs() {
		aggNames = append(aggNames, expr.ToField(ast.Child()).Name)
	}

	n := treeNode{
		Singletons: []string{"Aggregate"},
		Tuples: []treeContentTuple{
			{
				Key:   "groupings",
				Value: treeListContentFrom(groupNames...),
			},
			{
				Key:   "aggregates",
				Value: treeListContentFrom(aggNames...),
			},
		},
	}

	nextFM := t.writeNode(n)

	// Format grouping expressions
	groupNode := treeNode{Singletons: []string{"GroupExprs"}}
	groupFM := nextFM.writeNode(groupNode)
	for _, expr := range ast.GroupExprs() {
		groupFM.writeExpr(expr)
	}

	// Format aggregate expressions
	aggNode := treeNode{Singletons: []string{"AggregateExprs"}}
	aggFM := nextFM.writeNode(aggNode)
	for _, expr := range ast.AggregateExprs() {
		aggFM.writeAggregateExpr(&expr)
	}

	// format input plan
	nextFM.writePlan(ast.Child())
}

// writeLimitPlan writes a limit plan node to the tree formatter.
// It formats the limit plan with its skip and fetch values.
//
// The limit plan is represented in the tree as:
//
//	Limit offset=X fetch=Y
//	└── [Child Plan]
//
// Where X is the number of rows to skip (offset) and Y is the maximum
// number of rows to return (fetch). If offset is 0, no rows are skipped.
// If fetch is 0, all rows are returned after applying the offset.
func (t *treeFormatter) writeLimitPlan(ast *Limit) {

	n := treeNode{
		Singletons: []string{"Limit"},
		Tuples: []treeContentTuple{
			{
				Key:   "offset",
				Value: SingleContent(fmt.Sprintf("%d", ast.Skip())),
			},
			{
				Key:   "fetch",
				Value: SingleContent(fmt.Sprintf("%d", ast.Fetch())),
			},
		},
	}

	nextFM := t.writeNode(n)
	nextFM.writePlan(ast.Child())
}

func (t *treeFormatter) writeSortPlan(ast *Sort) {
	direction := "asc"
	if !ast.Expr().Asc() {
		direction = "desc"
	}

	nullsPosition := "last"
	if ast.Expr().NullsFirst() {
		nullsPosition = "first"
	}

	n := treeNode{
		Singletons: []string{"Sort"},
		Tuples: []treeContentTuple{
			{
				Key:   "expr",
				Value: SingleContent(ast.Expr().Name()),
			},
			{
				Key:   "direction",
				Value: SingleContent(direction),
			},
			{
				Key:   "nulls",
				Value: SingleContent(nullsPosition),
			},
		},
	}

	nextFM := t.writeNode(n)
	nextFM.writeExpr(ast.Expr().Expr())
	nextFM.writePlan(ast.Child())
}

func (t *treeFormatter) writeExpr(expr Expr) {
	switch expr.Type() {
	case ExprTypeColumn:
		t.writeColumnExpr(expr.Column())
	case ExprTypeLiteral:
		t.writeLiteralExpr(expr.Literal())
	case ExprTypeBinaryOp:
		t.writeBinaryOpExpr(expr.BinaryOp())
	case ExprTypeAggregate:
		t.writeAggregateExpr(expr.Aggregate())
	default:
		panic(fmt.Sprintf("unknown expr type: (named: %v, type: %T)", expr.Type(), expr))
	}
}

func (t *treeFormatter) writeColumnExpr(expr *ColumnExpr) {
	node := treeNode{
		Singletons: []string{"Column", fmt.Sprintf("#%s", expr.ColumnName())},
	}
	t.writeNode(node)
}

func (t *treeFormatter) writeLiteralExpr(expr *LiteralExpr) {
	node := treeNode{
		Singletons: []string{"Literal"},
		Tuples: []treeContentTuple{
			{
				Key:   "value",
				Value: SingleContent(expr.ValueString()),
			},
			{
				Key:   "type",
				Value: SingleContent(expr.ValueType().String()),
			},
		},
	}
	t.writeNode(node)
}

func (t *treeFormatter) writeBinaryOpExpr(expr *BinOpExpr) {
	wrapped := fmt.Sprintf("(%v)", expr.Op()) // for clarity
	n := treeNode{
		Singletons: []string{ExprTypeBinaryOp.String()},
		Tuples: []treeContentTuple{
			{
				Key:   "type",
				Value: SingleContent(expr.Type().String()),
			},
			{
				Key:   "op",
				Value: SingleContent(wrapped),
			}, {
				Key:   "name",
				Value: SingleContent(expr.Name()),
			},
		},
	}

	nextFM := t.writeNode(n)
	nextFM.writeExpr(expr.Left())
	nextFM.writeExpr(expr.Right())
}

func (t *treeFormatter) writeAggregateExpr(expr *AggregateExpr) {
	n := treeNode{
		Singletons: []string{"AggregateExpr"},
		Tuples: []treeContentTuple{{
			Key:   "op",
			Value: SingleContent(expr.Op()),
		}},
	}

	nextFM := t.writeNode(n)
	nextFM.writeExpr(expr.SubExpr())
}

// Format builds the tree and returns the formatted string
func (t *treeFormatter) Format(ast Plan) string {
	var sb strings.Builder
	t.writePlan(ast)
	t.format(&sb, "")
	return sb.String()
}

func (t *treeFormatter) format(sb *strings.Builder, indent string) {
	// Root node just formats children
	if t.isRoot {
		if len(t.children) > 0 {
			t.children[0].format(sb, "")
		}
		return
	}

	// Write node content
	for i, s := range t.node.Singletons {
		if i > 0 {
			sb.WriteByte(' ')
		}
		sb.WriteString(s)
	}

	if len(t.node.Tuples) > 0 {
		if len(t.node.Singletons) > 0 {
			sb.WriteByte(' ')
		}
		for i, tuple := range t.node.Tuples {
			if i > 0 {
				sb.WriteByte(' ')
			}
			sb.WriteString(tuple.Content())
		}
	}

	// Format children with proper tree characters
	for i, child := range t.children {
		sb.WriteByte('\n')
		sb.WriteString(indent)

		nextIndent := indent + treeContinueSignal
		if i == len(t.children)-1 {
			sb.WriteString(treeLastNodeSignal)
			nextIndent = indent + treeSpaceSignal
		} else {
			sb.WriteString(treeNodeSignal)
		}

		child.format(sb, nextIndent)
	}
}

// A node to be formatted in the tree formatter
type treeNode struct {
	// e.g. "SELECT"
	Singletons []string
	// e.g. "GROUP BY (Foo, Bar)",
	// "foo=bar"
	Tuples []treeContentTuple
}

type treeContent interface {
	Content() string
}

type treeContentTuple struct {
	Key   string
	Value treeContent
}

func (t treeContentTuple) Content() string {
	var sb strings.Builder
	sb.WriteString(t.Key)
	sb.WriteString("=")
	sb.WriteString(t.Value.Content())
	return sb.String()
}

type treeListContent []treeContent

func treeListContentFrom(values ...string) treeListContent {
	var contents []treeContent
	for _, value := range values {
		contents = append(contents, SingleContent(value))
	}
	return treeListContent(contents)
}

func (g treeListContent) Content() string {
	var sb strings.Builder
	sb.WriteString("(")
	for i, c := range g {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(c.Content())
	}
	sb.WriteString(")")
	return sb.String()
}

type SingleContent string

func (s SingleContent) Content() string {
	return string(s)
}
