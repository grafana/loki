package logical

import (
	"fmt"
	"strings"
)

// SSANode represents a single node in the SSA (Static Single Assignment) form
// Each node has a unique ID, a type, and ordered properties and references to other nodes
type SSANode struct {
	// ID is the unique identifier for this node
	ID int
	// NodeType is the type of this node (e.g., "MakeTable", "ColumnRef", etc.)
	NodeType string
	// Tuples represents the ordered properties of this node
	Tuples []nodeProperty
	// References to other nodes in the SSA form
	References []int
}

// nodeProperty represents a key-value property of an SSA node
type nodeProperty struct {
	Key   string
	Value string
}

// String returns a string representation of this node
// Format: %ID = NodeType [prop1=value1, prop2=value2, ...]
func (n *SSANode) String() string {
	var sb strings.Builder

	// Format the node ID and type
	sb.WriteString(fmt.Sprintf("%%%d = %s", n.ID, n.NodeType))

	// Add properties in brackets if any exist
	if len(n.Tuples) > 0 {
		sb.WriteString(" [")

		// Properties are already in the correct order
		for i, prop := range n.Tuples {
			if i > 0 {
				sb.WriteString(", ")
			}
			sb.WriteString(fmt.Sprintf("%s=%s", prop.Key, prop.Value))
		}

		sb.WriteString("]")
	}

	return sb.String()
}

// SSAForm represents a full query plan in SSA form
// It contains a list of nodes and the ID of the root node
type SSAForm struct {
	// nodes is an ordered list of SSA nodes, where each node's dependencies
	// are guaranteed to appear earlier in the list
	nodes []SSANode
}

// ConvertToSSA converts a logical plan to SSA form
// It performs a post-order traversal of the plan, adding nodes as it goes
func ConvertToSSA(plan Plan) (*SSAForm, error) {
	// Initialize the builder with an empty node at index 0
	builder := &ssaBuilder{
		nodes:     []SSANode{{}}, // Start with an empty node at index 0
		nodeMap:   make(map[string]int),
		nextID:    1,
		exprTypes: make(map[Expr]string),
	}

	_, err := builder.processPlan(plan)
	if err != nil {
		return nil, fmt.Errorf("error converting plan to SSA: %w", err)
	}

	return &SSAForm{
		nodes: builder.nodes,
	}, nil
}

// ssaBuilder is a helper type for building SSA forms
type ssaBuilder struct {
	nodes     []SSANode
	nodeMap   map[string]int // Maps node key to its ID
	nextID    int
	exprTypes map[Expr]string // Cache for expression types
}

// getID generates a unique ID for a node
func (b *ssaBuilder) getID() int {
	id := b.nextID
	b.nextID++
	return id
}

// processPlan processes a logical plan and returns the ID of the resulting SSA node.
func (b *ssaBuilder) processPlan(plan Plan) (int, error) {
	switch plan.Type() {
	case PlanTypeMakeTable:
		return b.processMakeTablePlan(plan.MakeTable())
	case PlanTypeSelect:
		return b.processSelectPlan(plan.Select())
	case PlanTypeLimit:
		return b.processLimitPlan(plan.Limit())
	case PlanTypeSort:
		return b.processSortPlan(plan.Sort())
	default:
		return 0, fmt.Errorf("unsupported plan type: %v", plan.Type())
	}
}

// processMakeTablePlan processes a maketable plan node
// It creates a MakeTable node with the table name
func (b *ssaBuilder) processMakeTablePlan(plan *MakeTable) (int, error) {
	// Create a node for the table
	id := b.getID()
	node := SSANode{
		ID:       id,
		NodeType: "MakeTable",
		Tuples: []nodeProperty{
			{Key: "name", Value: plan.TableName()},
		},
	}

	b.nodes = append(b.nodes, node)
	return id, nil
}

// processSelectPlan processes a select plan node
// It processes the child plan and select expression, then creates a Select node
func (b *ssaBuilder) processSelectPlan(plan *Select) (int, error) {
	// Process the child plan first
	childID, err := b.processPlan(plan.Child())
	if err != nil {
		return 0, err
	}

	// Process the select expression
	exprID, err := b.processExpr(plan.SelectExpr(), plan.Child())
	if err != nil {
		return 0, err
	}

	// Get the name of the expression
	var exprName string

	if plan.SelectExpr().Type() == ExprTypeBinaryOp {
		exprName = plan.SelectExpr().BinaryOp().Name
	} else {
		exprName = plan.SelectExpr().ToField(plan.Child()).Name
	}

	// Create a node for the select
	id := b.getID()
	node := SSANode{
		ID:       id,
		NodeType: "Select",
		Tuples: []nodeProperty{
			{Key: "name", Value: exprName},
			{Key: "predicate", Value: fmt.Sprintf("%%%d", exprID)},
			{Key: "plan", Value: fmt.Sprintf("%%%d", childID)},
		},
		References: []int{exprID, childID},
	}

	b.nodes = append(b.nodes, node)
	return id, nil
}

// processExpr processes an expression and returns its ID
// It handles different expression types by delegating to specific processing methods
func (b *ssaBuilder) processExpr(expr Expr, parent Plan) (int, error) {
	switch expr.Type() {
	case ExprTypeColumn:
		return b.processColumnExpr(expr.Column(), parent)
	case ExprTypeLiteral:
		return b.processLiteralExpr(expr.Literal())
	case ExprTypeBinaryOp:
		return b.processBinaryOpExpr(expr.BinaryOp(), parent)
	default:
		return 0, fmt.Errorf("unknown expression type: %v", expr.Type())
	}
}

// processColumnExpr processes a column expression
// It creates a ColumnRef node with the column name and type
func (b *ssaBuilder) processColumnExpr(expr *ColumnExpr, parent Plan) (int, error) {
	field := expr.ToField(parent)

	// Create a node for the column reference
	id := b.getID()
	node := SSANode{
		ID:       id,
		NodeType: "ColumnRef",
		Tuples: []nodeProperty{
			{Key: "name", Value: field.Name},
			{Key: "type", Value: field.Type.String()},
		},
	}

	b.nodes = append(b.nodes, node)
	return id, nil
}

// processLiteralExpr processes a literal expression
// It creates a Literal node with the value and type
func (b *ssaBuilder) processLiteralExpr(expr *LiteralExpr) (int, error) {
	// Create a node for the literal
	id := b.getID()
	node := SSANode{
		ID:       id,
		NodeType: "Literal",
		Tuples: []nodeProperty{
			{Key: "val", Value: expr.ValueString()},
			{Key: "type", Value: expr.ValueType().String()},
		},
	}

	b.nodes = append(b.nodes, node)
	return id, nil
}

// processBinaryOpExpr processes a binary operation expression
// It processes the left and right operands, then creates a BinaryOp node
func (b *ssaBuilder) processBinaryOpExpr(expr *BinOpExpr, parent Plan) (int, error) {
	// Process the left and right operands first
	leftID, err := b.processExpr(expr.Left, parent)
	if err != nil {
		return 0, err
	}

	rightID, err := b.processExpr(expr.Right, parent)
	if err != nil {
		return 0, err
	}

	// Create a node for the binary operation
	id := b.getID()
	node := SSANode{
		ID:       id,
		NodeType: "BinaryOp",
		Tuples: []nodeProperty{
			{Key: "op", Value: fmt.Sprintf("(%s)", expr.OpStringer().String())},
			{Key: "name", Value: expr.Name},
			{Key: "left", Value: fmt.Sprintf("%%%d", leftID)},
			{Key: "right", Value: fmt.Sprintf("%%%d", rightID)},
		},
		References: []int{leftID, rightID},
	}

	b.nodes = append(b.nodes, node)
	return id, nil
}

// processLimitPlan processes a limit plan and returns the ID of the resulting SSA node.
// This converts a Limit logical plan node to its SSA representation.
//
// The SSA node for a Limit plan has the following format:
//
//	%ID = Limit [Skip=X, Fetch=Y]
//
// Where X is the number of rows to skip and Y is the maximum number of rows to return.
// The Limit node always includes both Skip and Fetch properties, even when they are zero.
// This ensures consistent representation and allows for optimization in a separate step.
//
// The Limit node references its input plan as a dependency.
func (b *ssaBuilder) processLimitPlan(plan *Limit) (int, error) {
	// Process the input plan
	inputID, err := b.processPlan(plan.Input)
	if err != nil {
		return 0, fmt.Errorf("failed to process limit input plan: %w", err)
	}

	// Create properties for the limit node
	tuples := []nodeProperty{
		{
			Key:   "Skip",
			Value: fmt.Sprintf("%d", plan.Skip),
		},
		{
			Key:   "Fetch",
			Value: fmt.Sprintf("%d", plan.Fetch),
		},
	}

	// Create the limit node
	id := b.getID()
	b.nodes = append(b.nodes, SSANode{
		ID:         id,
		NodeType:   "Limit",
		Tuples:     tuples,
		References: []int{inputID},
	})
	return id, nil
}

// processSortPlan processes a sort plan and returns the ID of the resulting SSA node.
// This converts a Sort logical plan node to its SSA representation.
//
// The SSA node for a Sort plan has the following format:
//
//	%ID = Sort [expr=X, direction=Y, nulls=Z]
//
// Where X is the name of the sort expression, Y is the sort direction, and Z is the nulls position.
// The Sort node references its input plan and sort expression as dependencies.
func (b *ssaBuilder) processSortPlan(plan *Sort) (int, error) {
	// Process the child plan first
	childID, err := b.processPlan(plan.Child())
	if err != nil {
		return 0, err
	}

	// Process the sort expression
	exprID, err := b.processExpr(plan.Expr().Expr(), plan.Child())
	if err != nil {
		return 0, err
	}

	// Create direction and nulls position properties
	direction := "asc"
	if !plan.Expr().Asc() {
		direction = "desc"
	}

	nullsPosition := "last"
	if plan.Expr().NullsFirst() {
		nullsPosition = "first"
	}

	// Create the Sort node
	id := b.getID()
	node := SSANode{
		ID:       id,
		NodeType: "Sort",
		Tuples: []nodeProperty{
			{Key: "expr", Value: plan.Expr().Name()},
			{Key: "direction", Value: direction},
			{Key: "nulls", Value: nullsPosition},
		},
		References: []int{exprID, childID},
	}

	b.nodes = append(b.nodes, node)
	return id, nil
}

// String returns a string representation of the SSA form with the RETURN statement
func (f *SSAForm) String() string {
	if len(f.nodes) <= 1 {
		return ""
	}

	// The root is the last node added
	lastNodeID := f.nodes[len(f.nodes)-1].ID
	return f.Format() + fmt.Sprintf("\nRETURN %%%d", lastNodeID)
}

// Format returns a formatted string representation of the SSA form
// It includes all nodes but not the RETURN statement
func (f *SSAForm) Format() string {
	var sb strings.Builder

	// Add each node (skip node 0 if it exists)
	for i := 1; i < len(f.nodes); i++ {
		if i > 1 {
			sb.WriteString("\n")
		}
		sb.WriteString(f.nodes[i].String())
	}

	return sb.String()
}

// Nodes returns a map of node IDs to their string representations
// This is primarily used for testing
func (f *SSAForm) Nodes() map[int]string {
	result := make(map[int]string)
	for _, node := range f.nodes {
		if node.ID > 0 {
			result[node.ID] = node.String()
		}
	}
	return result
}
