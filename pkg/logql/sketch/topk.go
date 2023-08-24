package sketch

import (
	"container/heap"
	"fmt"
	"math"
	"reflect"
	"sort"
	"unsafe"

	"github.com/grafana/loki/pkg/logproto"

	"github.com/axiomhq/hyperloglog"
	"github.com/go-kit/log"
)

const DefaultGroupKey = "default_group"

type element struct {
	Event string
	Count float64
}

type TopKResult struct {
	groupingKey string
	Result      []element
}

func (e element) String() string {
	return fmt.Sprintf("{event: %s, count: %f}", e.Event, e.Count)
}

func (t TopKResult) Len() int { return len(t.Result) }

// for topk we actually want the largest item first
func (t TopKResult) Less(i, j int) bool { return t.Result[i].Count > t.Result[j].Count }
func (t TopKResult) Swap(i, j int)      { t.Result[i], t.Result[j] = t.Result[j], t.Result[i] }

// Topk is a structure that uses a Count Min Sketch and a Min-Heap to track the top k events by frequency.
// We also use the sketch-bf (https://ietresearch.onlinelibrary.wiley.com/doi/full/10.1049/ell2.12482) notion of a
// bloomfilter per count min sketch row to avoid having to iterate though the heap each time we want to check for
// existence of a given event (by identifier) in the heap.
type Topk struct {
	max int
	// the heap field is a map to allow for doing topk queries by groupingKey, like topk(10, query) by (endpoint)
	Heaps               map[string]*MinHeap
	sketch              *CountMinSketch
	bf                  [][]bool
	hll                 *hyperloglog.Sketch
	expectedCardinality int

	// save on allocs when converting from string event name to byte slice for hll insertion
	eventBytes []byte
}

func makeBF(col, row uint32) [][]bool {
	bf := make([][]bool, row)
	for i := range bf {
		bf[i] = make([]bool, col)
	}
	return bf
}

// NewCMSTopkForCardinality creates a new topk sketch where k is the amount of topk we want, and c is the expected
// total cardinality of the dataset the sketch should be able to handle, including other sketches that we may merge in.
func NewCMSTopkForCardinality(l log.Logger, k, c int) (*Topk, error) {
	epsilon := 0.001
	delta := 0.0001
	w := uint32(math.Ceil(math.E / epsilon))
	d := uint32(math.Ceil(math.Log(1 / delta)))

	sk, err := newCMSTopK(k, w, d)
	if err != nil {
		return &Topk{}, err
	}
	sk.expectedCardinality = c
	return sk, nil
}

// newCMSTopK creates a new topk sketch with a count min sketch of w by d dimensions, where w is the length of each row
// and d is the depth or # of rows. Remember that each row of a count min sketch increases the % chance of accuracy.
func newCMSTopK(k int, w, d uint32) (*Topk, error) {
	s, err := NewCountMinSketch(w, d)
	if err != nil {
		return &Topk{}, nil
	}
	return &Topk{
		max:    k,
		Heaps:  make(map[string]*MinHeap),
		sketch: s,
		hll:    hyperloglog.New16(),
		bf:     makeBF(w, d),
	}, nil
}

func TopkFromProto(t *logproto.TopK) (*Topk, error) {
	cms := &CountMinSketch{
		depth: t.Cms.Depth,
		width: t.Cms.Width,
	}
	for row := uint32(0); row < cms.depth; row++ {
		s := row * cms.width
		e := s + cms.width
		cms.counters = append(cms.counters, t.Cms.Counters[s:e])
	}

	hll := hyperloglog.New()
	err := hll.UnmarshalBinary(t.Hyperloglog)
	if err != nil {
		return nil, err
	}

	heaps := make(map[string]*MinHeap)
	for _, res := range t.Results {
		h := &MinHeap{}
		for _, e := range res.List {
			node := &Node{
				Event: e.Event,
				count: e.Count,
			}
			heap.Push(h, node)
		}

		heaps[res.Key] = h
	}

	// TODO(karsten): should we set expected cardinality as well?
	topk := &Topk{
		sketch: cms,
		hll:    hll,
		Heaps:  heaps,
	}
	return topk, nil
}

func (t *Topk) ToProto() (*logproto.TopK, error) {
	cms := &logproto.CountMinSketch{
		Depth: t.sketch.depth,
		Width: t.sketch.width,
	}
	cms.Counters = make([]float64, 0, cms.Depth*cms.Width)
	for row := uint32(0); row < cms.Depth; row++ {
		cms.Counters = append(cms.Counters, t.sketch.counters[row]...)
	}

	hllBytes, err := t.hll.MarshalBinary()
	if err != nil {
		return nil, err
	}

	list := make([]*logproto.TopK_Result, 0, len(t.Heaps))
	for key := range t.Heaps {
		res := make([]*logproto.TopK_Pair, 0, len(*t.Heaps[key]))
		for _, node := range *t.Heaps[key] {
			pair := &logproto.TopK_Pair{
				Event: node.Event,
				Count: node.count,
			}
			res = append(res, pair)
		}
		list = append(list, &logproto.TopK_Result{Key: key, List: res})
	}

	topk := &logproto.TopK{
		Cms:         cms,
		Hyperloglog: hllBytes,
		Results:     list,
	}
	return topk, nil
}

// wrapper to bundle together updating of the bf portion of the sketch and pushing of a new element
// to the heap
func (t *Topk) heapPush(h *MinHeap, event string, estimate float64, h1, h2 uint32) {
	var pos uint32
	for i := range t.bf {
		pos = t.sketch.getPos(h1, h2, uint32(i))
		t.bf[i][pos] = true
	}
	heap.Push(h, &Node{Event: event, count: estimate})
}

// wrapper to bundle together updating of the bf portion of the sketch for the removed and added event
// as well as replacing the min heap element with the new event and it's count
func (t *Topk) heapMinReplace(event, groupingKey string, estimate float64, removed string) {
	t.updateBF(removed, event)
	(*t.Heaps[groupingKey])[0].Event = event
	(*t.Heaps[groupingKey])[0].count = estimate
	heap.Fix(t.Heaps[groupingKey], 0)
}

// updates the BF to ensure that the removed event won't be mistakenly thought
// to be in the heap, and updates the BF to ensure that we would get a truthy result for the added event
func (t *Topk) updateBF(removed, added string) {
	r1, r2 := hashn(removed)
	a1, a2 := hashn(added)
	var pos uint32
	for i := range t.bf {
		// removed event
		pos = t.sketch.getPos(r1, r2, uint32(i))
		t.bf[i][pos] = false
		// added event
		pos = t.sketch.getPos(a1, a2, uint32(i))
		t.bf[i][pos] = true
	}
}

// todo: is there a way to save more bytes/allocs via a pool?
func unsafeGetBytes(s string) []byte {
	if s == "" {
		return nil // or []byte{}
	}
	return (*[0x7fff0000]byte)(unsafe.Pointer(
		(*reflect.StringHeader)(unsafe.Pointer(&s)).Data),
	)[:len(s):len(s)]
}

// ObserveForGroupingKey is our sketch event observation function, which is a bit more complex than the original count min sketch + heap TopK
// literature outlines. We're using some optimizations from the sketch-bf paper (here: http://www.eecs.harvard.edu/~michaelm/postscripts/tr-02-05.pdf)
// in order to reduce the # of heap operations required over time. As an example, with a cardinality of 100k we saw nearly 3x improvement
// in CPU usage by using these optimizations.
//
// By default when we observe an event, if it's already in the current topk we would update it's value in the heap structure
// with the new count min sketch estimate and then rebalance the heap. This is potentially a lot of heap balancing operations
// that, at the end of the day, aren't really important. What information do we care about from the heap when we're actually
// still observing events and tracking the topk? The minimum value that's stored in the heap. If we observe an event and it's
// new count is greater than the minimum value in the heap, that event should go into the heap and the event with the minimum
// value should come out. So the optimization is as follows:
//
// We only need to update the count for each event in the heap when we observe an event that's not in the heap, and it's
// new estimate is greater than the thing that's the current minimum value heap element. At that point, we update the values
// for each node in the heap and rebalance the heap, and then if the event we're observing has an estimate that is still
// greater than the minimum heap element count, we should put this event into the heap and remove the other one.
func (t *Topk) ObserveForGroupingKey(event, groupingKey string, count float64) {
	estimate, h1, h2 := t.sketch.ConservativeAdd(event, count)
	t.hll.Insert(unsafeGetBytes(event))

	if t.Heaps[groupingKey] == nil {
		t.Heaps[groupingKey] = &MinHeap{}
	}

	if t.InTopk(h1, h2) {
		return
	}

	if len(*t.Heaps[groupingKey]) < t.max {
		t.heapPush(t.Heaps[groupingKey], event, estimate, h1, h2)
		return
	}

	if estimate > t.Heaps[groupingKey].Peek().(*Node).count {
		var h1, h2 uint32
		var pos uint32
		for i := range *t.Heaps[groupingKey] {
			(*t.Heaps[groupingKey])[i].count = t.sketch.Count((*t.Heaps[groupingKey])[i].Event)
			if i <= len(*t.Heaps[groupingKey])/2 {
				heap.Fix(t.Heaps[groupingKey], i)
			}
			// ensure all the bf buckets are truthy for the event
			h1, h2 = hashn((*t.Heaps[groupingKey])[i].Event)
			for j := range t.bf {
				pos = t.sketch.getPos(h1, h2, uint32(j))
				t.bf[j][pos] = true
			}
		}
	}

	// double check if the event is already in the topk
	if t.InTopk(h1, h2) {
		return
	}

	// after we've updated the heap, if the estimate is still > the min heap element
	// we should replace the min heap element with the current event and rebalance the heap
	if estimate > t.Heaps[groupingKey].Peek().(*Node).count {
		if len(*t.Heaps[groupingKey]) == t.max {
			e := t.Heaps[groupingKey].Peek().(*Node).Event
			//r1, r2 := hashn(e)
			t.heapMinReplace(event, groupingKey, estimate, e)
			return
		}
		t.heapPush(t.Heaps[groupingKey], event, estimate, h1, h2)
	}
}

// Observe should only be used when there is no grouping key present for the overall query
func (t *Topk) Observe(event string, count float64) {
	t.ObserveForGroupingKey(event, DefaultGroupKey, count)
}

func removeDuplicates(t TopKResult) TopKResult {
	processed := map[string]struct{}{}
	w := 0
	for _, e := range t.Result {
		if _, exists := processed[e.Event]; !exists {
			// If this city has not been seen yet, add it to the list
			processed[e.Event] = struct{}{}
			t.Result[w] = e
			w++
		}
	}
	t.Result = t.Result[:w]
	return t
}

// Merge the given sketch into this one.
// The sketches must have the same dimensions.
// Note that our merge operation currently also replaces the heap
// by taking the combined topk list from both t and from and then deduplicating
// the union of the two, and finally pushing that list of things to a new heap
func (t *Topk) Merge(from *Topk) error {
	err := t.sketch.Merge(from.sketch)
	if err != nil {
		return err
	}

	var all TopKResult
	processed := make(map[string]struct{})
	for key := range t.Heaps {
		processed[key] = struct{}{}
		if _, ok := from.Heaps[key]; !ok {
			continue
		}
		all.Result = all.Result[:0]
		all.groupingKey = key
		for _, e := range *t.Heaps[key] {
			all.Result = append(all.Result, element{Event: e.Event, Count: t.sketch.Count(e.Event)})
		}

		for _, e := range *from.Heaps[key] {
			all.Result = append(all.Result, element{Event: e.Event, Count: t.sketch.Count(e.Event)})
		}

		all = removeDuplicates(all)
		sort.Sort(all)
		temp := &MinHeap{}
		var h1, h2 uint32
		// TODO: merging should also potentially replace it's bloomfilter? or 0 everything in the bloomfilter
		for _, e := range all.Result[:t.max] {
			h1, h2 = hashn(e.Event)
			t.heapPush(temp, e.Event, e.Count, h1, h2)
		}
		t.Heaps[key] = temp
	}

	for key := range from.Heaps {
		if _, ok := processed[key]; ok {
			continue
		}
		t.Heaps[key] = from.Heaps[key]
	}

	return nil
}

// InTopk checks to see if an event is currently in the topk for this sketch
func (t *Topk) InTopk(h1, h2 uint32) bool {
	ret := true
	var pos uint32
	for i := range t.bf {
		pos = t.sketch.getPos(h1, h2, uint32(i))
		if !t.bf[i][pos] {
			ret = false
		}
	}
	return ret
}

func (t *Topk) Topk() []TopKResult {
	res := make([]TopKResult, 0, len(t.Heaps))
	//i := 0
	for key := range t.Heaps {
		n := t.max

		keyRes := TopKResult{
			groupingKey: key,
			Result:      make([]element, 0, len(*t.Heaps[key])),
		}
		if len(*t.Heaps[key]) < t.max {
			n = len(*t.Heaps[key])
		}
		for _, e := range *t.Heaps[key] {
			keyRes.Result = append(keyRes.Result, element{
				Event: e.Event,
				Count: t.sketch.Count(e.Event),
			})
		}
		sort.Sort(keyRes)
		keyRes.Result = keyRes.Result[:n]
		res = append(res, keyRes)
	}

	return res
}

func (t *Topk) TopkForGroupingKey(groupingKey string) TopKResult {
	n := t.max
	if len(*t.Heaps[groupingKey]) < t.max {
		n = len(*t.Heaps[groupingKey])
	}
	res := TopKResult{
		groupingKey: "",
		Result:      make([]element, 0, len(*t.Heaps[groupingKey])),
	}

	for _, e := range *t.Heaps[groupingKey] {
		res.Result = append(res.Result, element{
			Event: e.Event,
			Count: t.sketch.Count(e.Event),
		})
	}
	sort.Sort(res)
	res.Result = res.Result[:n]
	return res
}

func (t *Topk) GroupingKeys() []string {
	keys := make([]string, 0, len(t.Heaps))
	for key := range t.Heaps {
		keys = append(keys, key)
	}
	return keys
}

// DistinctEventsForGroupingKey returns a list of distinct events observed.
// Returns nil if group was not found.
func (t *Topk) DistinctEventsForGroupingKey(groupingKey string) []string {
	group, ok := t.Heaps[groupingKey]
	if !ok {
		return nil
	}

	events := make([]string, 0, len(*group))
	for _, node := range *group {
		events = append(events, node.Event)
	}
	return events
}

// Cardinality returns the estimated cardinality of the input plus whether the size of t's
// count min sketch was big enough for that estimated cardinality.
func (t *Topk) Cardinality() (uint64, bool) {
	est := t.hll.Estimate()
	// hll estimate has an overcounting error % of 2%
	return t.hll.Estimate(), est <= uint64(float64(t.expectedCardinality)*1.02)
}
