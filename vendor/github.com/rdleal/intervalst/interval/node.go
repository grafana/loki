package interval

import "math"

type color bool

const (
	red   color = true
	black color = false
)

type node[V, T any] struct {
	interval interval[V, T]
	maxEnd   T
	right    *node[V, T]
	left     *node[V, T]
	color    color
	size     int
}

func newNode[V, T any](intervl interval[V, T], c color) *node[V, T] {
	return &node[V, T]{
		interval: intervl,
		maxEnd:   intervl.end,
		color:    c,
		size:     1,
	}
}

func flipColors[T, V any](n *node[V, T]) {
	n.color = !n.color
	if n.left != nil {
		n.left.color = !n.left.color
	}
	if n.right != nil {
		n.right.color = !n.right.color
	}
}

func isRed[V, T any](n *node[V, T]) bool {
	if n == nil {
		return false
	}
	return n.color == red
}

func min[V, T any](n *node[V, T]) *node[V, T] {
	for n != nil && n.left != nil {
		n = n.left
	}
	return n
}

func max[V, T any](n *node[V, T]) *node[V, T] {
	for n != nil && n.right != nil {
		n = n.right
	}
	return n
}

func updateSize[V, T any](n *node[V, T]) {
	n.size = 1 + size(n.left) + size(n.right)
}

func height[V, T any](n *node[V, T]) float64 {
	if n == nil {
		return 0
	}

	return 1 + math.Max(height(n.left), height(n.right))
}

func size[V, T any](n *node[V, T]) int {
	if n == nil {
		return 0
	}
	return n.size
}

func updateMaxEnd[V, T any](n *node[V, T], cmp CmpFunc[T]) {
	n.maxEnd = n.interval.end
	if n.left != nil && cmp.gt(n.left.maxEnd, n.maxEnd) {
		n.maxEnd = n.left.maxEnd
	}

	if n.right != nil && cmp.gt(n.right.maxEnd, n.maxEnd) {
		n.maxEnd = n.right.maxEnd
	}
}

func rotateLeft[V, T any](n *node[V, T], cmp CmpFunc[T]) *node[V, T] {
	x := n.right
	n.right = x.left
	x.left = n
	x.color = n.color
	x.maxEnd = n.maxEnd
	n.color = red
	x.size = n.size

	updateSize(n)
	updateMaxEnd(n, cmp)
	return x
}

func rotateRight[V, T any](n *node[V, T], cmp CmpFunc[T]) *node[V, T] {
	x := n.left
	n.left = x.right
	x.right = n
	x.color = n.color
	x.maxEnd = n.maxEnd
	n.color = red
	x.size = n.size

	updateSize(n)
	updateMaxEnd(n, cmp)
	return x
}

func balanceNode[V, T any](n *node[V, T], cmp CmpFunc[T]) *node[V, T] {
	if isRed(n.right) && !isRed(n.left) {
		n = rotateLeft(n, cmp)
	}

	if isRed(n.left) && isRed(n.left.left) {
		n = rotateRight(n, cmp)
	}

	if isRed(n.left) && isRed(n.right) {
		flipColors(n)
	}

	return n
}

func moveRedLeft[V, T any](n *node[V, T], cmp CmpFunc[T]) *node[V, T] {
	flipColors(n)
	if n.right != nil && isRed(n.right.left) {
		n.right = rotateRight(n.right, cmp)
		n = rotateLeft(n, cmp)
		flipColors(n)
	}
	return n
}

func moveRedRight[V, T any](n *node[V, T], cmp CmpFunc[T]) *node[V, T] {
	flipColors(n)
	if n.left != nil && isRed(n.left.left) {
		n = rotateRight(n, cmp)
		flipColors(n)
	}
	return n
}

func fixUp[V, T any](n *node[V, T], cmp CmpFunc[T]) *node[V, T] {
	updateMaxEnd(n, cmp)

	return balanceNode(n, cmp)
}
