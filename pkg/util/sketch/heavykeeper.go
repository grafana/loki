package sketch

import (
	"container/heap"
	"github.com/axiomhq/hyperloglog"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"math"
	"math/rand"
	"sort"
)

type heavyKeeper struct {
	// we could change this to a hash via the query trace ID as the seed, the loki stream fingerprint, etc
	fp    uint32
	count uint32
}

type HeavyKeeperTopK struct {
	k                   uint32
	decay               float64
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
		decay:  decay,
		k:      uint32(k),
		heap:   MinHeap{},
		sketch: sk,
		hll:    hyperloglog.New16(),
	}
}

func (t *HeavyKeeperTopK) Observe(event string) {
	t.hll.Insert([]byte(event))

	heapMin := uint32(0)
	if len(t.heap) > 0 {
		heapMin = t.heap.Peek().(*node).count
	}

	h1, h2 := hashn(event)
	maxCount := uint32(0)
	for i := uint32(0); int(i) < len(t.sketch); i++ {
		_, itemHeapExist := t.heap.Find(event)
		// there should be a better way to do this but at the moment we need to
		// update the min for each iteration in case we inserted or updated on the last round
		if len(t.heap) > 0 {
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
					//removed = true
					//removedEvent = t.sketch[i][pos].fp
					t.sketch[i][pos].fp = h1
					t.sketch[i][pos].count = 1

					if maxCount < 1 {
						maxCount = 1
					}
				}
			}
		}

		// update heap
		if itemHeapExist {
			t.heap.update(event, maxCount)
			continue
		}
		// item doesn't exist in heap
		// if we aren't already tracking the max # of things we can just add this event
		if len(t.heap) < int(t.k) {
			heap.Push(&t.heap, &node{
				event: event,
				count: maxCount,
			})
			continue
		}
		// otherwise, if the max count for this event is > heap min
		// we need to pop the top and add the new event
		if maxCount > heapMin {
			heap.Pop(&t.heap)
			heap.Push(&t.heap, &node{
				event: event,
				count: maxCount,
			})
		}
	}
}

// InTopk checks to see if an event is already in the topk for this query
func (t *HeavyKeeperTopK) InTopk(event string) bool {
	_, ok := t.heap.Find(event)
	return ok
}

func (t *HeavyKeeperTopK) query(event string) uint32 {
	max := uint32(0)
	h1, h2 := hashn(event)

	var pos uint32
	for i := 0; i < len(t.sketch); i++ {
		pos = (h1 + uint32(i)*h2) % uint32(len(t.sketch[0]))
		if t.sketch[i][pos].count > max {
			max = t.sketch[i][pos].count
		}
	}
	return max
}

func (t *HeavyKeeperTopK) Topk() TopKResult {
	res := make(TopKResult, 0, len(t.heap))
	for _, e := range t.heap {
		res = append(res, element{
			Event: e.event,
			Count: int64(e.count),
		})
	}
	//fmt.Println("len(heap): ", len(t.heap))
	//fmt.Println("sizeof HK sketch:", size.Of(t.sketch))
	//fmt.Println("sizeof heap: ", size.Of(t.heap))
	//fmt.Println("sizeof hll: ", size.Of(t.hll))
	sort.Sort(res)
	return res[:t.k]
}

// Merge the given sketch into this one.
// The sketches must have the same dimensions.
func (t *HeavyKeeperTopK) Merge(from *HeavyKeeperTopK) error {
	// merging via using the same logic as observe to decay or increment counters
	// or via this merge extension of the heap doesn't seem to be consistently
	// as accurate as if we'd just used a single HK sketch, AFAICT there is no official
	// proven way of merging HK sketches
	for _, e := range from.heap {
		if _, ok := t.heap.Find(e.event); ok {
			t.heap.update(e.event, from.query(e.event))
			continue
		}
		t.heap.Push(&node{
			event: e.event,
			count: from.query(e.event),
		})
	}

	return nil
}

// returns the estimated cardinality of the input plus whether the HK sketch size
// was big enough for that estimated cardinality.
func (t *HeavyKeeperTopK) Cardinality() (uint64, bool) {
	est := t.hll.Estimate()
	return t.hll.Estimate(), (est >= uint64(float64(t.expectedCardinality)*0.98)) && (est <= uint64(float64(t.expectedCardinality)*1.02))
}
