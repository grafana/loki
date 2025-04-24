package topk_test

import (
	"fmt"
	"slices"
	"sort"
	"testing"

	"github.com/grafana/loki/v3/pkg/util/topk"
	"github.com/stretchr/testify/require"
)

// ExampleHeap_greatest shows how to use a [topk.Heap] to get the top-k greatest
// elements in descending order.
func ExampleHeap_greatest() {
	heap := &topk.Heap[int]{
		Limit: 3,
		Less:  func(a, b int) bool { return a < b },
	}

	for i := range 10 {
		heap.Push(i)
	}

	actual := heap.PopAll()
	slices.Reverse(actual) // Reverse to get in greatest-descending order.

	fmt.Println(actual)
	// Output: [9 8 7]
}

// ExampleHeap_least shows how to use a [topk.Heap] to get the top-k least
// elements in ascending order.
func ExampleHeap_least() {
	heap := &topk.Heap[int]{
		Limit: 3,
		Less:  func(a, b int) bool { return a > b },
	}

	for i := range 10 {
		heap.Push(i)
	}

	actual := heap.PopAll()
	slices.Reverse(actual) // Reverse to get in least-ascending order.

	fmt.Println(actual)
	// Output: [0 1 2]
}

func TestHeap_Range(t *testing.T) {
	heap := &topk.Heap[int]{
		Limit: 3,
		Less:  func(a, b int) bool { return a < b },
	}

	for i := range 10 {
		heap.Push(i)
	}

	var actual []int
	for v := range heap.Range() {
		actual = append(actual, v)
	}
	sort.Ints(actual)

	expected := []int{7, 8, 9}
	require.Equal(t, expected, actual)
}

func TestHeap_Range_Empty(t *testing.T) {
	heap := &topk.Heap[int]{
		Limit: 3,
		Less:  func(a, b int) bool { return a < b },
	}

	require.NotPanics(t, func() {
		// Iterating over an empty heap should be a no-op.
		for range heap.Range() {
			t.Fatal("there should not be any values in the empty heap")
		}
	})
}
