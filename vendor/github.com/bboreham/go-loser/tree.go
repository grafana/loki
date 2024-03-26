// Loser tree, from https://en.wikipedia.org/wiki/K-way_merge_algorithm#Tournament_Tree

package loser

import "golang.org/x/exp/constraints"

type Value constraints.Ordered

type Sequence[E Value] interface {
	At() E      // Returns the current value.
	Next() bool // Advances and returns true if there is a value at this new position.
}

func New[E Value, S Sequence[E]](sequences []S, maxVal E) *Tree[E, S] {
	nSequences := len(sequences)
	t := Tree[E, S]{
		maxVal: maxVal,
		nodes:  make([]node[E, S], nSequences*2),
	}
	for i, s := range sequences {
		t.nodes[i+nSequences].items = s
		t.moveNext(i + nSequences) // Must call Next on each item so that At() has a value.
	}
	if nSequences > 0 {
		t.nodes[0].index = -1 // flag to be initialized on first call to Next().
	}
	return &t
}

// Call the close function on all sequences that are still open.
func (t *Tree[E, S]) Close() {
	for _, e := range t.nodes[len(t.nodes)/2 : len(t.nodes)] {
		if e.index == -1 {
			continue
		}
	}
}

// A loser tree is a binary tree laid out such that nodes N and N+1 have parent N/2.
// We store M leaf nodes in positions M...2M-1, and M-1 internal nodes in positions 1..M-1.
// Node 0 is a special node, containing the winner of the contest.
type Tree[E Value, S Sequence[E]] struct {
	maxVal E
	nodes  []node[E, S]
}

type node[E Value, S Sequence[E]] struct {
	index int // This is the loser for all nodes except the 0th, where it is the winner.
	value E   // Value copied from the loser node, or winner for node 0.
	items S   // Only populated for leaf nodes.
}

func (t *Tree[E, S]) moveNext(index int) bool {
	n := &t.nodes[index]
	if n.items.Next() {
		n.value = n.items.At()
		return true
	}
	n.value = t.maxVal
	n.index = -1
	return false
}

func (t *Tree[E, S]) Winner() S {
	return t.nodes[t.nodes[0].index].items
}

func (t *Tree[E, S]) At() E {
	return t.nodes[0].value
}

func (t *Tree[E, S]) Next() bool {
	nodes := t.nodes
	if len(nodes) == 0 {
		return false
	}
	if nodes[0].index == -1 { // If tree has not been initialized yet, do that.
		t.initialize()
		return nodes[nodes[0].index].index != -1
	}
	if nodes[nodes[0].index].index == -1 { // already exhausted
		return false
	}
	t.moveNext(nodes[0].index)
	t.replayGames(nodes[0].index)
	return nodes[nodes[0].index].index != -1
}

// Current winner has been advanced independently; fix up the loser tree.
func (t *Tree[E, S]) Fix(closed bool) {
	nodes := t.nodes
	cur := &nodes[nodes[0].index]
	if closed {
		cur.value = t.maxVal
		cur.index = -1
	} else {
		cur.value = cur.items.At()
	}
	t.replayGames(nodes[0].index)
}

func (t *Tree[E, S]) IsEmpty() bool {
	nodes := t.nodes
	if nodes[0].index == -1 { // If tree has not been initialized yet, do that.
		t.initialize()
	}
	return nodes[nodes[0].index].index == -1
}

func (t *Tree[E, S]) initialize() {
	winner := t.playGame(1)
	t.nodes[0].index = winner
	t.nodes[0].value = t.nodes[winner].value
}

// Find the winner at position pos; if it is a non-leaf node, store the loser.
// pos must be >= 1 and < len(t.nodes)
func (t *Tree[E, S]) playGame(pos int) int {
	nodes := t.nodes
	if pos >= len(nodes)/2 {
		return pos
	}
	left := t.playGame(pos * 2)
	right := t.playGame(pos*2 + 1)
	var loser, winner int
	if nodes[left].value < nodes[right].value {
		loser, winner = right, left
	} else {
		loser, winner = left, right
	}
	nodes[pos].index = loser
	nodes[pos].value = nodes[loser].value
	return winner
}

// Starting at pos, which is a winner, re-consider all values up to the root.
func (t *Tree[E, S]) replayGames(pos int) {
	nodes := t.nodes
	winningValue := nodes[pos].value
	for n := parent(pos); n != 0; n = parent(n) {
		node := &nodes[n]
		if node.value < winningValue {
			// Record pos as the loser here, and the old loser is the new winner.
			node.index, pos = pos, node.index
			node.value, winningValue = winningValue, node.value
		}
	}
	// pos is now the winner; store it in node 0.
	nodes[0].index = pos
	nodes[0].value = winningValue
}

func parent(i int) int { return i >> 1 }
