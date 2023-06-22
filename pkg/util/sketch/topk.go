package sketch

import (
	"container/heap"
	"fmt"
	"sort"

	"github.com/axiomhq/hyperloglog"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
)

// Topk is a structure that uses a Count Min Sketch, Min-Heap, and map of string -> uint32,
// the latter two of which have len(k), to track the top k events by frequency.
type Topk struct {
	max               int
	currentTop        map[string]uint32
	heap              MinHeap
	sketch            *CountMinSketch
	hashfuncs, rowlen int
	hll               *hyperloglog.Sketch
}

// get the correct sketch width based on the expected cardinality of the set
// we might need to do something smarter here to round up to next order of magnitude if we're say more than 10%
// over a given size that currently exists, or have some more intermediate sizes
func getCMSWidth(l log.Logger, c int) int {
	// default to something reasonable for low cardinality
	width := 32
	switch {
	case c >= 1000001:
		if l != nil {
			level.Warn(l).Log("cardinality is greater than 1M but we don't currently have predefined sketch sizes for cardinalities that large")
		}
	case c >= 1000000:
		width = 409600
		fmt.Println("creating for 5")

	case c >= 100000:
		width = 65536
		fmt.Println("creating for 4")

	case c >= 10000:
		width = 5120
		fmt.Println("creating for 3")

	case c >= 1000:
		width = 640
		fmt.Println("creating for 2")

	case c >= 100:
		width = 48

		fmt.Println("creating for 1")
	}
	return width
}

func NewCMSTopkForCardinality(l log.Logger, decay float64, k, c int) HeavyKeeperTopK {
	// a depth of > 4 didn't seem to make things siginificantly more accurate during testing
	w := getCMSWidth(l, c)
	d := 4

	sk := NewHeavyKeeperTopK(decay, k, w, d)
	sk.expectedCardinality = c
	return sk
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
		hll:        hyperloglog.New16(),
	}, nil
}

func (t *Topk) Observe(event string) {
	t.hll.Insert([]byte(event))
	t.sketch.ConservativeIncrement(event)
	estimate := t.sketch.Count(event)

	// check if the event is already in the topk, if it is we should update it's count
	if t.InTopk(event) {
		t.heap.update(event, estimate)
		t.currentTop[event] = estimate
		return
	}

	if len(t.currentTop) < t.max {
		heap.Push(&t.heap, &node{event: event, count: estimate})
		t.currentTop[event] = estimate
		return
	}

	if estimate > t.heap.Peek().(*node).count {
		if len(t.currentTop) == t.max {
			min := heap.Pop(&t.heap).(*node)
			// todo: refactor so we have a nicer way of not calling pop if the
			// heap length is 0
			if min != nil {
				delete(t.currentTop, min.event)
			}
		}

		ele := node{event: event, count: estimate}
		heap.Push(&t.heap, &ele)
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

func (t *Topk) Cardinality() uint64 {
	return t.hll.Estimate()
}
