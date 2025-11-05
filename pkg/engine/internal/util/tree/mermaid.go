package tree

import (
	"fmt"
	"io"
	"strings"

	"github.com/google/uuid"
)

type MermaidDiagram struct {
	w io.Writer
}

func NewMermaid(w io.Writer) *MermaidDiagram {
	return &MermaidDiagram{w: w}
}

func (m *MermaidDiagram) Write(node *Node) error {
	if node == nil {
		return nil
	}

	// Write the Mermaid graph header
	if _, err := io.WriteString(m.w, "graph TB\n"); err != nil {
		return err
	}

	// Start traversal from the root
	return m.traverse(node, "", "==>")
}

func (m *MermaidDiagram) traverse(n *Node, parentID string, connector string) error {
	// Use a recursive function to traverse the tree
	if n == nil {
		return nil
	}

	// Create a node ID
	nodeID := uuid.NewString()
	if nodeID == "" {
		nodeID = fmt.Sprintf("%p", n)
	}

	// Write the node definition
	nodeDef := fmt.Sprintf("  %s[\"<b>%s</b> <i>%s</i> <br>%s\"]\n", nodeID, safeString(n.Name), safeString(n.ID), formatProperties(n.Properties))
	if _, err := io.WriteString(m.w, nodeDef); err != nil {
		return err
	}

	for _, comment := range n.Comments {
		if err := m.traverse(comment, nodeID, "-.-"); err != nil {
			return err
		}
	}

	// If there's a parent, create an edge
	if parentID != "" {
		edge := fmt.Sprintf("  %s %s %s\n", parentID, connector, nodeID)
		if _, err := io.WriteString(m.w, edge); err != nil {
			return err
		}
	}

	// Traverse children
	for _, child := range n.Children {
		if err := m.traverse(child, nodeID, connector); err != nil {
			return err
		}
	}

	return nil
}

func formatProperties(properties []Property) string {
	var sb strings.Builder
	for i, prop := range properties {
		_, _ = sb.WriteString(prop.Key)
		_, _ = sb.WriteString("=")
		for ii, val := range prop.Values {
			fmt.Fprintf(&sb, "%s", safeString(fmt.Sprintf("%v", val)))
			if ii < len(prop.Values)-1 {
				_, _ = sb.WriteString(",")
			}
		}
		if i < len(properties)-1 {
			_, _ = sb.WriteString("<br>")
		}
	}
	return sb.String()
}

func safeString(s string) string {
	return strings.ReplaceAll(s, `"`, `&quot;`)
}
