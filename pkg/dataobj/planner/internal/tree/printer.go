package tree

import (
	"fmt"
	"io"
)

const (
	symPrefix   = "    "
	symIndent   = "│   "
	symConn     = "├── "
	symLastConn = "└── "
)

// Property represents a property of a [Node]. It is a key-value-pair, where
// the value is either a single value or a list of values.
// When the value is a multi-value, the field IsMultiValue needs to be set to
// `true`.
// A single-value property is represented as `key=value` and a multi-value
// property as `key=(value1, value2, ...)`.
type Property struct {
	// Key is the name of the property.
	Key string
	// Values holds the value(s) of the property.
	Values []any
	// IsMultiValue marks whether the property is a multi-value property.
	IsMultiValue bool
}

// NewProperty creates a new Property with the specified key, multi-value flag, and values.
// The multi parameter determines if the property should be treated as a multi-value property.
func NewProperty(key string, multi bool, values ...any) Property {
	return Property{
		Key:          key,
		Values:       values,
		IsMultiValue: multi,
	}
}

// Node represents a node in a tree structure that can be traversed and printed
// by the [Printer].
// It allows for building hierarchical representations of data where each node
// can have multiple properties and multiple children.
type Node struct {
	// ID is a unique identifier for the node.
	ID string
	// Name is the display name of the node.
	Name string
	// Properties contains a list of key-value properties associated with the node.
	Properties []Property
	// Children are child nodes of the node.
	Children []*Node
	// Comments, like Children, are child nodes of the node, with the difference
	// that comments are indented a level deeper than children. A common use-case
	// for comments are tree-style properies of a node, such as expressions of a
	// physical plan node.
	Comments []*Node
}

// NewNode creates a new node with the given name, unique identifier and
// properties.
func NewNode(name, id string, properties ...Property) *Node {
	return &Node{
		ID:         id,
		Name:       name,
		Properties: properties,
	}
}

// AddChild creates a new node with the given name, unique identifier, and properties
// and adds it to the parent node.
func (n *Node) AddChild(name, id string, properties []Property) *Node {
	child := NewNode(name, id, properties...)
	n.Children = append(n.Children, child)
	return child
}

func (n *Node) AddComment(name, id string, properties []Property) *Node {
	node := NewNode(name, id, properties...)
	n.Comments = append(n.Comments, node)
	return node
}

// Printer is used for writing the hierarchical representation of a tree
// of [Node]s.
type Printer struct {
	w io.StringWriter
}

// NewPrinter creates a new [Printer] instance that writes to the specified
// [io.StringWriter].
func NewPrinter(w io.StringWriter) *Printer {
	return &Printer{w: w}
}

// Print writes the entire tree structure starting from the given root node to
// the printer's [io.StringWriter].
// Example output:
//
//	SortMerge #sort order=ASC column=timestamp
//	├── Limit #limit1 limit=1000
//	│   └── DataObjScan #scan1 location=dataobj_1
//	└── Limit #limit2 limit=1000
//	    └── DataObjScan #scan2 location=dataobj_2
func (tp *Printer) Print(root *Node) {
	tp.printNode(root)
	tp.printChildren(root.Comments, root.Children, "")
}

func (tp *Printer) printNode(node *Node) {
	tp.w.WriteString(node.Name)

	if node.ID != "" {
		tp.w.WriteString(" #")
		tp.w.WriteString(node.ID)
	}

	if len(node.Properties) == 0 {
		tp.w.WriteString("\n")
		return
	}

	tp.w.WriteString(" ")
	for i, attr := range node.Properties {
		tp.w.WriteString(attr.Key)
		tp.w.WriteString("=")

		if attr.IsMultiValue {
			tp.w.WriteString("(")
		}
		for ii, val := range attr.Values {
			tp.w.WriteString(fmt.Sprintf("%v", val))
			if ii < len(attr.Values)-1 {
				tp.w.WriteString(", ")
			}
		}

		if attr.IsMultiValue {
			tp.w.WriteString(")")
		}
		if i < len(node.Properties)-1 {
			tp.w.WriteString(" ")
		}
	}
	tp.w.WriteString("\n")
}

// printChildren recursively prints all children with appropriate indentation
func (tp *Printer) printChildren(comments, children []*Node, prefix string) {
	hasChildren := len(children) > 0

	// Iterate over sub nodes first.
	// They have extended indentation compared to regular child nodes
	// and depending if there are child nodes, also have a | as prefix.
	for i, node := range comments {
		isLast := i == len(comments)-1

		// Choose connector symbols based on whether this is the last item
		connector := symPrefix + symConn
		newPrefix := prefix + symIndent + symIndent
		if hasChildren {
			connector = symIndent + symConn
		}

		if isLast {
			connector = symPrefix + symLastConn
			newPrefix = prefix + symIndent + symPrefix
			if hasChildren {
				connector = symIndent + symLastConn
			}
		}

		// Print this node
		tp.w.WriteString(prefix)
		tp.w.WriteString(connector)
		tp.printNode(node)

		// Recursively print children
		tp.printChildren(node.Comments, node.Children, newPrefix)
	}

	// Iterate over child nodes last.
	for i, node := range children {
		isLast := i == len(children)-1

		// Choose connector symbols based on whether this is the last item
		connector := symConn
		newPrefix := prefix + symIndent
		if isLast {
			connector = symLastConn
			newPrefix = prefix + symPrefix
		}

		// Print this node
		tp.w.WriteString(prefix)
		tp.w.WriteString(connector)
		tp.printNode(node)

		// Recursively print children
		tp.printChildren(node.Comments, node.Children, newPrefix)
	}
}
