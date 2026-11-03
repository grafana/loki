package sticky

import (
	"math"
	"slices"
)

// NOTE: The repair algorithm and the explanation below are LLM written.
// They came out of a session that explored open research on optimal ways
// to represent flows and stickiness. The test suite ensuring correctness
// and speed is extensive: a brute force over random small groups checks
// the exact optimum on balance, rack, and stickiness, and the repair has
// its own benchmarks. Trust the tests before the prose.

// Balancing settles how many partitions each member holds but not which.
// Among the assignments with those counts, some keep far more partitions
// where they were, and some place far more in the rack they are led from.
// The repair below rearranges the plan to the best of them: balance first,
// then rack, then stickiness.
//
// Partitions nobody can tell apart form a row: they have the same
// subscribers and are led from the same rack. The plan collapses to a
// table of how many of each row each member holds, and how many of those
// it arrived with. Take four members and two topics, with no racks. m0
// reads t1; m1 reads both and arrived holding t1/0; m2 reads t0 and
// arrived holding t0/0 and t0/1; m3 is new and reads both. Three
// partitions over four members, so somebody ends with nothing. Balancing
// gave t1/0 to m0 and t0/1 to m1, and only m2's t0/0 stayed put:
//
//	     row t0 (m1, m2, m3)      row t1 (m0, m1, m3)
//	m0                            holds 1, arrived with 0
//	m1   holds 1, arrived with 0  holds 0, arrived with 1
//	m2   holds 1, arrived with 2
//	m3   holds 0, arrived with 0  holds 0, arrived with 0
//
// A cell's price is what it holds beyond what it arrived with, since each
// of those is a move, plus a weight far above any number of moves for
// every partition it holds off rack. A rotation takes one from some cells
// and adds one to others. Here, m3 taking one of t0 costs a move, m1
// giving one of t0 up saves one, m1 taking one of t1 back is free since
// it arrived with one, and m0 giving t1 up saves one: a rotation worth
// -1. It leaves m0 with nothing and m3 with one, which is allowed because
// m0 sat one level above m3: the sorted loads are {1, 1, 1, 0} before and
// after, and two partitions now stay put instead of one.
//
// The table is a flow and this is cycle cancelling on it. A cell's cost is
// convex in its count, so the residual graph has one arc each way per
// cell, priced at the next partition in or out, and the plan is optimal
// exactly when no rotation lowers its price. We find negative cycles with
// Bellman-Ford and cancel them until none remain, then rewrite the plan
// from the table with every member first taking back what it arrived
// with.

// stickyCell counts how many partitions of one row one member holds, and
// prices the next one in or out.
type stickyCell struct {
	add, drop int64 // what one more partition here costs, and one fewer
	row       int32
	x         int32 // partitions of this row the member holds
	held      int32 // how many of them it arrived holding
	member    uint16
	offrack   bool // the member is not in this row's rack
}

// noDrop is the price of giving up a partition a cell does not hold: past
// any distance the finder can reach, so that arc never relaxes.
const noDrop = 1 << 62

// reprice sets what one more partition here costs, which is a move unless
// the member is still below what it arrived with, plus a zone crossing if
// it is off rack; and what giving one up costs, which is what the last one
// in cost.
func (c *stickyCell) reprice(rackw int64) {
	c.add, c.drop = 0, 0
	if c.x >= c.held {
		c.add = 1
	}
	if c.x > c.held {
		c.drop = -1
	}
	if c.offrack {
		c.add += rackw
		c.drop -= rackw
	}
	if c.x == 0 {
		c.drop = noDrop
	}
}

// room is how many partitions can move through this cell in the given
// direction before the price of the next one changes.
func (c *stickyCell) room(delta int32) int32 {
	if delta > 0 {
		if c.x < c.held {
			return c.held - c.x
		}
		return math.MaxInt32
	}
	if c.x > c.held {
		return c.x - c.held
	}
	return c.x
}

// stickyTable is the rows by members table the repair works on. Topics
// with the same subscribers form a class, and row r holds the partitions
// of class r/stride led from rack r%stride. A member has a slot in every
// rack's row of each of its classes, in order, so a partition's row and a
// member's slot in it are arithmetic on the class and rack. The cells are
// the slots of rows that hold any partition; the rest could never change.
type stickyTable struct {
	cells         []stickyCell
	cellAt        []int32   // slot => cell, or -1 in a row with no partitions
	memberClasses [][]int32 // each member's classes, sorted
	classStart    []int32   // member m's slots start at classStart[m]*stride
	stride        int32     // racks plus one for no rack: rows and slots per class
	nrows         int32

	// rackw is the price of holding one partition off rack. No two plans
	// differ in stickiness by more than the number of partitions, so at
	// this weight one zone crossing outranks any amount of stickiness.
	rackw int64

	owners  []uint32 // partition => topic
	classOf []int32  // topic => class
	racks   []uint16 // partition => rack, nil without racks

	// crossed is set if some partition is held by a member other than the
	// one that arrived with it. Counts cannot see that: two members each
	// holding the other's partition look settled.
	crossed bool
}

func (t *stickyTable) rowOf(partNum int32) int32 {
	var rack int32
	if t.racks != nil {
		rack = int32(t.racks[partNum])
	}
	return t.classOf[t.owners[partNum]]*t.stride + rack
}

// slotOf returns a partition's row and the slot of a member in it, or -1
// for the slot if the member does not subscribe to the partition's topic.
func (t *stickyTable) slotOf(partNum int32, member uint16) (row, slot int32) {
	class := t.classOf[t.owners[partNum]]
	var rack int32
	if t.racks != nil {
		rack = int32(t.racks[partNum])
	}
	mine := t.memberClasses[member]
	lo, hi := 0, len(mine)
	for lo < hi {
		if mid := (lo + hi) / 2; mine[mid] < class {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	if lo == len(mine) || mine[lo] != class {
		return class*t.stride + rack, -1
	}
	return class*t.stride + rack, (t.classStart[member]+int32(lo))*t.stride + rack
}

// cellOf is slotOf's cell, or -1 in a row that holds no partition.
func (t *stickyTable) cellOf(partNum int32, member uint16) (row, cell int32) {
	row, slot := t.slotOf(partNum, member)
	if slot < 0 {
		return row, -1
	}
	return row, t.cellAt[slot]
}

// repairAssignment rearranges which partitions each member holds until no
// rotation through the table lowers the price.
func (b *balancer) repairAssignment() {
	// With uniform subscriptions and no racks there is one row, and the
	// only rotation is a member handing a partition to one a level below.
	// That lowers the price only if the giver holds more than it arrived
	// with and the taker less, and balancing never leaves that: unassigned
	// partitions go to the least loaded member first, then moves go from
	// the top level to the bottom with the top only falling and the bottom
	// only rising, so no member that received ends above one that gave.
	if !b.isComplex && b.partRacks == nil {
		return
	}
	t := b.stickyTable()
	if len(t.cells) == 0 {
		return
	}
	loads := make([]int32, len(b.members))
	for i := range t.cells {
		loads[t.cells[i].member] += t.cells[i].x
	}

	f := newStickyFinder(int(t.nrows), len(b.members))
	var moved bool
	for cycles := f.find(t.cells, loads); len(cycles) > 0; cycles = f.find(t.cells, loads) {
		moved = true
		for _, cycle := range cycles {
			t.apply(cycle, loads)
		}
	}

	// If nothing rotated and everybody still holds what it arrived with,
	// the plan already is what the table says.
	if !moved && !t.crossed {
		return
	}
	b.realizeAssignment(t)
}

// apply moves partitions around one rotation. Every cell on it holds the
// same price for as many partitions as its tightest cell has room for, so
// they all move at once. A rotation that shifts load moves one: it changes
// which levels two members sit on, so the next search has to re-price it.
func (t *stickyTable) apply(cycle stickyCycle, loads []int32) {
	n := int32(1)
	if !cycle.shiftsLoad {
		n = math.MaxInt32
		for _, step := range cycle.steps {
			n = min(n, t.cells[step.cell].room(step.delta))
		}
	}
	for _, step := range cycle.steps {
		c := &t.cells[step.cell]
		c.x += step.delta * n
		c.reprice(t.rackw)
		loads[c.member] += step.delta * n
	}
}

// topicClasses groups topics whose subscribers are exactly the same set:
// any member that may hold one may hold the other, and nothing else about
// a topic is priced. Two thousand members reading the same topics with one
// of them reading one extra is two classes, not a row per topic.
//
// Returns the class of each topic and how many classes there are.
func (b *balancer) topicClasses() (classOf []int32, nclasses int) {
	classOf = make([]int32, len(b.topicNames))
	if !b.isComplex {
		return classOf, 1
	}

	nwords := (len(b.members) + 63) / 64
	subs := make([]uint64, len(b.topicNames)*nwords)
	for topicNum, members := range b.subscribers {
		for _, m := range members {
			subs[topicNum*nwords+int(m)/64] |= 1 << (m % 64)
		}
	}
	bitset := func(topicNum int32) []uint64 {
		return subs[int(topicNum)*nwords : int(topicNum+1)*nwords]
	}

	// Topics are bucketed by a hash of their bitset, mixed with FNV-1a's
	// constants a word at a time. Only the equality check below decides
	// membership; the hash just keeps the comparisons few.
	const fnvOffset, fnvPrime = 14695981039346656037, 1099511628211
	byHash := make(map[uint64][]int32)
	var reps []int32 // one topic of each class
	for topicNum := range classOf {
		mine := bitset(int32(topicNum))
		h := uint64(fnvOffset)
		for _, w := range mine {
			h = (h ^ w) * fnvPrime
		}
		class := int32(-1)
		for _, c := range byHash[h] {
			if slices.Equal(bitset(reps[c]), mine) {
				class = c
				break
			}
		}
		if class < 0 {
			class = int32(len(reps))
			reps = append(reps, int32(topicNum))
			byHash[h] = append(byHash[h], class)
		}
		classOf[topicNum] = class
	}
	return classOf, len(reps)
}

// stickyTable builds the table of rows by the members that may hold them.
func (b *balancer) stickyTable() *stickyTable {
	classOf, nclasses := b.topicClasses()
	t := &stickyTable{
		memberClasses: make([][]int32, len(b.members)),
		classStart:    make([]int32, len(b.members)+1),
		stride:        int32(b.nRacks) + 1,
		owners:        b.partOwners,
		classOf:       classOf,
		racks:         b.partRacks,
	}
	t.nrows = int32(nclasses) * t.stride

	// A member's classes are those of the topics it subscribes to. With
	// uniform subscriptions that is the one class; otherwise we gather them
	// from the subscribers of each topic, then sort each member's and drop
	// the repeats.
	if !b.isComplex {
		zero := []int32{0}
		for m := range b.members {
			t.memberClasses[m] = zero
		}
	} else {
		counts := make([]int32, len(b.members)+1)
		for _, members := range b.subscribers {
			for _, m := range members {
				counts[m+1]++
			}
		}
		for m := range b.members {
			counts[m+1] += counts[m]
		}
		flat := make([]int32, counts[len(b.members)])
		next := slices.Clone(counts[:len(b.members)])
		for topicNum, members := range b.subscribers {
			for _, m := range members {
				flat[next[m]] = classOf[topicNum]
				next[m]++
			}
		}
		// A member reading five hundred topics of one class has five
		// hundred copies of it here; a stamp per class drops the repeats
		// before the sort rather than after.
		lastSeen := make([]int32, nclasses) // member that last had the class, plus one
		for m := range b.members {
			mine := flat[counts[m]:counts[m+1]]
			kept := mine[:0]
			for _, class := range mine {
				if lastSeen[class] != int32(m)+1 {
					lastSeen[class] = int32(m) + 1
					kept = append(kept, class)
				}
			}
			slices.Sort(kept)
			t.memberClasses[m] = kept
		}
	}
	for m := range b.members {
		t.classStart[m+1] = t.classStart[m] + int32(len(t.memberClasses[m]))
	}

	// A member gets a slot in every rack's row of each of its classes.
	slots := make([]stickyCell, 0, int(t.classStart[len(b.members)])*int(t.stride))
	for m := range b.members {
		for _, class := range t.memberClasses[m] {
			for rack := range t.stride {
				offrack := rack != noRack && b.memberRacks[m] != noRack && uint16(rack) != b.memberRacks[m]
				slots = append(slots, stickyCell{row: class*t.stride + rack, member: uint16(m), offrack: offrack})
			}
		}
	}
	for m := range b.plan {
		for _, p := range b.plan[m] {
			if orig := b.origOwner[p]; orig != unassignedPart && orig != uint16(m) {
				t.crossed = true
			}
			if _, s := t.slotOf(p, uint16(m)); s >= 0 {
				slots[s].x++
			}
		}
	}
	// held counts everything a member arrived with, not only what it still
	// has: a member that lost four of a row it had five of can take three
	// back, and counting only current holdings makes every cell look settled.
	for p, orig := range b.origOwner {
		if orig == unassignedPart {
			continue
		}
		if _, s := t.slotOf(int32(p), orig); s >= 0 {
			slots[s].held++
		}
	}

	// Only a row that holds partitions can take part in a rotation. With
	// racks that leaves out every class's no rack row and every rack a
	// class has no partition in, so the finder gets the rest and nothing
	// else.
	rowTotal := make([]int32, t.nrows)
	for i := range slots {
		rowTotal[slots[i].row] += slots[i].x
	}
	t.rackw = int64(len(b.partOwners)) + 1
	t.cellAt = make([]int32, len(slots))
	for i := range slots {
		t.cellAt[i] = -1
		if rowTotal[slots[i].row] > 0 {
			t.cellAt[i] = int32(len(t.cells))
			slots[i].reprice(t.rackw)
			t.cells = append(t.cells, slots[i])
		}
	}
	return t
}

// stickyStep is one leg of a rotation: a cell gaining or losing one.
type stickyStep struct {
	cell  int32
	delta int32
}

// stickyCycle is one rotation that lowers the price. shiftsLoad is set if
// it moves load between members, which only happens one level at a time.
type stickyCycle struct {
	steps      []stickyStep
	shiftsLoad bool
}

const stickyUnset = -1

// stickyFinder searches the table for a rotation that lowers the price.
// Nodes are rows, members, and one node per level of load. A row to a
// member means the member takes one more of the row; the member back to
// the row means it gives one up. A member to its level's node and out to a
// member one level below shifts one partition of load between them, which
// leaves the balance exactly as it was.
//
// Nothing stops a rotation from entering a member from one level node and
// leaving to the next, which would shift load two levels and improve the
// balance. Balancing has already made the load vector optimal, so no such
// rotation is feasible and none is ever found.
//
// This is Bellman-Ford from every node at once. The predecessor chains
// close a cycle exactly when some rotation lowers the price.
type stickyFinder struct {
	nrows, nmembers int
	levelAt         []int32 // load => level node, or unset for a load nobody has
	dist            []int64
	viaCell         []int32 // the cell a node was reached through, if any
	viaFrom         []int32 // the node it was reached from
	seen            []int32
	roots           []int32
	cycles          []stickyCycle
}

func newStickyFinder(nrows, nmembers int) *stickyFinder {
	nodes := nrows + 2*nmembers // at most one level per member
	return &stickyFinder{
		nrows:    nrows,
		nmembers: nmembers,
		dist:     make([]int64, nodes),
		viaCell:  make([]int32, nodes),
		viaFrom:  make([]int32, nodes),
		seen:     make([]int32, nodes),
	}
}

func (f *stickyFinder) memberNode(m uint16) int32 { return int32(f.nrows) + int32(m) }

// find returns rotations that lower the price and share no node, or
// nothing if none is left.
func (f *stickyFinder) find(cells []stickyCell, loads []int32) []stickyCycle {
	// Load only shifts to a member one level below, so each level some
	// member is on gets a node: members one below enter it by taking, and
	// members on it leave it by giving one up.
	var top int32
	for _, load := range loads {
		top = max(top, load)
	}
	f.levelAt = slices.Grow(f.levelAt[:0], int(top)+2)[:top+2]
	for i := range f.levelAt {
		f.levelAt[i] = stickyUnset
	}
	nodes := f.nrows + f.nmembers
	for _, load := range loads {
		if load > 0 && f.levelAt[load] == stickyUnset {
			f.levelAt[load] = int32(nodes)
			nodes++
		}
	}
	clear(f.dist[:nodes])
	for i := range nodes {
		f.viaCell[i] = stickyUnset
		f.viaFrom[i] = stickyUnset
	}

	for range nodes {
		moved := false
		for i := range cells {
			c := &cells[i]
			r, m := c.row, f.memberNode(c.member)
			if d := f.dist[r] + c.add; d < f.dist[m] {
				f.dist[m], f.viaCell[m], f.viaFrom[m] = d, int32(i), r
				moved = true
			}
			if d := f.dist[m] + c.drop; d < f.dist[r] {
				f.dist[r], f.viaCell[r], f.viaFrom[r] = d, int32(i), m
				moved = true
			}
		}
		for m := range f.nmembers {
			node := f.memberNode(uint16(m))
			// Taking one more puts a member on the level above; a member
			// on that level giving one up comes down to this one.
			if take := f.levelAt[loads[m]+1]; take != stickyUnset && f.dist[node] < f.dist[take] {
				f.dist[take], f.viaCell[take], f.viaFrom[take] = f.dist[node], stickyUnset, node
				moved = true
			}
			if loads[m] > 0 {
				if shed := f.levelAt[loads[m]]; f.dist[shed] < f.dist[node] {
					f.dist[node], f.viaCell[node], f.viaFrom[node] = f.dist[shed], stickyUnset, shed
					moved = true
				}
			}
		}
		if !moved {
			return nil
		}
		if f.roots = f.onCycles(f.roots[:0], nodes); len(f.roots) > 0 {
			f.cycles = f.cycles[:0]
			for _, at := range f.roots {
				f.cycles = append(f.cycles, f.extract(at))
			}
			return f.cycles
		}
	}

	// Unreachable: a distance can only still fall after a pass per node
	// if its predecessor chain has closed a cycle, which the check above
	// would have found.
	return nil
}

// onCycles returns one node from every cycle in the predecessor chains.
// Each node has one predecessor, so no two cycles share a node, and every
// one of them is a rotation that lowers the price.
func (f *stickyFinder) onCycles(roots []int32, nodes int) []int32 {
	seen := f.seen[:nodes]
	for i := range seen {
		seen[i] = stickyUnset
	}
	for start := range int32(nodes) {
		if seen[start] != stickyUnset {
			continue
		}
		node := start
		for node != stickyUnset && seen[node] == stickyUnset {
			seen[node] = start
			node = f.viaFrom[node]
		}
		if node != stickyUnset && seen[node] == start {
			roots = append(roots, node)
		}
	}
	return roots
}

func (f *stickyFinder) extract(at int32) stickyCycle {
	var cycle stickyCycle
	for node := at; ; {
		switch {
		case node >= int32(f.nrows+f.nmembers): // a level node
			cycle.shiftsLoad = true
		case f.viaCell[node] == stickyUnset: // a member reached from a level node
		case node >= int32(f.nrows): // a member taking one from a row
			cycle.steps = append(cycle.steps, stickyStep{f.viaCell[node], +1})
		default: // a row a member gave one back to
			cycle.steps = append(cycle.steps, stickyStep{f.viaCell[node], -1})
		}
		if node = f.viaFrom[node]; node == at {
			return cycle
		}
	}
}

// realizeAssignment rewrites the plan to the table's counts. Every member
// first takes back what it arrived with, as far as its count allows;
// whatever is left goes on its row's pile, and cells still short fill up
// from there. Going by who arrived with a partition rather than who holds
// it is what lets a member recover what it lost.
func (b *balancer) realizeAssignment(t *stickyTable) {
	// A row's pile ends up holding what its cells cannot take back, which
	// the counts already say.
	need := make([]int32, len(t.cells))
	pileSize := make([]int32, t.nrows)
	for i := range t.cells {
		c := &t.cells[i]
		need[i] = c.x
		pileSize[c.row] += c.x - min(c.x, c.held)
	}
	piles := make([][]int32, t.nrows)
	for r := range piles {
		piles[r] = make([]int32, 0, pileSize[r])
	}
	var total int
	for m := range b.plan {
		total += len(b.plan[m])
	}
	all := make([]int32, 0, total)
	for m := range b.plan {
		all = append(all, b.plan[m]...)
		b.plan[m] = b.plan[m][:0]
	}
	for _, p := range all {
		if orig := b.origOwner[p]; orig != unassignedPart {
			if _, at := t.cellOf(p, orig); at >= 0 && need[at] > 0 {
				need[at]--
				b.plan[orig] = append(b.plan[orig], p)
				continue
			}
		}
		row := t.rowOf(p)
		piles[row] = append(piles[row], p)
	}
	// A row's cells count exactly its partitions, so the piles run out
	// exactly as the cells fill.
	for i := range t.cells {
		c := &t.cells[i]
		b.plan[c.member] = append(b.plan[c.member], piles[c.row][:need[i]]...)
		piles[c.row] = piles[c.row][need[i]:]
	}
}
