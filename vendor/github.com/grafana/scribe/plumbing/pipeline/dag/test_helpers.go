package dag

import (
	"fmt"
	"testing"
)

func EdgesToMap[T any](e map[int64][]Edge[T]) map[int64][]int64 {
	m := map[int64][]int64{}
	for k, v := range e {
		ids := make([]int64, len(v))
		for i, node := range v {
			ids[i] = node.To.ID
		}

		m[k] = ids
	}

	return m
}

func NodeIDs[T any](nodes []Node[T]) []int64 {
	ids := make([]int64, len(nodes))
	for i, v := range nodes {
		ids[i] = v.ID
	}

	return ids
}

func ensureGraphEdges[T any](expected map[int64][]int64, graphEdges map[int64][]Edge[T]) error {
	if len(expected) != len(graphEdges) {
		return fmt.Errorf("Unexpected number of graph edges. Expected '%d' but received '%d'", len(expected), len(graphEdges))
	}

	for id, expect := range expected {
		edges := graphEdges[id]
		if _, ok := expected[id]; !ok {
			return fmt.Errorf("Found unexpected node ID '%d' in graph edges", id)
		}

		// Calculate the IDs since this is a []dag.Node and not []int64
		ids := make([]int64, len(edges))
		for i, edge := range edges {
			ids[i] = edge.To.ID
		}
		if len(expect) != len(ids) {
			return fmt.Errorf("Unequal number of edges for node '%d'. Expected '%d' edges (%+v), received '%d' edges (%+v)", id, len(expect), expect, len(ids), ids)
		}
		for i, id := range ids {
			if expect[i] != id {
				return fmt.Errorf("Expected node '%d' to have edges '%+v' but has '%+v'", id, expect, ids)
			}
		}
	}
	return nil
}

// EnsureGraphEdges is a test helper function that is used inside the dag tests and outside in other implementations
// to easily ensure that we are using the dag properly.
func EnsureGraphEdges[T any](t *testing.T, expected map[int64][]int64, graphEdges map[int64][]Edge[T]) {
	t.Helper()

	if err := ensureGraphEdges(expected, graphEdges); err != nil {
		t.Fatalf("Unexpected graph edges received.\nError: %s\nEdges: %+v\nExpected: %+v", err.Error(), EdgesToMap(graphEdges), expected)
	}
}

func ensureGraphNodes(expected []int64, nodes []int64) error {
	if len(expected) != len(nodes) {
		return fmt.Errorf("Unexpected number of graph nodes. Expected '%d' but received '%d'", len(expected), len(nodes))
	}

	for i, expect := range expected {
		if nodes[i] != expect {
			return fmt.Errorf("Found unexpected node ID '%d' in graph edges", nodes[i])
		}
	}
	return nil
}

// EnsureGraphNodes is a test helper function that is used inside the dag tests and outside in other implementations
// to easily ensure that we are using the dag properly.
func EnsureGraphNodes[T any](t *testing.T, expected []int64, nodes []Node[T]) {
	t.Helper()
	nodeIDs := make([]int64, len(nodes))
	for i, v := range nodes {
		nodeIDs[i] = v.ID
	}

	if err := ensureGraphNodes(expected, nodeIDs); err != nil {
		t.Fatalf("Unexpected graph nodes received.\nError: %s\nNodes: %+v\nExpected: %+v", err.Error(), nodeIDs, expected)
	}
}
