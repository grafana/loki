package interval

// Find returns the value which interval key exactly matches with the given start and end interval.
// It returns true as the second return value if an exaclty matching interval key is found in the tree;
// otherwise, false.
func (st *SearchTree[V, T]) Find(start, end T) (V, bool) {
	st.mu.RLock()
	defer st.mu.RUnlock()

	var val V

	interval, ok := find(st.root, start, end, st.cmp)
	if !ok {
		return val, false
	}

	return interval.val, true
}

func find[V, T any](root *node[V, T], start, end T, cmp CmpFunc[T]) (interval[V, T], bool) {
	if root == nil {
		return interval[V, T]{}, false
	}

	cur := root
	for cur != nil {
		switch {
		case cur.interval.equal(start, end, cmp):
			return cur.interval, true
		case cur.interval.less(start, end, cmp):
			cur = cur.right
		default:
			cur = cur.left
		}
	}

	return interval[V, T]{}, false
}

// AnyIntersection returns a value which interval key intersects with the given start and end interval.
// It returns true as the second return value if any intersection is found in the tree; otherwise, false.
func (st *SearchTree[V, T]) AnyIntersection(start, end T) (V, bool) {
	st.mu.RLock()
	defer st.mu.RUnlock()

	var val V

	interval, ok := anyIntersections(st.root, start, end, st.cmp)
	if !ok {
		return val, false
	}

	return interval.val, true
}

func anyIntersections[V, T any](root *node[V, T], start, end T, cmp CmpFunc[T]) (interval[V, T], bool) {
	if root == nil {
		return interval[V, T]{}, false
	}

	cur := root
	for cur != nil {
		if cur.interval.intersects(start, end, cmp) {
			return cur.interval, true
		}

		next := cur.left
		if cur.left == nil || cmp.gt(start, cur.left.maxEnd) {
			next = cur.right
		}

		cur = next
	}

	return interval[V, T]{}, false
}

// AllIntersections returns a slice of values which interval key intersects with the given start and end interval.
// It returns true as the second return value if any intersection is found in the tree; otherwise, false.
func (st *SearchTree[V, T]) AllIntersections(start, end T) ([]V, bool) {
	st.mu.RLock()
	defer st.mu.RUnlock()

	var vals []V
	if st.root == nil {
		return vals, false
	}

	searchInOrder(st.root, start, end, st.cmp, func(it interval[V, T]) {
		vals = append(vals, it.val)
	})

	return vals, len(vals) > 0
}

func searchInOrder[V, T any](n *node[V, T], start, end T, cmp CmpFunc[T], foundFn func(interval[V, T])) {
	if n.left != nil && cmp.gte(n.left.maxEnd, start) {
		searchInOrder(n.left, start, end, cmp, foundFn)
	}

	if n.interval.intersects(start, end, cmp) {
		foundFn(n.interval)
	}

	if n.right != nil && cmp.gte(n.right.maxEnd, start) {
		searchInOrder(n.right, start, end, cmp, foundFn)
	}
}

// Min returns the value which interval key is the minimum interval key in the tree.
// It returns false as the second return value if the tree is empty; otherwise, true.
func (st *SearchTree[V, T]) Min() (V, bool) {
	st.mu.RLock()
	defer st.mu.RUnlock()

	var val V
	if st.root == nil {
		return val, false
	}

	val = min(st.root).interval.val

	return val, true
}

// Max returns the value which interval key is the maximum interval in the tree.
// It returns false as the second return value if the tree is empty; otherwise, true.
func (st *SearchTree[V, T]) Max() (V, bool) {
	st.mu.RLock()
	defer st.mu.RUnlock()

	var val V
	if st.root == nil {
		return val, false
	}

	val = max(st.root).interval.val

	return val, true
}

// Ceil returns a value which interval key is the smallest interval key greater than the given start and end interval.
// It returns true as the second return value if there's a ceiling interval key for the given start and end interval
// in the tree; otherwise, false.
func (st *SearchTree[V, T]) Ceil(start, end T) (V, bool) {
	st.mu.RLock()
	defer st.mu.RUnlock()

	var val V
	interval, ok := ceil(st.root, start, end, st.cmp)
	if !ok {
		return val, false
	}

	return interval.val, true
}

func ceil[V, T any](root *node[V, T], start, end T, cmp CmpFunc[T]) (interval[V, T], bool) {
	if root == nil {
		return interval[V, T]{}, false
	}

	var ceil *node[V, T]

	cur := root
	for cur != nil {
		if cur.interval.equal(start, end, cmp) {
			return cur.interval, true
		}

		if cur.interval.less(start, end, cmp) {
			cur = cur.right
		} else {
			ceil = cur
			cur = cur.left
		}
	}

	if ceil == nil {
		return interval[V, T]{}, false
	}

	return ceil.interval, true
}

// Floor returns a value which interval key is the greatest interval key lesser than the given start and end interval.
// It returns true as the second return value if there's a floor interval key for the given start and end interval
// in the tree; otherwise, false.
func (st *SearchTree[V, T]) Floor(start, end T) (V, bool) {
	st.mu.RLock()
	defer st.mu.RUnlock()

	var val V
	interval, ok := floor(st.root, start, end, st.cmp)
	if !ok {
		return val, false
	}

	return interval.val, true
}

func floor[V, T any](root *node[V, T], start, end T, cmp CmpFunc[T]) (interval[V, T], bool) {
	if root == nil {
		return interval[V, T]{}, false
	}

	var floor *node[V, T]

	cur := root
	for cur != nil {
		if cur.interval.equal(start, end, cmp) {
			return cur.interval, true
		}

		if cur.interval.less(start, end, cmp) {
			floor = cur
			cur = cur.right
		} else {
			cur = cur.left
		}
	}

	if floor == nil {
		return interval[V, T]{}, false
	}

	return floor.interval, true
}

// Rank returns the number of intervals strictly less than the given start and end interval.
func (st *SearchTree[V, T]) Rank(start, end T) int {
	st.mu.RLock()
	defer st.mu.RUnlock()

	return rank(st.root, start, end, st.cmp)
}

func rank[V, T any](root *node[V, T], start, end T, cmp CmpFunc[T]) int {
	var rank int
	cur := root

	for cur != nil {
		if cur.interval.equal(start, end, cmp) {
			rank += size(cur.left)
			break
		} else if cur.interval.less(start, end, cmp) {
			rank += 1 + size(cur.left)
			cur = cur.right
		} else {
			cur = cur.left
		}
	}

	return rank
}

// Select returns the value which interval key is the kth smallest interval key in the tree.
// It returns false if k is not between 0 and N-1, where N is the number of interval keys
// in the tree; otherwise, true.
func (st *SearchTree[V, T]) Select(k int) (V, bool) {
	st.mu.RLock()
	defer st.mu.RUnlock()

	var val V

	interval, ok := selectInterval(st.root, k)
	if !ok {
		return val, false
	}

	return interval.val, true
}

func selectInterval[V, T any](root *node[V, T], k int) (interval[V, T], bool) {
	cur := root
	for cur != nil {
		t := size(cur.left)
		switch {
		case t > k:
			cur = cur.left
		case t < k:
			cur = cur.right
			k = k - t - 1
		default:
			return cur.interval, true
		}
	}

	return interval[V, T]{}, false
}

// Find returns the values which interval key exactly matches with the given start and end interval.
// It returns true as the second return value if an exaclty matching interval key is found in the tree;
// otherwise, false.
func (st *MultiValueSearchTree[V, T]) Find(start, end T) ([]V, bool) {
	st.mu.RLock()
	defer st.mu.RUnlock()

	var vals []V

	interval, ok := find(st.root, start, end, st.cmp)
	if !ok {
		return vals, false
	}

	return interval.vals, true
}

// AnyIntersection returns values which interval key intersects with the given start and end interval.
// It returns true as the second return value if any intersection is found in the tree; otherwise, false.
func (st *MultiValueSearchTree[V, T]) AnyIntersection(start, end T) ([]V, bool) {
	st.mu.RLock()
	defer st.mu.RUnlock()

	interval, ok := anyIntersections(st.root, start, end, st.cmp)
	if !ok {
		return nil, false
	}

	return interval.vals, true
}

// AllIntersections returns a slice of values which interval key intersects with the given start and end interval.
// It returns true as the second return value if any intersection is found in the tree; otherwise, false.
func (st *MultiValueSearchTree[V, T]) AllIntersections(start, end T) ([]V, bool) {
	st.mu.RLock()
	defer st.mu.RUnlock()

	var vals []V
	if st.root == nil {
		return vals, false
	}

	searchInOrder(st.root, start, end, st.cmp, func(it interval[V, T]) {
		vals = append(vals, it.vals...)
	})

	return vals, len(vals) > 0
}

// Min returns the values which interval key is the minimum interval key in the tree.
// It returns false as the second return value if the tree is empty; otherwise, true.
func (st *MultiValueSearchTree[V, T]) Min() ([]V, bool) {
	st.mu.RLock()
	defer st.mu.RUnlock()

	var vals []V
	if st.root == nil {
		return vals, false
	}

	vals = min(st.root).interval.vals

	return vals, true
}

// Max returns the values which interval key is the maximum interval in the tree.
// It returns false as the second return value if the tree is empty; otherwise, true.
func (st *MultiValueSearchTree[V, T]) Max() ([]V, bool) {
	st.mu.RLock()
	defer st.mu.RUnlock()

	var vals []V
	if st.root == nil {
		return vals, false
	}

	vals = max(st.root).interval.vals

	return vals, true
}

// Ceil returns the values which interval key is the smallest interval key greater than the given start and end interval.
// It returns true as the second return value if there's a ceiling interval key for the given start and end interval
// in the tree; otherwise, false.
func (st *MultiValueSearchTree[V, T]) Ceil(start, end T) ([]V, bool) {
	st.mu.RLock()
	defer st.mu.RUnlock()

	var vals []V
	interval, ok := ceil(st.root, start, end, st.cmp)
	if !ok {
		return vals, false
	}

	return interval.vals, true
}

// Floor returns the values which interval key is the greatest interval key lesser than the given start and end interval.
// It returns true as the second return value if there's a floor interval key for the given start and end interval
// in the tree; otherwise, false.
func (st *MultiValueSearchTree[V, T]) Floor(start, end T) ([]V, bool) {
	st.mu.RLock()
	defer st.mu.RUnlock()

	var vals []V
	interval, ok := floor(st.root, start, end, st.cmp)
	if !ok {
		return vals, false
	}

	return interval.vals, true
}

// Rank returns the number of intervals strictly less than the given start and end interval.
func (st *MultiValueSearchTree[V, T]) Rank(start, end T) int {
	st.mu.RLock()
	defer st.mu.RUnlock()

	return rank(st.root, start, end, st.cmp)
}

// Select returns the values which interval key is the kth smallest interval key in the tree.
// It returns false if k is not between 0 and N-1, where N is the number of interval keys
// in the tree; otherwise, true.
func (st *MultiValueSearchTree[V, T]) Select(k int) ([]V, bool) {
	st.mu.RLock()
	defer st.mu.RUnlock()

	var vals []V

	interval, ok := selectInterval(st.root, k)
	if !ok {
		return vals, false
	}

	return interval.vals, true
}
