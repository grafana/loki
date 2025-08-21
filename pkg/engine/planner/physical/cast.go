package physical

import "fmt"

// CastType represents the target type for casting
type CastType int

const (
	CastTypeInt64 CastType = iota
	CastTypeFloat64
)

// Cast represents a column to cast and its target type
type Cast struct {
	Column string
	Type   CastType
}

// CastNode represents a physical node that casts columns to different types
type CastNode struct {
	id    string
	Casts []Cast
}

// ID returns a unique identifier for this CastNode
func (n *CastNode) ID() string {
	if n.id != "" {
		return n.id
	}
	return fmt.Sprintf("%p", n)
}

// Type returns the node type
func (n *CastNode) Type() NodeType {
	return NodeTypeCast
}

// Accept implements the Node interface
func (n *CastNode) Accept(v Visitor) error {
	return v.VisitCast(n)
}

// isNode marks CastNode as a Node
func (n *CastNode) isNode() {}

// String returns a string representation of the cast node
func (n *CastNode) String() string {
	var casts []string
	for _, c := range n.Casts {
		var typeStr string
		switch c.Type {
		case CastTypeInt64:
			typeStr = "int64"
		case CastTypeFloat64:
			typeStr = "float64"
		default:
			typeStr = "unknown"
		}
		casts = append(casts, fmt.Sprintf("%s->%s", c.Column, typeStr))
	}
	return fmt.Sprintf("CastNode{casts:%v}", casts)
}
