package sketch

import (
	"container/heap"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"math"
	"math/rand"
	"sort"

	"github.com/axiomhq/hyperloglog"
)

type heavyKeeper struct {
	// we could change this to a hash via the query trace ID as the seed, the loki stream fingerprint, etc
	fp    uint32
	count uint32
}

type HeavyKeeperTopK struct {
	k     uint32
	decay float64
	// string to refs
	currentTop map[string]uint32
	// refs to strings
	currentTopReverse   map[uint32]string
	heap                MinHeap
	sketch              [][]heavyKeeper
	hll                 *hyperloglog.Sketch
	expectedCardinality int
}

// get the correct sketch width based on the expected cardinality of the set
// we might need to do something smarter here to round up to next order of magnitude if we're say more than 10%
// over a given size that currently exists, or have some more intermediate sizes
func getHKWidth(l log.Logger, c int) int {
	// default to something reasonable for low cardinality
	width := 32
	switch {
	case c >= 1000001:
		if l != nil {
			level.Warn(l).Log("cardinality is greater than 1M but we don't currently have predefined sketch sizes for cardinalities that large")
		}
	case c >= 1000000:
		width = 307200
	case c >= 100000:
		width = 40960
	case c >= 10000:
		width = 4096
	case c >= 1000:
		width = 512
	case c >= 100:
		width = 48
	}
	return width
}

func NewHKForCardinality(l log.Logger, decay float64, k, c int) HeavyKeeperTopK {
	// a depth of > 4 didn't seem to make things siginificantly more accurate during testing
	w := getHKWidth(l, c)
	d := 4

	sk := NewHeavyKeeperTopK(decay, k, w, d)
	sk.expectedCardinality = c
	return sk
}

func NewHeavyKeeperTopK(decay float64, k, w, d int) HeavyKeeperTopK {
	sk := make([][]heavyKeeper, d)
	for i := 0; i < d; i++ {
		sk[i] = make([]heavyKeeper, w)
	}
	return HeavyKeeperTopK{
		decay:             decay,
		k:                 uint32(k),
		currentTop:        make(map[string]uint32, 2*k),
		currentTopReverse: make(map[uint32]string),
		heap:              MinHeap{},
		sketch:            sk,
		hll:               hyperloglog.New16(),
	}
}

func (t *HeavyKeeperTopK) Observe(event string) {
	t.hll.Insert([]byte(event))

	heapMin := uint32(0)
	if len(t.currentTop) > 0 {
		heapMin = t.heap.Peek().(*node).count
	}
	removed := false
	var removedEvent uint32
	h1, h2 := hashn(event)
	maxCount := uint32(0)
	for i := uint32(0); int(i) < len(t.sketch); i++ {
		_, itemHeapExist := t.heap.Find(event)
		// there should be a better way to do this but at the moment we need to
		// update the min for each iteration in case we inserted or updated on the last round
		if len(t.currentTop) > 0 {
			heapMin = t.heap.Peek().(*node).count
		}

		pos := (h1 + i*h2) % uint32(len(t.sketch[0]))

		fingerprint := t.sketch[i][pos].fp
		count := t.sketch[i][pos].count

		if count == 0 {
			t.sketch[i][pos].fp = h1
			t.sketch[i][pos].count = 1
			if maxCount < 1 {
				maxCount = 1
			}

		} else if fingerprint == h1 {
			if itemHeapExist || count <= heapMin {
				t.sketch[i][pos].count++
				if maxCount < t.sketch[i][pos].count {
					maxCount = t.sketch[i][pos].count
				}
			}

		} else {
			decay := math.Pow(t.decay, float64(count))
			if rand.Float64() < decay {
				t.sketch[i][pos].count--
				if t.sketch[i][pos].count == 0 {
					removed = true
					removedEvent = t.sketch[i][pos].fp
					t.sketch[i][pos].fp = h1
					t.sketch[i][pos].count = 1

					if maxCount < 1 {
						maxCount = 1
					}
				}
			}
		}

		if removed {
			delete(t.currentTop, t.currentTopReverse[h1])
			delete(t.currentTopReverse, removedEvent)
		}

		// update heap
		if itemHeapExist {
			t.heap.update(event, maxCount)
			t.currentTop[event] = h1
			t.currentTopReverse[h1] = event
			continue
		}
		// item doesn't exist in heap
		// if we aren't already tracking the max # of things we can just add this event
		if len(t.currentTop) < int(t.k) {
			heap.Push(&t.heap, &node{
				event: event,
				count: maxCount,
			})

			t.currentTop[event] = h1
			t.currentTopReverse[h1] = event
			continue
		}
		// otherwise, if the max count for this event is > heap min
		// we need to pop the top and add the new event
		if maxCount > heapMin {
			m := heap.Pop(&t.heap)
			delete(t.currentTop, m.(*node).event)
			heap.Push(&t.heap, &node{
				event: event,
				count: maxCount,
			})
			t.currentTop[event] = h1
			t.currentTopReverse[h1] = event
		}
	}
}

// InTopk checks to see if an event is already in the topk for this query
func (t *HeavyKeeperTopK) InTopk(event string) bool {
	_, ok := t.currentTop[event]
	return ok
}

func (t *HeavyKeeperTopK) Topk() TopKResult {
	res := make(TopKResult, 0, len(t.currentTop))
	for e, c := range t.currentTop {
		res = append(res, element{
			Event: e,
			Count: int64(c),
		})
	}
	sort.Sort(res)
	return res
}

// returns the estimated cardinality of the input plus whether the HK sketch size
// was big enough for that estimated cardinality.
func (t *HeavyKeeperTopK) Cardinality() (uint64, bool) {
	est := t.hll.Estimate()
	return t.hll.Estimate(), (est >= uint64(float64(t.expectedCardinality)*0.98)) && (est <= uint64(float64(t.expectedCardinality)*1.02))
}
