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
		tp.w.WriteString(" <")
		tp.w.WriteString(node.ID)
		tp.w.WriteString(">")
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

// printChildren recursively prints all children with appropriate indentation.
func (tp *Printer) printChildren(comments, children []*Node, prefix string) {
	hasChildren := len(children) > 0

	// Iterate over sub nodes first.
	// They have extended indentation compared to regular child nodes
	// and depending if there are child nodes, also have a | as prefix.
	for i, node := range comments {
		isLast := i == len(comments)-1

		// Choose indentation symbols based on whether the node we're printing has
		// any children to print.
		indent := symIndent
		if !hasChildren {
			indent = symPrefix
		}

		// Choose connector symbols based on whether this is the last item
		connector := symPrefix + symConn
		newPrefix := prefix + indent + symIndent
		if hasChildren {
			connector = symIndent + symConn
		}

		if isLast {
			connector = symPrefix + symLastConn
			newPrefix = prefix + indent + symPrefix
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
