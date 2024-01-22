package interval

// Delete removes the given start and end interval key and its associated value from the tree.
// It does nothing if the given start and end interval key doesn't exist in the tree.
//
// Delete returns an InvalidIntervalError if the given end is less than or equal to the given start value.
func (st *SearchTree[V, T]) Delete(start, end T) error {
	st.mu.Lock()
	defer st.mu.Unlock()

	if st.root == nil {
		return nil
	}

	intervl := interval[V, T]{
		start:      start,
		end:        end,
		allowPoint: st.config.allowIntervalPoint,
	}

	if intervl.isInvalid(st.cmp) {
		return newInvalidIntervalError(intervl)
	}

	st.root = delete(st.root, intervl, st.cmp)
	if st.root != nil {
		st.root.color = black
	}

	return nil
}

func delete[V, T any](n *node[V, T], intervl interval[V, T], cmp CmpFunc[T]) *node[V, T] {
	if n == nil {
		return nil
	}

	if intervl.less(n.interval.start, n.interval.end, cmp) {
		if n.left != nil && !isRed(n.left) && !isRed(n.left.left) {
			n = moveRedLeft(n, cmp)
		}
		n.left = delete(n.left, intervl, cmp)
	} else {
		if isRed(n.left) {
			n = rotateRight(n, cmp)
		}
		if n.interval.equal(intervl.start, intervl.end, cmp) && n.right == nil {
			return nil
		}
		if n.right != nil && !isRed(n.right) && !isRed(n.right.left) {
			n = moveRedRight(n, cmp)
		}
		if n.interval.equal(intervl.start, intervl.end, cmp) {
			minNode := min(n.right)
			n.interval = minNode.interval
			n.right = deleteMin(n.right, cmp)
		} else {
			n.right = delete(n.right, intervl, cmp)
		}
	}

	updateSize(n)

	return fixUp(n, cmp)
}

func deleteMin[V, T any](n *node[V, T], cmp CmpFunc[T]) *node[V, T] {
	if n.left == nil {
		return nil
	}

	if !isRed(n.left) && !isRed(n.left.left) {
		n = moveRedLeft(n, cmp)
	}

	n.left = deleteMin(n.left, cmp)

	updateSize(n)

	return fixUp(n, cmp)
}

// DeleteMin removes the smallest interval key and its associated value from the tree.
func (st *SearchTree[V, T]) DeleteMin() {
	st.mu.Lock()
	defer st.mu.Unlock()

	if st.root == nil {
		return
	}

	st.root = deleteMin(st.root, st.cmp)
	if st.root != nil {
		st.root.color = black
	}
}

// DeleteMax removes the largest interval key and its associated value from the tree.
func (st *SearchTree[V, T]) DeleteMax() {
	st.mu.Lock()
	defer st.mu.Unlock()

	if st.root == nil {
		return
	}

	st.root = deleteMax(st.root, st.cmp)
	if st.root != nil {
		st.root.color = black
	}
}

func deleteMax[V, T any](n *node[V, T], cmp CmpFunc[T]) *node[V, T] {
	if isRed(n.left) {
		n = rotateRight(n, cmp)
	}

	if n.right == nil {
		return nil
	}

	if !isRed(n.right) && !isRed(n.right.left) {
		n = moveRedRight(n, cmp)
	}

	n.right = deleteMax(n.right, cmp)

	updateSize(n)

	return fixUp(n, cmp)
}

// Delete removes the given start and end interval key and its associated values from the tree.
// It does nothing if the given start and end interval key doesn't exist in the tree.
//
// Delete returns an InvalidIntervalError if the given end is less than or equal to the given start value.
func (st *MultiValueSearchTree[V, T]) Delete(start, end T) error {
	st.mu.Lock()
	defer st.mu.Unlock()

	if st.root == nil {
		return nil
	}

	intervl := interval[V, T]{
		start:      start,
		end:        end,
		allowPoint: st.config.allowIntervalPoint,
	}

	if intervl.isInvalid(st.cmp) {
		return newInvalidIntervalError(intervl)
	}

	st.root = delete(st.root, intervl, st.cmp)
	if st.root != nil {
		st.root.color = black
	}

	return nil
}

// DeleteMin removes the smallest interval key and its associated values from the tree.
func (st *MultiValueSearchTree[V, T]) DeleteMin() {
	st.mu.Lock()
	defer st.mu.Unlock()

	if st.root == nil {
		return
	}

	st.root = deleteMin(st.root, st.cmp)
	if st.root != nil {
		st.root.color = black
	}
}

// DeleteMax removes the largest interval key and its associated values from the tree.
func (st *MultiValueSearchTree[V, T]) DeleteMax() {
	st.mu.Lock()
	defer st.mu.Unlock()

	if st.root == nil {
		return
	}

	st.root = deleteMax(st.root, st.cmp)
	if st.root != nil {
		st.root.color = black
	}
}
