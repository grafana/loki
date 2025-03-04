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

// processPlan processes a plan node and returns its ID
// It handles different plan types by delegating to specific processing methods
func (b *ssaBuilder) processPlan(plan Plan) (int, error) {
	switch plan.Category() {
	case PlanCategoryTable:
		return b.processTablePlan(plan.(tableNode))
	case PlanCategoryFilter:
		return b.processFilterPlan(plan.(filterNode))
	case PlanCategoryProjection:
		return b.processProjectionPlan(plan.(projectionNode))
	case PlanCategoryAggregate:
		return b.processAggregatePlan(plan.(aggregateNode))
	default:
		return 0, fmt.Errorf("unknown plan type: %v", plan.Category())
	}
}

// processTablePlan processes a table plan node
// It creates a MakeTable node with the table name
func (b *ssaBuilder) processTablePlan(plan tableNode) (int, error) {
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

// processFilterPlan processes a filter plan node
// It processes the child plan and filter expression, then creates a Filter node
func (b *ssaBuilder) processFilterPlan(plan filterNode) (int, error) {
	// Process the child plan first
	childID, err := b.processPlan(plan.Child())
	if err != nil {
		return 0, err
	}

	// Process the filter expression
	exprID, err := b.processExpr(plan.FilterExpr(), plan.Child())
	if err != nil {
		return 0, err
	}

	// Get the name of the expression
	var exprName string
	if binaryOp, ok := plan.FilterExpr().(binaryOpExpr); ok {
		exprName = binaryOp.Name()
	} else {
		exprName = plan.FilterExpr().ToField(plan.Child()).Name
	}

	// Create a node for the filter
	id := b.getID()
	node := SSANode{
		ID:       id,
		NodeType: "Filter",
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

// processProjectionPlan processes a projection plan node
// It processes the child plan and all projection expressions, then creates a Project node
func (b *ssaBuilder) processProjectionPlan(plan projectionNode) (int, error) {
	// Process the child plan first
	childID, err := b.processPlan(plan.Child())
	if err != nil {
		return 0, err
	}

	// Process all projection expressions
	var props []nodeProperty
	var references []int

	// Process expressions and build properties in a stable order
	// determined by the order of expressions in the plan
	for _, expr := range plan.ProjectExprs() {
		exprID, err := b.processExpr(expr, plan.Child())
		if err != nil {
			return 0, err
		}

		field := expr.ToField(plan.Child())
		props = append(props, nodeProperty{
			Key:   field.Name,
			Value: fmt.Sprintf("%%%d", exprID),
		})
		references = append(references, exprID)
	}

	// Create a node for the projection
	id := b.getID()
	node := SSANode{
		ID:         id,
		NodeType:   "Project",
		Tuples:     props,
		References: append(references, childID), // Add childID to references
	}

	b.nodes = append(b.nodes, node)
	return id, nil
}

// processAggregatePlan processes an aggregate plan node
// It processes the child plan, group expressions, and aggregate expressions,
// then creates an AggregatePlan node
func (b *ssaBuilder) processAggregatePlan(plan aggregateNode) (int, error) {
	// Process the child plan first
	_, err := b.processPlan(plan.Child())
	if err != nil {
		return 0, err
	}

	// Process group expressions
	var groupingRefs []string
	var groupingIDs []int

	for _, expr := range plan.GroupExprs() {
		exprID, err := b.processExpr(expr, plan.Child())
		if err != nil {
			return 0, err
		}

		groupingRefs = append(groupingRefs, fmt.Sprintf("%%%d", exprID))
		groupingIDs = append(groupingIDs, exprID)
	}

	// Process aggregate expressions
	var aggregationRefs []string
	var aggregationIDs []int

	for _, expr := range plan.AggregateExprs() {
		exprID, err := b.processAggregateExpr(expr, plan.Child())
		if err != nil {
			return 0, err
		}

		aggregationRefs = append(aggregationRefs, fmt.Sprintf("%%%d", exprID))
		aggregationIDs = append(aggregationIDs, exprID)
	}

	// Create a node for the aggregate plan
	id := b.getID()
	node := SSANode{
		ID:       id,
		NodeType: "AggregatePlan",
		Tuples: []nodeProperty{
			{Key: "aggregations", Value: fmt.Sprintf("[%s]", strings.Join(aggregationRefs, ", "))},
			{Key: "groupings", Value: fmt.Sprintf("[%s]", strings.Join(groupingRefs, ", "))},
		},
		References: append(groupingIDs, aggregationIDs...),
	}

	b.nodes = append(b.nodes, node)
	return id, nil
}

// processExpr processes an expression and returns its ID
// It handles different expression types by delegating to specific processing methods
func (b *ssaBuilder) processExpr(expr Expr, parent Plan) (int, error) {
	switch expr.Category() {
	case ExprCategoryColumn:
		return b.processColumnExpr(expr.(columnExpr), parent)
	case ExprCategoryLiteral:
		return b.processLiteralExpr(expr.(literalExpr))
	case ExprCategoryBinaryOp:
		return b.processBinaryOpExpr(expr.(binaryOpExpr), parent)
	case ExprCategoryAggregate:
		return b.processAggregateExpr(expr.(aggregateExpr), parent)
	default:
		return 0, fmt.Errorf("unknown expression type: %v", expr.Category())
	}
}

// processColumnExpr processes a column expression
// It creates a ColumnRef node with the column name and type
func (b *ssaBuilder) processColumnExpr(expr columnExpr, parent Plan) (int, error) {
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
func (b *ssaBuilder) processLiteralExpr(expr literalExpr) (int, error) {
	// Create a node for the literal
	id := b.getID()
	node := SSANode{
		ID:       id,
		NodeType: "Literal",
		Tuples: []nodeProperty{
			{Key: "val", Value: expr.Literal()},
			{Key: "type", Value: expr.ValueType().String()},
		},
	}

	b.nodes = append(b.nodes, node)
	return id, nil
}

// processBinaryOpExpr processes a binary operation expression
// It processes the left and right operands, then creates a BinaryOp node
func (b *ssaBuilder) processBinaryOpExpr(expr binaryOpExpr, parent Plan) (int, error) {
	// Process the left and right operands first
	leftID, err := b.processExpr(expr.Left(), parent)
	if err != nil {
		return 0, err
	}

	rightID, err := b.processExpr(expr.Right(), parent)
	if err != nil {
		return 0, err
	}

	// Create a node for the binary operation
	id := b.getID()
	node := SSANode{
		ID:       id,
		NodeType: "BinaryOp",
		Tuples: []nodeProperty{
			{Key: "op", Value: fmt.Sprintf("(%s)", expr.Op())},
			{Key: "name", Value: expr.Name()},
			{Key: "left", Value: fmt.Sprintf("%%%d", leftID)},
			{Key: "right", Value: fmt.Sprintf("%%%d", rightID)},
		},
		References: []int{leftID, rightID},
	}

	b.nodes = append(b.nodes, node)
	return id, nil
}

// processAggregateExpr processes an aggregate expression
// It processes the input expression first if it exists, then creates an AggregationExpr node
func (b *ssaBuilder) processAggregateExpr(expr aggregateExpr, parent Plan) (int, error) {
	// Process the input expression first if it exists
	var exprID int
	var err error
	if expr.Expr() != nil {
		exprID, err = b.processExpr(expr.Expr(), parent)
		if err != nil {
			return 0, err
		}
	}

	// Create a node for the aggregate expression
	id := b.getID()
	node := SSANode{
		ID:       id,
		NodeType: "AggregationExpr",
		Tuples: []nodeProperty{
			{Key: "name", Value: expr.Name()},
			{Key: "op", Value: string(expr.Op())},
		},
	}

	if expr.Expr() != nil {
		node.References = []int{exprID}
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
