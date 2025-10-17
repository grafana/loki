package workflow

import (
	"fmt"
	"io"
	"strings"

	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/util/dag"
	"github.com/grafana/loki/v3/pkg/engine/internal/util/tree"
)

// Sprint returns a string representation of the workflow.
func Sprint(wf *Workflow) string {
	var sb strings.Builder
	_ = Fprint(&sb, wf)
	return sb.String()
}

// Fprint prints a string representation of the workflow to the given writer.
func Fprint(w io.Writer, wf *Workflow) error {
	visited := make(map[*Task]struct{}, wf.graph.Len())

	roots := wf.graph.Roots()
	for _, root := range roots {
		err := wf.graph.Walk(root, func(n *Task) error {
			if _, seen := visited[n]; seen {
				return nil
			}
			visited[n] = struct{}{}

			fmt.Fprintf(w, "Task %s\n", n.ID())
			fmt.Fprintln(w, "-------------------------------")

			var sb strings.Builder
			for _, root := range n.Fragment.Roots() {
				printer := tree.NewPrinter(&sb)

				planTree := physical.BuildTree(n.Fragment, root)

				for node, streams := range n.Sources {
					treeNode := findTreeNode(planTree, func(n *tree.Node) bool { return n.Context == node })
					if treeNode == nil {
						continue
					}

					for _, stream := range streams {
						treeNode.AddComment("@source", "", []tree.Property{tree.NewProperty("stream", false, stream.ULID.String())})
					}
				}

				for node, streams := range n.Sinks {
					treeNode := findTreeNode(planTree, func(n *tree.Node) bool { return n.Context == node })
					if treeNode == nil {
						continue
					}

					for _, stream := range streams {
						treeNode.AddComment("@sink", "", []tree.Property{tree.NewProperty("stream", false, stream.ULID.String())})
					}
				}

				printer.Print(planTree)
			}

			if _, err := io.Copy(w, strings.NewReader(sb.String())); err != nil {
				return err
			} else if _, err := fmt.Fprintln(w); err != nil {
				return err
			}
			return nil
		}, dag.PreOrderWalk)
		if err != nil {
			return err
		}
	}

	return nil
}

// findTreeNode finds the first node in the tree that satisfies the given
// predicate. findTreeNode returns nil if no node is found.
func findTreeNode(root *tree.Node, f func(node *tree.Node) bool) *tree.Node {
	if f(root) {
		return root
	}

	for _, child := range root.Children {
		if node := findTreeNode(child, f); node != nil {
			return node
		}
	}

	for _, comment := range root.Comments {
		if node := findTreeNode(comment, f); node != nil {
			return node
		}
	}

	return nil
}
