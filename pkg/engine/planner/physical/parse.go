package physical

import (
	"fmt"
)

// ParseNode represents a parsing operation in the physical plan.
// It extracts structured fields from log lines using the specified parser.
type ParseNode struct {
	id            string
	Kind          ParserKind
	RequestedKeys []string
}

// ParserKind represents the type of parser to use
type ParserKind int

const (
	ParserInvalid ParserKind = iota
	ParserLogfmt
	ParserJSON
)

func (p ParserKind) String() string {
	switch p {
	case ParserLogfmt:
		return "logfmt"
	case ParserJSON:
		return "json"
	default:
		return "invalid"
	}
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
