package tsdb

import (
	"github.com/grafana/loki/v3/pkg/logproto"
)

// mergeChunkRefsLoserTree performs a K-way streaming merge of already-sorted
// per-file chunk-ref lists using a loser tree (tournament tree) keyed on
// ChunkRef.Less. See
//   https://en.wikipedia.org/wiki/K-way_merge_algorithm#Tournament_Tree
//
// A loser tree does one comparison per level per emitted element (vs.
// log₂K sift-down comparisons for a binary heap), and the comparison path
// is fixed by position, which is cache-friendlier. At small K the constant
// setup cost of the tree can offset the win — benchmarks decide which
// approach ships.
//
// Vendored generic loser trees (github.com/bboreham/go-loser,
// github.com/grafana/dskit/loser) key on cmp.Ordered / constraints.Ordered,
// which excludes struct-valued ChunkRef. So this is hand-rolled; the shape
// follows dskit's iterative init and comparison-first replay.
//
// Dedup is inline against the last emitted ChunkRef, identical to
// mergeChunkRefsHeap. Callers retain ownership of input slices.
func mergeChunkRefsLoserTree(res []logproto.ChunkRefWithSizingInfo, xs [][]logproto.ChunkRefWithSizingInfo) []logproto.ChunkRefWithSizingInfo {
	if res == nil {
		res = ChunkRefsPool.Get()
	}
	res = res[:0]

	switch len(xs) {
	case 0:
		return res
	case 1:
		return dedupSortedInto(res, xs[0])
	}

	k := len(xs)
	// Pad number of leaves to next power of two ≥ 2. The iterative init
	// walks pairs of children, which relies on a fully balanced tree.
	nLeaves := 1
	for nLeaves < k {
		nLeaves *= 2
	}
	if nLeaves < 2 {
		nLeaves = 2
	}

	t := chunkRefLoserTree{
		xs:        xs,
		nLeaves:   nLeaves,
		cursors:   make([]int, nLeaves),
		exhausted: make([]bool, nLeaves),
		nodes:     make([]int, 2*nLeaves),
	}
	for i := 0; i < nLeaves; i++ {
		if i >= k || len(xs[i]) == 0 {
			t.exhausted[i] = true
		}
	}
	t.init()

	var (
		haveLast bool
		last     logproto.ChunkRef
	)
	for !t.exhausted[t.winner] {
		w := t.winner
		ref := xs[w][t.cursors[w]]
		if !haveLast || ref.ChunkRef != last {
			res = append(res, ref)
			last = ref.ChunkRef
			haveLast = true
		}
		t.advance()
	}

	return res
}

// chunkRefLoserTree is a fixed-shape tournament tree over K leaf lists.
//
// Layout (nLeaves is a power of two ≥ 2):
//   - t.nodes[0]         : winner leaf index
//   - t.nodes[1..nL-1]   : loser leaf index at each internal node
//   - t.nodes[nL..2nL-1] : leaves — nodes[nL+i] is leaf i
//
// Each leaf i corresponds to input xs[i] (or a padded sentinel when
// i >= len(xs)). t.cursors[i] is the current position in xs[i];
// t.exhausted[i] is true when leaf i has no more values.
type chunkRefLoserTree struct {
	xs        [][]logproto.ChunkRefWithSizingInfo
	nLeaves   int
	cursors   []int
	exhausted []bool
	nodes     []int
	winner    int
}

// less reports whether leaf a currently has a smaller value than leaf b.
// An exhausted leaf always compares greater (loses).
func (t *chunkRefLoserTree) less(a, b int) bool {
	aEx := t.exhausted[a]
	bEx := t.exhausted[b]
	if aEx {
		return false
	}
	if bEx {
		return true
	}
	return t.xs[a][t.cursors[a]].ChunkRef.Less(t.xs[b][t.cursors[b]].ChunkRef)
}

// init runs the initial tournament so every internal node holds the loser
// of its subtree and node 0 holds the overall winner. Follows the iterative
// pair-wise construction from grafana/dskit/loser.
func (t *chunkRefLoserTree) init() {
	// Scratch buffer holding the winner promoted through each subtree.
	// Indices mirror t.nodes.
	winners := make([]int, 2*t.nLeaves)
	// Leaves are their own initial winners.
	for i := t.nLeaves; i < 2*t.nLeaves; i++ {
		winners[i] = i - t.nLeaves
	}
	// Walk pairs (2i, 2i+1) from the deepest level up. Loser is stored on
	// the parent internal node; winner is promoted for the next round.
	for i := 2*t.nLeaves - 2; i > 0; i -= 2 {
		l := winners[i]
		r := winners[i+1]
		var loser, winner int
		if t.less(l, r) {
			winner, loser = l, r
		} else {
			winner, loser = r, l
		}
		p := i / 2
		t.nodes[p] = loser
		winners[p] = winner
	}
	t.winner = winners[1]
	t.nodes[0] = t.winner
}

// advance consumes the current winner's top value, moves its cursor, and
// replays the tournament along the path from that leaf up to the root.
// This is O(log₂ nLeaves) comparisons per call.
func (t *chunkRefLoserTree) advance() {
	w := t.winner
	t.cursors[w]++
	if t.cursors[w] >= len(t.xs[w]) {
		t.exhausted[w] = true
	}
	// Position of leaf w in t.nodes.
	pos := t.nLeaves + w
	for p := pos / 2; p > 0; p /= 2 {
		loser := t.nodes[p]
		if !t.less(w, loser) {
			// The stored loser now beats the previous winner; swap.
			t.nodes[p] = w
			w = loser
		}
	}
	t.winner = w
	t.nodes[0] = w
}
