package sketch

import (
	"sort"
)

// Topk is a structure that uses a Count Min Sketch, Min-Heap, and map of string -> uint32,
// the latter two of which have len(k), to track the top k events by frequency.
type Topk struct {
	max               int
	currentTop        map[string]uint32
	heap              MinHeap
	sketch            *CountMinSketch
	hashfuncs, rowlen int
}

func NewCMSTopK(k, w, d int) (Topk, error) {
	s, err := NewCountMinSketch(w, d)
	if err != nil {
		return Topk{}, nil
	}
	return Topk{
		max:        k,
		currentTop: make(map[string]uint32, k),
		heap:       MinHeap{},
		sketch:     s,
	}, nil
}

func (t *Topk) Observe(event string) {
	t.sketch.ConservativeIncrement(event)
	estimate := t.sketch.Count(event)

	// check if the event is already in the topk, if it is we should update it's count
	if t.InTopk(event) {
		t.heap.update(event, estimate)
		t.currentTop[event] = estimate
		return
	}

	if len(t.currentTop) < t.max {
		t.heap.Push(&node{event: event, count: estimate})
		t.currentTop[event] = estimate
		return
	}

	if estimate > t.heap.Peek().(*node).count {
		if len(t.currentTop) == t.max {
			min := t.heap.Pop().(*node)
			// todo: refactor so we have a nicer way of not calling pop if the
			// heap length is 0
			if min != nil {
				delete(t.currentTop, min.event)
			}
		}

		ele := node{event: event, count: estimate}
		t.heap.Push(&ele)
		t.currentTop[event] = estimate
	}
}

// InTopk checks to see if an event is already in the topk for this query
func (t *Topk) InTopk(event string) bool {
	_, ok := t.currentTop[event]
	return ok
}

type element struct {
	Event string
	Count int64
}

type TopKResult []element

func (t TopKResult) Len() int { return len(t) }

// for topk we actually want the largest item first
func (t TopKResult) Less(i, j int) bool { return t[i].Count > t[j].Count }
func (t TopKResult) Swap(i, j int)      { t[i], t[j] = t[j], t[i] }

func (t *Topk) Topk() TopKResult {
	n := t.max
	if len(t.currentTop) < t.max {
		n = len(t.currentTop)
	}
	res := make(TopKResult, 0, len(t.currentTop))
	for e := range t.currentTop {
		res = append(res, element{
			Event: e,
			Count: int64(t.sketch.Count(e)),
		})
	}
	sort.Sort(res)
	return res[:n]
}
