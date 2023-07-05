package sketch

import (
	"container/heap"
	"sort"

	"github.com/axiomhq/hyperloglog"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
)

// Topk is a structure that uses a Count Min Sketch, Min-Heap, and map of string -> uint32,
// the latter two of which have len(k), to track the top k events by frequency.
type Topk struct {
	max                 int
	heap                *MinHeap
	sketch              *CountMinSketch
	hashfuncs, rowlen   int
	hll                 *hyperloglog.Sketch
	expectedCardinality int
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
	case c >= 100000:
		width = 65536
	case c >= 10000:
		width = 5120
	case c >= 1000:
		width = 640
	case c >= 100:
		width = 48
	}
	return width
}

func NewCMSTopkForCardinality(l log.Logger, k, c int) (*Topk, error) {
	// a depth of > 4 didn't seem to make things siginificantly more accurate during testing
	w := getCMSWidth(l, c)
	d := 4

	sk, err := NewCMSTopK(k, w, d)
	if err != nil {
		return &Topk{}, err
	}
	sk.expectedCardinality = c
	return sk, nil
}

func NewCMSTopK(k, w, d int) (*Topk, error) {
	s, err := NewCountMinSketch(w, d)
	if err != nil {
		return &Topk{}, nil
	}
	return &Topk{
		max:    k,
		heap:   &MinHeap{},
		sketch: s,
		hll:    hyperloglog.New16(),
	}, nil
}

func (t *Topk) Observe(event string) {
	t.hll.Insert([]byte(event))
	t.sketch.ConservativeIncrement(event)
	estimate := t.sketch.Count(event)

	// check if the event is already in the topk, if it is we should update it's count
	if t.InTopk(event) {
		t.heap.update(event, estimate)
		return
	}

	if len(*t.heap) < t.max {
		heap.Push(t.heap, &node{event: event, count: estimate})
		return
	}

	if estimate > t.heap.Peek().(*node).count {
		if len(*t.heap) == t.max {
			_ = heap.Pop(t.heap).(*node)
		}
		ele := node{event: event, count: estimate}
		heap.Push(t.heap, &ele)
	}
}

func removeDuplicates(t TopKResult) TopKResult {
	processed := map[string]struct{}{}
	w := 0
	for _, e := range t {
		if _, exists := processed[e.Event]; !exists {
			// If this city has not been seen yet, add it to the list
			processed[e.Event] = struct{}{}
			t[w] = e
			w++
		}
	}
	return t[:w]
}

// Merge the given sketch into this one.
// The sketches must have the same dimensions.
func (t *Topk) Merge(from *Topk) error {
	err := t.sketch.Merge(from.sketch)
	if err != nil {
		return err
	}

	var all TopKResult
	for _, e := range *t.heap {
		all = append(all, element{Event: e.event, Count: int64(t.sketch.Count(e.event))})
	}

	for _, e := range *from.heap {
		all = append(all, element{Event: e.event, Count: int64(t.sketch.Count(e.event))})
	}
	all = removeDuplicates(all)
	sort.Sort(all)
	temp := &MinHeap{}
	for _, e := range all[:t.max] {
		heap.Push(temp, &node{event: e.Event, count: uint32(e.Count)})
	}
	t.heap = temp

	return nil
}

// InTopk checks to see if an event is already in the topk for this query
func (t *Topk) InTopk(event string) bool {
	_, ok := t.heap.Find(event)
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
	if len(*t.heap) < t.max {
		n = len(*t.heap)
	}
	res := make(TopKResult, 0, len(*t.heap))
	for _, e := range *t.heap {
		res = append(res, element{
			Event: e.event,
			Count: int64(t.sketch.Count(e.event)),
		})
	}
	sort.Sort(res)
	//fmt.Println("sizeof CMS sketch:", size.Of(t.sketch))
	//fmt.Println("sizeof heap: ", size.Of(t.heap))
	//fmt.Println("sizeof hll: ", size.Of(t.hll))
	return res[:n]
}

// returns the estimated cardinality of the input plus whether the HK sketch size
// was big enough for that estimated cardinality.
func (t *Topk) Cardinality() (uint64, bool) {
	est := t.hll.Estimate()
	return t.hll.Estimate(), (est >= uint64(float64(t.expectedCardinality)*0.98)) && (est <= uint64(float64(t.expectedCardinality)*1.02))
}
