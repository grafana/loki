package tree

import (
	"fmt"
	"io"
)

type Attribute struct {
	Key    string
	Values []any
	Multi  bool
}

func NewAttribute(key string, multi bool, values ...any) Attribute {
	return Attribute{
		Key:    key,
		Values: values,
		Multi:  multi,
	}
}

// Node represents an element in a tree structure
type Node struct {
	ID         string
	Type       string
	Attributes []Attribute
	Children   []*Node
}

func NewNode(name, ident string, attr ...Attribute) *Node {
	return &Node{
		ID:         ident,
		Type:       name,
		Attributes: attr,
	}
}

// AddChild adds a child node to the parent node
func (n *Node) AddChild(name, ident string, attr []Attribute) *Node {
	child := NewNode(name, ident, attr...)
	n.Children = append(n.Children, child)
	return child
}

// Printer is responsible for printing tree structures
type Printer struct {
	w io.StringWriter
}

// NewPrinter creates a new Printer instance that writes to the specified builder
func NewPrinter(w io.StringWriter) *Printer {
	return &Printer{w: w}
}

// Print prints the entire tree structure starting from the given root node
func (tp *Printer) Print(root *Node) {
	tp.printNode(root)
	tp.printChildren(root.Children, "")
}

func (tp *Printer) printNode(node *Node) {
	tp.w.WriteString(node.Type)

	if node.ID != "" {
		tp.w.WriteString(" #")
		tp.w.WriteString(node.ID)
	}

	if len(node.Attributes) == 0 {
		tp.w.WriteString("\n")
		return
	}

	tp.w.WriteString(" ")
	for i, attr := range node.Attributes {
		tp.w.WriteString(attr.Key)
		tp.w.WriteString("=")
		if attr.Multi {
			tp.w.WriteString("(")
		}
		for ii, val := range attr.Values {
			tp.w.WriteString(fmt.Sprintf("%v", val))
			if ii < len(attr.Values)-1 {
				tp.w.WriteString(", ")
			}
		}
		if attr.Multi {
			tp.w.WriteString(")")
		}
		if i < len(node.Attributes)-1 {
			tp.w.WriteString(" ")
		}
	}
	tp.w.WriteString("\n")
}

// printChildren recursively prints all children with appropriate indentation
func (tp *Printer) printChildren(nodes []*Node, prefix string) {
	for i, node := range nodes {
		isLast := i == len(nodes)-1

		// Choose connector symbols based on whether this is the last item
		connector := "├── "
		newPrefix := prefix + "│   "
		if isLast {
			connector = "└── "
			newPrefix = prefix + "    "
		}

		// Print this node
		tp.w.WriteString(prefix)
		tp.w.WriteString(connector)
		tp.printNode(node)

		// Recursively print children
		tp.printChildren(node.Children, newPrefix)
	}
}
