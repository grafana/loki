package sticky

import "container/heap"

// Graph maps members to partitions they want to steal.
//
// The representation was chosen so as to avoid updating all members on any
// partition move; move updates are one map update.
type graph struct {
	b *balancer

	// node => edges out
	// "from a node (member), which topicNum could we steal?"
	out [][]uint32

	// edge => who owns this edge; built in balancer's assignUnassigned
	cxns []partitionConsumer

	// scores are all node scores from a search node. The distance field
	// is reset on findSteal to infinityScore..
	scores pathScores

	// heapBuf and pathBuf are backing buffers that are reused every
	// findSteal; note that pathBuf must be done being used before
	// the next find steal, but it always is.
	heapBuf pathHeap
	pathBuf []stealSegment
}

func (b *balancer) newGraph(
	partitionConsumers []partitionConsumer,
	topicPotentials [][]uint16,
) graph {
	g := graph{
		b:       b,
		out:     make([][]uint32, len(b.members)),
		cxns:    partitionConsumers,
		scores:  make([]pathScore, len(b.members)),
		heapBuf: make([]*pathScore, len(b.members)),
	}
	outBufs := make([]uint32, len(b.members)*len(topicPotentials))
	for memberNum := range b.plan {
		out := outBufs[:0:len(topicPotentials)]
		outBufs = outBufs[len(topicPotentials):]
		// In the worst case, if every node is linked to each other,
		// each node will have nparts edges. We preallocate the worst
		// case. It is common for the graph to be highly connected.
		g.out[memberNum] = out
	}
	for topicNum, potentials := range topicPotentials {
		for _, potential := range potentials {
			g.out[potential] = append(g.out[potential], uint32(topicNum))
		}
	}
	return g
}

func (g *graph) changeOwnership(edge int32, newDst uint16) {
	g.cxns[edge].memberNum = newDst
}

// findSteal uses Dijkstra search to find a path from the best node it can reach.
func (g *graph) findSteal(from uint16) ([]stealSegment, bool) {
	// First, we must reset our scores from any prior run. This is O(M),
	// but is fast and faster than making a map and extending it a lot.
	for i := range g.scores {
		g.scores[i].distance = infinityScore
		g.scores[i].done = false
	}

	first, _ := g.getScore(from)

	first.distance = 0
	first.done = true

	g.heapBuf = append(g.heapBuf[:0], first)
	rem := &g.heapBuf
	for rem.Len() > 0 {
		current := heap.Pop(rem).(*pathScore)
		if current.level > first.level+1 {
			path := g.pathBuf[:0]
			for current.parent != nil {
				path = append(path, stealSegment{
					current.node,
					current.parent.node,
					current.srcEdge,
				})
				current = current.parent
			}
			g.pathBuf = path
			return path, true
		}

		current.done = true

		for _, topicNum := range g.out[current.node] {
			info := g.b.topicInfos[topicNum]
			firstPartNum, lastPartNum := info.partNum, info.partNum+info.partitions
			for edge := firstPartNum; edge < lastPartNum; edge++ {
				neighborNode := g.cxns[edge].memberNum
				neighbor, isNew := g.getScore(neighborNode)
				if neighbor.done {
					continue
				}

				distance := current.distance + 1

				// The neighbor is the current node that owns this edge.
				// If our node originally owned this partition, then it
				// would be preferable to steal edge back.
				srcIsOriginal := g.cxns[edge].originalNum == current.node

				// If this is a new neighbor (our first time seeing the neighbor
				// in our search), this is also the shortest path to reach them,
				// where shortest defers preference to original sources THEN distance.
				if isNew {
					neighbor.parent = current
					neighbor.srcIsOriginal = srcIsOriginal
					neighbor.srcEdge = edge
					neighbor.distance = distance
					neighbor.heapIdx = len(*rem)
					heap.Push(rem, neighbor)
				} else if !neighbor.srcIsOriginal && srcIsOriginal {
					// If the search path has seen this neighbor before, but
					// we now are evaluating a partition that would increase
					// stickiness if stolen, then fixup the neighbor's parent
					// and srcEdge.
					neighbor.parent = current
					neighbor.srcIsOriginal = true
					neighbor.srcEdge = edge
					neighbor.distance = distance
					heap.Fix(rem, neighbor.heapIdx)
				}
			}
		}
	}

	return nil, false
}

type stealSegment struct {
	src  uint16 // member num
	dst  uint16 // member num
	part int32  // partNum
}

// As we traverse a graph, we assign each node a path score, which tracks a few
// numbers for what it would take to reach this node from our first node.
type pathScore struct {
	// Done is set to true when we pop a node off of the graph. Once we
	// pop a node, it means we have found a best path to that node and
	// we do not want to revisit it for processing if any other future
	// nodes reach back to this one.
	done bool

	// srcIsOriginal is true if, were our parent to steal srcEdge, would
	// that put srcEdge back on the original member. That is, if we are B
	// and our parent is A, does our srcEdge originally belong do A?
	//
	// This field exists to work around a very slim edge case where a
	// partition is stolen by B and then needs to be stolen back by A
	// later.
	srcIsOriginal bool

	node     uint16 // our member num
	distance int32  // how many steals it would take to get here
	srcEdge  int32  // the partition used to reach us
	level    int32  // partitions owned on this segment
	parent   *pathScore
	heapIdx  int
}

type pathScores []pathScore

const infinityScore = 1<<31 - 1

func (g *graph) getScore(node uint16) (*pathScore, bool) {
	r := &g.scores[node]
	exists := r.distance != infinityScore
	if !exists {
		*r = pathScore{
			node:     node,
			level:    int32(len(g.b.plan[node])),
			distance: infinityScore,
		}
	}
	return r, !exists
}

type pathHeap []*pathScore

func (p *pathHeap) Len() int { return len(*p) }
func (p *pathHeap) Swap(i, j int) {
	h := *p
	l, r := h[i], h[j]
	l.heapIdx, r.heapIdx = r.heapIdx, l.heapIdx
	h[i], h[j] = r, l
}

// For our path, we always want to prioritize stealing a partition we
// originally owned. This may result in a longer steal path, but it will
// increase stickiness.
//
// Next, our real goal, which is to find a node we can steal from. Because of
// this, we always want to sort by the highest level. The pathHeap stores
// reachable paths, so by sorting by the highest level, we terminate quicker:
// we always check the most likely candidates to quit our search.
//
// Finally, we simply prefer searching through shorter paths and, barring that,
// just sort by node.
func (p *pathHeap) Less(i, j int) bool {
	l, r := (*p)[i], (*p)[j]
	return l.srcIsOriginal && !r.srcIsOriginal || !l.srcIsOriginal && !r.srcIsOriginal &&
		(l.level > r.level || l.level == r.level &&
			(l.distance < r.distance || l.distance == r.distance &&
				l.node < r.node))
}

func (p *pathHeap) Push(x any) { *p = append(*p, x.(*pathScore)) }
func (p *pathHeap) Pop() any {
	h := *p
	l := len(h)
	r := h[l-1]
	*p = h[:l-1]
	return r
}
