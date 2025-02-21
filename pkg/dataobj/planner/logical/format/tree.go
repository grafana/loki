package format

import (
	"strings"
)

const (
	treeNode     = "├── "
	treeLastNode = "└── "
	treeContinue = "│   "
	treeSpace    = "    "
)

// TreeFormatter implements Formatter to produce tree-style output
type TreeFormatter struct {
	node     Node
	children []*TreeFormatter
	parent   *TreeFormatter
	isRoot   bool
}

// NewTreeFormatter creates a new root TreeFormatter
func NewTreeFormatter() *TreeFormatter {
	return &TreeFormatter{isRoot: true}
}

// WriteNode implements Formatter
func (t *TreeFormatter) WriteNode(node Node) Formatter {
	child := &TreeFormatter{
		node:   node,
		parent: t,
	}
	t.children = append(t.children, child)
	return child
}

// Format implements Formatter
func (t *TreeFormatter) Format() string {
	var sb strings.Builder
	t.format(&sb, "")
	return sb.String()
}

func (t *TreeFormatter) format(sb *strings.Builder, indent string) {
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

		nextIndent := indent + treeContinue
		if i == len(t.children)-1 {
			sb.WriteString(treeLastNode)
			nextIndent = indent + treeSpace
		} else {
			sb.WriteString(treeNode)
		}

		child.format(sb, nextIndent)
	}
}
