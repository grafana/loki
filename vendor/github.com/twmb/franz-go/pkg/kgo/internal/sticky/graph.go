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

	// topicOwners is, per topic, the members holding any of its partitions
	// and which ones. The search walks this rather than every partition of
	// the topic: a member holding a thousand partitions of a topic is one
	// edge, not a thousand. Entries are emptied but never removed, so an
	// owner's index is stable for the life of a balance.
	topicOwners [][]ownerParts

	// partPos is each partition's index within its owner's parts, so that
	// moving a partition is O(1) rather than a scan.
	partPos []int32

	// One of these finds a member's entry in topicOwners: the grid when
	// it is at most a few times the partition count, else the map.
	ownerGrid []int32
	ownerMap  map[uint64]int32

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
		b:           b,
		out:         make([][]uint32, len(b.members)),
		cxns:        partitionConsumers,
		scores:      make([]pathScore, len(b.members)),
		heapBuf:     make([]*pathScore, len(b.members)),
		topicOwners: make([][]ownerParts, len(b.topicInfos)),
		partPos:     make([]int32, len(partitionConsumers)),
	}
	if grid := len(b.topicInfos) * len(b.members); grid <= 8*len(partitionConsumers) {
		g.ownerGrid = make([]int32, grid)
		for i := range g.ownerGrid {
			g.ownerGrid[i] = -1
		}
	} else {
		g.ownerMap = make(map[uint64]int32)
	}
	// Out edges are per topic, not per partition. As in topicPotentials,
	// we reserve the average topics per member and let the few members
	// above average grow by append.
	var nsubs int
	for _, potentials := range topicPotentials {
		nsubs += len(potentials)
	}
	perMember := nsubs/len(b.members) + 1
	outBufs := make([]uint32, perMember*len(b.members))
	for memberNum := range b.plan {
		g.out[memberNum] = outBufs[:0:perMember]
		outBufs = outBufs[perMember:]
	}
	for topicNum, potentials := range topicPotentials {
		for _, potential := range potentials {
			g.out[potential] = append(g.out[potential], uint32(topicNum))
		}
	}
	for edge, cxn := range partitionConsumers {
		if cxn.memberNum != unassignedPart {
			g.addEdge(int32(edge), cxn.memberNum)
		}
	}
	return g
}

// ownerParts is one member's partitions of one topic.
type ownerParts struct {
	member uint16
	parts  []int32
}

func (g *graph) ownerOf(topicNum uint32, member uint16) *ownerParts {
	owners := g.topicOwners[topicNum]
	at := int32(-1)
	if g.ownerGrid != nil {
		at = g.ownerGrid[int(topicNum)*len(g.b.members)+int(member)]
	} else if i, ok := g.ownerMap[uint64(topicNum)<<16|uint64(member)]; ok {
		at = i
	}
	if at < 0 {
		at = int32(len(owners))
		if g.ownerGrid != nil {
			g.ownerGrid[int(topicNum)*len(g.b.members)+int(member)] = at
		} else {
			g.ownerMap[uint64(topicNum)<<16|uint64(member)] = at
		}
		g.topicOwners[topicNum] = append(owners, ownerParts{member: member})
	}
	return &g.topicOwners[topicNum][at]
}

func (g *graph) addEdge(edge int32, member uint16) {
	o := g.ownerOf(g.b.partOwners[edge], member)
	g.partPos[edge] = int32(len(o.parts))
	o.parts = append(o.parts, edge)
}

func (g *graph) removeEdge(edge int32, member uint16) {
	o := g.ownerOf(g.b.partOwners[edge], member)
	s := o.parts
	i, last := g.partPos[edge], int32(len(s)-1)
	s[i] = s[last]
	g.partPos[s[i]] = i
	o.parts = s[:last]
}

func (g *graph) changeOwnership(edge int32, newDst uint16) {
	g.removeEdge(edge, g.cxns[edge].memberNum)
	g.cxns[edge].memberNum = newDst
	g.addEdge(edge, newDst)
}

// findSteal uses Dijkstra search to find a path from the best node it can reach.
func (g *graph) findSteal(from uint16) ([]stealSegment, bool) {
	// First, we must reset our scores from any prior run. This is O(M),
	// but is fast and faster than making a map and extending it a lot.
	for i := range g.scores {
		g.scores[i].distance = infinityScore
	}

	first, _ := g.getScore(from)
	first.distance = 0

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

		// One edge per member holding any of a topic we want, stealing
		// whichever of its partitions comes first. Walking every partition
		// would rediscover the same members over and over.
		for _, topicNum := range g.out[current.node] {
			for i := range g.topicOwners[topicNum] {
				o := &g.topicOwners[topicNum][i]
				if len(o.parts) == 0 {
					continue // emptied, but kept so indices stay stable
				}
				g.reach(rem, current, o.member, o.parts[0])
			}
		}
	}

	return nil, false
}

// reach records the first way we find to reach neighborNode, which is by
// current stealing edge from it. Later ways are never shorter.
func (g *graph) reach(rem *pathHeap, current *pathScore, neighborNode uint16, edge int32) {
	neighbor, isNew := g.getScore(neighborNode)
	if !isNew {
		return
	}
	neighbor.parent = current
	neighbor.srcEdge = edge
	neighbor.distance = current.distance + 1
	heap.Push(rem, neighbor)
}

type stealSegment struct {
	src  uint16 // member num
	dst  uint16 // member num
	part int32  // partNum
}

// As we traverse a graph, we assign each node a path score, which tracks a few
// numbers for what it would take to reach this node from our first node.
type pathScore struct {
	node     uint16 // our member num
	distance int32  // how many steals it would take to get here
	srcEdge  int32  // the partition used to reach us
	level    int32  // partitions owned on this segment
	parent   *pathScore
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
	h[i], h[j] = h[j], h[i]
}

// Our goal is to find a node we can steal from, so we sort by the highest
// level. The pathHeap stores reachable paths, so by sorting by the highest
// level, we terminate quicker: we always check the most likely candidates
// to quit our search.
//
// Barring that, we simply prefer searching through shorter paths and, barring
// that, just sort by node.
func (p *pathHeap) Less(i, j int) bool {
	l, r := (*p)[i], (*p)[j]
	return l.level > r.level || l.level == r.level &&
		(l.distance < r.distance || l.distance == r.distance &&
			l.node < r.node)
}

func (p *pathHeap) Push(x any) { *p = append(*p, x.(*pathScore)) }
func (p *pathHeap) Pop() any {
	h := *p
	l := len(h)
	r := h[l-1]
	*p = h[:l-1]
	return r
}
