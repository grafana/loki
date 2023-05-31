package sketch

import (
	"hash/fnv"

	"github.com/dustin/go-probably"
)

// Topk is a structure that uses a Count Min Sketch, Min-Heap, and slice of int64,
// the latter two of which have len(k), to track the top k events by frequency.
// The reason the last element is a slice is that we're likely getting the TopK
// where k is a small amount, say 10 or 100. In that case, traversing the slice to check
// for existence of a particular hash isn't that much more costly than doing so via a map
// and we avoid the extra memory overhead of using a map.
type Topk struct {
	max int
	// slice of the hashes for things we've seen, tracks
	currentTop []string
	heap       *MinHeap
	sketch     *probably.Sketch
}

func NewTopk(k int, numEntries int) (Topk, error) {
	// this is the amount we're okay with overcounting by in the sketch
	// for example; if a keys real value was 100 we might receive an estimate of 105
	// but if a keys real value is 0 we also might receive an estimate of 5
	diff := 5

	// we need to choose the sketch size
	// todo: derive the
	eps := float64(diff) / float64(numEntries/2)

	// we want a 99.9% probability of being within the range of exact value to exact value + diff
	s, err := NewCountMinSketch(eps, 0.999)
	if err != nil {
		return Topk{}, err
	}
	return Topk{
		max:        k,
		currentTop: make([]string, 0, k),
		heap:       NewMinHeap(k), //make heap,
		sketch:     s,
	}, nil
}

func (t *Topk) Observe(event string) {
	t.sketch.Add(event, 1)
	// todo: do we want to use our own sketch implementation, keep cast here, or change the heap types
	estimate := int64(t.sketch.Count(event))
	// check if the event is already in the topk, if it is we should update it's count
	if t.InTopk(event) {
		t.heap.UpdateValue(event)
		return
	}
	if len(t.currentTop) < t.heap.max {
		t.heap.Push(event, estimate)
		t.currentTop = append(t.currentTop, event)
		return
	}
	if estimate > t.heap.min().count {
		a := fnv.New64()
		a.Write([]byte(event))
		// remove the min event from the heap
		if len(t.currentTop) == t.max {
			min := t.heap.Pop()
			removeIndex := -1
			for i, e := range t.currentTop {
				if e == min.event {
					removeIndex = i
				}
			}
			// just to be safe, but this should never happen
			if removeIndex > -1 {
				t.currentTop[removeIndex] = t.currentTop[len(t.currentTop)-1]
				t.currentTop = t.currentTop[:len(t.currentTop)-1]
			}
		}

		insertEstimate := t.sketch.Count(event)
		// insert the new event onto the heap
		t.heap.Push(event, int64(insertEstimate))
		t.currentTop = append(t.currentTop, event)
	}
}

// InTopk checks to see if an event is already in the topk for this query
func (t *Topk) InTopk(event string) bool {
	// check for the thing
	for _, e := range t.currentTop {
		if e == event {
			return true
		}
	}
	return false
}

type TopKResult struct {
	Event string
	Count int64
}

func (t *Topk) Topk() []TopKResult {
	res := make([]TopKResult, len(t.currentTop))
	for i, e := range t.currentTop {
		res[i] = TopKResult{
			Event: e,
			Count: int64(t.sketch.Count(e)),
		}
	}
	return res
}
