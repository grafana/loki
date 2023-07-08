package sketch

import (
	"container/heap"
	"fmt"
	"github.com/axiomhq/hyperloglog"
	"math"
	"sort"
)

// SketchBF is an extension of CMS + heap topk implementations
// it uses an additional "bloom filter" of bits to know whether
// a given thing is already part of the heap
type SketchBF struct {
	max                 int
	heap                MinHeap
	hll                 *hyperloglog.Sketch
	expectedCardinality int

	// sketch portion
	depth, width   int
	cms            *CountMinSketch
	bf             [][]bool
	eventPositions []uint32
}

func NewSketchBFForCardinality(k, c int) (*SketchBF, error) {
	// a depth of > 4 didn't seem to make things siginificantly more accurate during testing
	w := getCMSWidth(nil, c)
	d := 4

	sk, err := NewSketchBF(k, w, d)
	if err != nil {
		return &SketchBF{}, err
	}
	sk.expectedCardinality = c
	return sk, nil
}

// NewSketchBF creates a new CMS for a given width and depth.
func NewSketchBF(k, w, d int) (*SketchBF, error) {
	if d < 1 || w < 1 {
		return nil, fmt.Errorf("sketch dimensions must be positive, w: %d, d: %d", w, d)
	}
	cms, _ := NewCountMinSketch(w, d)

	return &SketchBF{
		max:            k,
		depth:          d,
		width:          w,
		heap:           MinHeap{},
		cms:            cms,
		bf:             makeBF(w, d),
		hll:            hyperloglog.New16(),
		eventPositions: make([]uint32, d),
	}, nil
}

func makeBF(col, row int) [][]bool {
	bf := make([][]bool, row)
	for i := range bf {
		bf[i] = make([]bool, col)
	}
	return bf
}

func (s *SketchBF) getPos(h1, h2 uint32, row int) uint32 {
	pos := (h1 + uint32(row)*h2) % uint32(s.width)
	return pos
}

func (s *SketchBF) Observe(event string) {
	// avoid calling count so that we can save the positions in each row for this event
	h1, h2 := hashn(event)
	val := uint32(math.MaxUint32)
	var pos uint32
	for i := 0; i < s.depth; i++ {
		pos = s.getPos(h1, h2, i)
		s.eventPositions[i] = pos
		if s.cms.counters[i][pos] < val {
			val = s.cms.counters[i][pos]
		}

	}
	val++
	ok := true
	s.hll.InsertHash(uint64(h1))
	// update the counter in the CMS but also check all the
	// bloom filter counters to see if the event is already in the heap
	for i := 0; i < s.depth; i++ {

		pos = s.eventPositions[i]
		if s.cms.counters[i][pos] < val {
			s.cms.counters[i][pos] = val
		}
		if s.bf[i][pos] != true {
			ok = false
		}
	}

	// avoid updating the heap, we don't need to
	if ok {
		return
	}

	// if the value is not greater than the current heap min we can continue
	if len(s.heap) > 0 && val < s.heap.Peek().(*node).count {
		return
	}

	// update the heap and bloom filter values
	oMin := uint32(math.MaxUint32)
	oMinIdx := 0
	for i := 0; i < len(s.heap); i++ {
		min := uint32(math.MaxUint32)
		for r := range s.cms.counters {
			pos = s.heap[i].sketchPositions[r]
			s.bf[r][pos] = true
			if s.cms.counters[r][pos] < min {
				min = s.cms.counters[r][pos]
			}
			if s.cms.counters[r][pos] < oMin {
				oMin = s.cms.counters[r][pos]
				oMinIdx = i
			}
		}
		s.heap[i].count = min
	}
	heap.Fix(&s.heap, oMinIdx)

	// check if the event is in the heap now
	ok = true
	for i := 0; i < s.depth; i++ {
		pos = s.eventPositions[i]
		if s.bf[i][pos] != true {
			ok = false
		}
	}
	if ok {
		return
	}

	if s.heap.Len() < s.max {
		n := &node{event: event, count: val}
		n.sketchPositions = make([]uint32, len(s.eventPositions))
		copy(n.sketchPositions, s.eventPositions)
		heap.Push(&s.heap, n)
		for r := range s.bf {
			pos = s.eventPositions[r]
			s.bf[r][pos] = true
		}
		return
	}

	// if the estimate is still greater than the updated min, we can do the dance
	if val > s.heap.Peek().(*node).count {
		var r1, r2 uint32
		removed := false
		var removedEvent string
		if len(s.heap) == s.max {
			e := heap.Pop(&s.heap).(*node)
			removedEvent = e.event
			// we need to update the bf for the thing we popped
			removed = true
		}

		ele := node{event: event, count: val}
		ele.sketchPositions = make([]uint32, len(s.eventPositions))
		copy(ele.sketchPositions, s.eventPositions)
		heap.Push(&s.heap, &ele)
		if removed {
			r1, r2 = hashn(removedEvent)
		}
		var rPos uint32
		for r := range s.bf {
			// do the removed position first just incase the new added
			// position overlaps
			if removed {
				rPos = s.getPos(r1, r2, r)
				s.bf[r][rPos] = false
			}
			pos = s.eventPositions[r]
			s.bf[r][pos] = true
		}
	}
}

// Merge the given sketch into this one.
// The sketches must have the same dimensions.
func (s *SketchBF) Merge(from *CountMinSketch) error {
	if s.depth != from.depth || s.width != from.width {
		return fmt.Errorf("Can't merge different sketches with different dimensions")
	}

	s.cms.Merge(from)
	return nil
}

func (s *SketchBF) Topk() TopKResult {
	n := s.max
	if len(s.heap) < s.max {
		n = len(s.heap)
	}
	res := make(TopKResult, 0, len(s.heap))
	for _, e := range s.heap {
		res = append(res, element{
			Event: e.event,
			Count: int64(s.cms.Count(e.event)),
		})
	}
	sort.Sort(res)
	return res[:n]
}
