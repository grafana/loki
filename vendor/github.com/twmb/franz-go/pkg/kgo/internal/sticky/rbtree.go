package sticky

// This file contains a vendoring of github.com/twmb/go-rbtree, with interface
// types replaced with *partitionLevel. We do this to simplify (and slightly)
// speed up the rbtree, get rid of a bunch of code we do not need, and to drop
// a dep.

type color bool

const red, black color = true, false

// Tree is a red-black tree.
type treePlan struct {
	root *treePlanNode
	size int
}

type treePlanNode struct {
	left   *treePlanNode
	right  *treePlanNode
	parent *treePlanNode
	color  color
	item   *partitionLevel
}

// liftRightSideOf is rotateLeft.
//
// Graphically speaking, this takes the node on the right and lifts it above
// ourselves. IMO trying to visualize a "rotation" is confusing.
func (t *treePlan) liftRightSideOf(n *treePlanNode) {
	r := n.right
	t.relinkParenting(n, r)

	// lift the right
	n.right = r.left
	n.parent = r

	// fix the lifted right's left
	if r.left != nil {
		r.left.parent = n
	}
	r.left = n
}

// liftLeftSideOf is rotateRight, renamed to aid my visualization.
func (t *treePlan) liftLeftSideOf(n *treePlanNode) {
	l := n.left
	t.relinkParenting(n, l)

	n.left = l.right
	n.parent = l

	if l.right != nil {
		l.right.parent = n
	}
	l.right = n
}

// relinkParenting is called to fix a former child c of node n's parent
// relationship to the parent of n.
//
// After this, the n node can be considered to have no parent.
func (t *treePlan) relinkParenting(n, c *treePlanNode) {
	p := n.parent
	if c != nil {
		c.parent = p
	}
	if p == nil {
		t.root = c
		return
	}
	if n == p.left {
		p.left = c
	} else {
		p.right = c
	}
}

func (n *treePlanNode) sibling() *treePlanNode {
	if n.parent == nil {
		return nil
	}
	if n == n.parent.left {
		return n.parent.right
	}
	return n.parent.left
}

func (n *treePlanNode) uncle() *treePlanNode {
	p := n.parent
	if p.parent == nil {
		return nil
	}
	return p.sibling()
}

func (n *treePlanNode) grandparent() *treePlanNode {
	return n.parent.parent
}

func (n *treePlanNode) isBlack() bool {
	return n == nil || n.color == black
}

func (t *treePlan) insert(i *partitionLevel) *treePlanNode {
	r := &treePlanNode{item: i}
	t.reinsert(r)
	return r
}

func (t *treePlan) reinsert(n *treePlanNode) {
	*n = treePlanNode{
		color: red,
		item:  n.item,
	}
	t.size++
	if t.root == nil {
		n.color = black
		t.root = n
		return
	}

	on := t.root
	var set **treePlanNode
	for {
		if n.item.less(on.item) {
			if on.left == nil {
				set = &on.left
				break
			}
			on = on.left
		} else {
			if on.right == nil {
				set = &on.right
				break
			}
			on = on.right
		}
	}

	n.parent = on
	*set = n

repair:
	// Case 1: we have jumped back to the root. Paint it black.
	if n.parent == nil {
		n.color = black
		return
	}

	// Case 2: if our parent is black, us being red does not add a new black
	// to the chain and cannot increase the maximum number of blacks from
	// root, so we are done.
	if n.parent.color == black {
		return
	}

	// Case 3: if we have an uncle and it is red, then we flip our
	// parent's, uncle's, and grandparent's color.
	//
	// This stops the red-red from parent to us, but may introduce
	// a red-red from grandparent to its parent, so we set ourselves
	// to the grandparent and go back to the repair beginning.
	if uncle := n.uncle(); uncle != nil && uncle.color == red {
		n.parent.color = black
		uncle.color = black
		n = n.grandparent()
		n.color = red
		goto repair
	}

	// Case 4 step 1: our parent is red but uncle is black. Step 2 relies
	// on the node being on the "outside". If we are on the inside, our
	// parent lifts ourselves above itself, thus making the parent the
	// outside, and then we become that parent.
	p := n.parent
	g := p.parent
	if n == p.right && p == g.left {
		t.liftRightSideOf(p)
		n = n.left
	} else if n == p.left && p == g.right {
		t.liftLeftSideOf(p)
		n = n.right
	}

	// Care 4 step 2: we are on the outside, and we and our parent are red.
	// If we are on the left, our grandparent lifts its left and then swaps
	// its and our parent's colors.
	//
	// This fixes the red-red situation while preserving the number of
	// blacks from root to leaf property.
	p = n.parent
	g = p.parent

	if n == p.left {
		t.liftLeftSideOf(g)
	} else {
		t.liftRightSideOf(g)
	}
	p.color = black
	g.color = red
}

func (t *treePlan) delete(n *treePlanNode) {
	t.size--

	// We only want to delete nodes with at most one child. If this has
	// two, we find the max node on the left, set this node's item to that
	// node's item, and then delete that max node.
	if n.left != nil && n.right != nil {
		remove := n.left.max()
		n.item, remove.item = remove.item, n.item
		n = remove
	}

	// Determine which child to elevate into our position now that we know
	// we have at most one child.
	c := n.right
	if n.right == nil {
		c = n.left
	}

	t.doDelete(n, c)
	t.relinkParenting(n, c)
}

// Since we do not represent leave nodes with objects, we relink the parent
// after deleting. See the Wikipedia note. Most of our deletion operations
// on n (the dubbed "shadow" node) rather than c.
func (t *treePlan) doDelete(n, c *treePlanNode) {
	// If the node was red, we deleted a red node; the number of black
	// nodes along any path is the same and we can quit.
	if n.color != black {
		return
	}

	// If the node was black, then, if we have a child and it is red,
	// we switch the child to black to preserve the path number.
	if c != nil && c.color == red {
		c.color = black
		return
	}

	// We either do not have a child (nil is black), or we do and it
	// is black. We must preserve the number of blacks.

case1:
	// Case 1: if the child is the new root, then the tree must have only
	// had up to two elements and now has one or zero.  We are done.
	if n.parent == nil {
		return
	}

	// Note that if we are here, we must have a sibling.
	//
	// The first time through, from the deleted node, the deleted node was
	// black and the child was black. This being two blacks meant that the
	// original node's parent required two blacks on the other side.
	//
	// The second time through, through case 3, the sibling was repainted
	// red... so it must still exist.

	// Case 2: if the child's sibling is red, we recolor the parent and
	// sibling and lift the sibling, ensuring we have a black sibling.
	s := n.sibling()
	if s.color == red {
		n.parent.color = red
		s.color = black
		if n == n.parent.left {
			t.liftRightSideOf(n.parent)
		} else {
			t.liftLeftSideOf(n.parent)
		}
		s = n.sibling()
	}

	// Right here, we know the sibling is black. If both sibling children
	// are black or nil leaves (black), we enter cases 3 and 4.
	if s.left.isBlack() && s.right.isBlack() {
		// Case 3: if the parent, sibling, sibling's children are
		// black, we can paint the sibling red to fix the imbalance.
		// However, the same black imbalance can exist on the other
		// side of the parent, so we go back to case 1 on the parent.
		s.color = red
		if n.parent.color == black {
			n = n.parent
			goto case1
		}

		// Case 4: if the sibling and sibling's children are black, but
		// the parent is red, We can swap parent and sibling colors to
		// fix our imbalance. We have no worry of further imbalances up
		// the tree since we deleted a black node, replaced it with a
		// red node, and then painted that red node black.
		n.parent.color = black
		return
	}

	// Now we know the sibling is black and one of its children is red.

	// Case 5: in preparation for 6, if we are on the left, we want our
	// sibling, if it has a right child, for that child's color to be red.
	// We swap the sibling and sibling's left's color (since we know the
	// sibling has a red child and that the right is black) and we lift the
	// left child.
	//
	// This keeps the same number of black nodes and under the sibling.
	if n == n.parent.left && s.right.isBlack() {
		s.color = red
		s.left.color = black
		t.liftLeftSideOf(s)
	} else if n == n.parent.right && s.left.isBlack() {
		s.color = red
		s.right.color = black
		t.liftRightSideOf(s)
	}
	s = n.sibling() // can change from the above case

	// At this point, we know we have a black sibling and, if we are on
	// the left, it has a red child on its right.

	// Case 6: we lift the sibling above the parent, swap the sibling's and
	// parent's color, and change the sibling's right's color from red to
	// black.
	//
	// This brings in a black above our node to replace the one we deleted,
	// while preserves the number of blacks on the other side of the path.
	s.color = n.parent.color
	n.parent.color = black
	if n == n.parent.left {
		s.right.color = black
		t.liftRightSideOf(n.parent)
	} else {
		s.left.color = black
		t.liftLeftSideOf(n.parent)
	}
}

func (t *treePlan) findWith(cmp func(*partitionLevel) int) *treePlanNode {
	on := t.root
	for on != nil {
		way := cmp(on.item)
		switch {
		case way < 0:
			on = on.left
		case way == 0:
			return on
		case way > 0:
			on = on.right
		}
	}
	return nil
}

func (t *treePlan) findWithOrInsertWith(
	find func(*partitionLevel) int,
	insert func() *partitionLevel,
) *treePlanNode {
	found := t.findWith(find)
	if found == nil {
		return t.insert(insert())
	}
	return found
}

func (t *treePlan) min() *treePlanNode {
	if t.root == nil {
		return nil
	}
	return t.root.min()
}

func (n *treePlanNode) min() *treePlanNode {
	for n.left != nil {
		n = n.left
	}
	return n
}

func (t *treePlan) max() *treePlanNode {
	if t.root == nil {
		return nil
	}
	return t.root.max()
}

func (n *treePlanNode) max() *treePlanNode {
	for n.right != nil {
		n = n.right
	}
	return n
}
