package physical

import (
	"fmt"

	"github.com/grafana/loki/v3/pkg/engine/planner/logical"
)

// ParseNode represents a parsing operation in the physical plan.
// It extracts structured fields from log lines using the specified parser.
type ParseNode struct {
	id            string
	Kind          logical.ParserKind
	RequestedKeys []string
}

// ID returns a unique identifier for this ParseNode
func (n *ParseNode) ID() string {
	if n.id != "" {
		return n.id
	}
	return fmt.Sprintf("%p", n)
}

// Type returns the node type
func (n *ParseNode) Type() NodeType {
	return NodeTypeParse
}

// Accept allows the ParseNode to be visited by a Visitor
func (n *ParseNode) Accept(v Visitor) error {
	return v.VisitParse(n)
}

// isNode marks ParseNode as a Node
func (n *ParseNode) isNode() {}