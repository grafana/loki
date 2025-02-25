package logical

import (
	"fmt"
	"strings"
)

// SSANode represents a single node in the SSA form
type SSANode struct {
	// ID is the unique identifier for this node in the SSA form
	ID string
	// NodeType specifies the type of operation this node represents
	NodeType nodeType
	// Refs contains references to other SSA nodes that this node depends on
	Refs []string
	// Original contains a reference to the original AST node
	Original ast
}

// SSAForm represents a full query plan in SSA form
type SSAForm struct {
	// Nodes is an ordered list of SSA nodes, where each node's dependencies
	// are guaranteed to appear earlier in the list
	Nodes []SSANode
	// RootRef is the reference to the root node of the query plan
	RootRef string
}

// Find all nodes in the AST using post-order traversal
// This ensures that all dependencies appear before their dependents
func postOrder(node ast) []ast {
	var result []ast

	// Process children first
	for _, child := range node.ASTChildren() {
		result = append(result, postOrder(child)...)
	}

	// Then add this node
	result = append(result, node)
	return result
}

// ConvertToSSA converts a Plan to SSA form using post-order traversal
// that ensures all dependencies of a node appear earlier in the resulting list
func ConvertToSSA(plan Plan) (*SSAForm, error) {
	// First convert Plan to AST
	root, ok := plan.(ast)
	if !ok {
		return nil, fmt.Errorf("plan of type %T does not implement ast interface", plan)
	}

	var nodes []SSANode
	idMap := make(map[ast]string)

	// Get all nodes in post-order
	orderedNodes := postOrder(root)

	// Create SSA nodes for each AST node
	for i, node := range orderedNodes {
		id := fmt.Sprintf("%%%d", i+1)
		idMap[node] = id

		// Create a new SSA node
		ssaNode := SSANode{
			ID:       id,
			NodeType: node.Type(),
			Original: node,
		}

		nodes = append(nodes, ssaNode)
	}

	// Add references to dependencies
	for i, node := range nodes {
		var refs []string
		for _, child := range node.Original.ASTChildren() {
			refs = append(refs, idMap[child])
		}
		nodes[i].Refs = refs
	}

	return &SSAForm{
		Nodes:   nodes,
		RootRef: idMap[root],
	}, nil
}

// String returns a string representation of the SSA form
func (s *SSAForm) String() string {
	var sb strings.Builder

	for _, node := range s.Nodes {
		// Format the node line
		sb.WriteString(fmt.Sprintf("%s = %s", node.ID, NodeTypeName(node.NodeType)))

		// Add references
		if len(node.Refs) > 0 {
			sb.WriteString(fmt.Sprintf(" %s", strings.Join(node.Refs, ", ")))
		}

		sb.WriteString("\n")
	}

	// Add the return statement
	sb.WriteString(fmt.Sprintf("RETURN %s\n", s.RootRef))

	return sb.String()
}

// ReconstructPlan converts an SSA form back to a Plan
// This is a placeholder for future implementation
func (s *SSAForm) ReconstructPlan() (Plan, error) {
	// To be implemented
	return nil, fmt.Errorf("not implemented yet")
}
