package v1

// MergeBlockQuerier is a heap implementation of BlockQuerier backed by multiple blocks
// It is used to merge multiple blocks into a single ordered querier
// NB(owen-d): it uses a custom heap implementation because Pop() only returns a single
// value of the top-most iterator, rather than the iterator itself
type MergeBlockQuerier struct {
	qs []PeekingIterator[*SeriesWithBloom]

	cache *SeriesWithBloom
	ok    bool
}

func NewMergeBlockQuerier(queriers ...PeekingIterator[*SeriesWithBloom]) *MergeBlockQuerier {
	res := &MergeBlockQuerier{
		qs: queriers,
	}
	res.init()
	return res
}

func (mbq MergeBlockQuerier) Len() int {
	return len(mbq.qs)
}

func (mbq *MergeBlockQuerier) Less(i, j int) bool {
	a, aOk := mbq.qs[i].Peek()
	b, bOk := mbq.qs[j].Peek()
	if !bOk {
		return true
	}
	if !aOk {
		return false
	}
	return a.Series.Fingerprint < b.Series.Fingerprint
}

func (mbq *MergeBlockQuerier) Swap(a, b int) {
	mbq.qs[a], mbq.qs[b] = mbq.qs[b], mbq.qs[a]
}

func (mbq *MergeBlockQuerier) Next() bool {
	mbq.cache = mbq.pop()
	return mbq.cache != nil
}

// TODO(owen-d): don't swallow this error
func (mbq *MergeBlockQuerier) Err() error {
	return nil
}

func (mbq *MergeBlockQuerier) At() *SeriesWithBloom {
	return mbq.cache
}

func (mbq *MergeBlockQuerier) push(x PeekingIterator[*SeriesWithBloom]) {
	mbq.qs = append(mbq.qs, x)
	mbq.up(mbq.Len() - 1)
}

func (mbq *MergeBlockQuerier) pop() *SeriesWithBloom {
	for {
		if mbq.Len() == 0 {
			return nil
		}

		cur := mbq.qs[0]
		if ok := cur.Next(); !ok {
			mbq.remove(0)
			continue
		}

		result := cur.At()

		_, ok := cur.Peek()
		if !ok {
			// that was the end of the iterator. remove it from the heap
			mbq.remove(0)
		} else {
			_ = mbq.down(0)
		}

		return result
	}
}

func (mbq *MergeBlockQuerier) remove(idx int) {
	mbq.Swap(idx, mbq.Len()-1)
	mbq.qs[len(mbq.qs)-1] = nil // don't leak reference
	mbq.qs = mbq.qs[:mbq.Len()-1]
	mbq.fix(idx)
}

// fix re-establishes the heap ordering after the element at index i has changed its value.
// Changing the value of the element at index i and then calling fix is equivalent to,
// but less expensive than, calling Remove(h, i) followed by a Push of the new value.
// The complexity is O(log n) where n = h.Len().
func (mbq *MergeBlockQuerier) fix(i int) {
	if !mbq.down(i) {
		mbq.up(i)
	}
}

func (mbq *MergeBlockQuerier) up(j int) {
	for {
		i := (j - 1) / 2 // parent
		if i == j || !mbq.Less(j, i) {
			break
		}
		mbq.Swap(i, j)
		j = i
	}
}

func (mbq *MergeBlockQuerier) down(i0 int) (moved bool) {
	i := i0
	n := mbq.Len()
	for {
		j1 := 2*i + 1
		if j1 >= n || j1 < 0 { // j1 < 0 after int overflow
			break
		}
		j := j1 // left child
		if j2 := j1 + 1; j2 < n && mbq.Less(j2, j1) {
			j = j2 // take the higher priority child index
		}
		if !mbq.Less(j, i) {
			break
		}
		mbq.Swap(i, j)
		i = j
	}

	return i > i0
}

// establish heap invariants. O(n)
func (mbq *MergeBlockQuerier) init() {
	n := mbq.Len()
	for i := n/2 - 1; i >= 0; i-- {
		_ = mbq.down(i)
	}
}
