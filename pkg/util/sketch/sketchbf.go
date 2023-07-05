package sketch

import (
	"container/heap"
	"fmt"
	"github.com/axiomhq/hyperloglog"
	"sort"
)

// SketchBF is an extension of CMS + heap topk implementations
// it uses an additional "bloom filter" of bits to know whether
// a given thing is already part of the heap
type SketchBF struct {
	max int
	//currentTop          map[string]uint32
	heap                MinHeap
	hll                 *hyperloglog.Sketch
	expectedCardinality int

	// sketch portion
	depth, width int
	cms          *CountMinSketch
	bf           [][]bool
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
	//return &SketchBF{
	//	depth: d,
	//	width: w,
	//
	//}, nil

	return &SketchBF{
		max:   k,
		depth: d,
		width: w,
		heap:  MinHeap{},
		cms:   cms,
		bf:    makeBF(w, d),
		hll:   hyperloglog.New16(),
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

//// Add 'count' occurences of the given input.
//func (s *SketchBF) add(event string, count int) {
//	// see the comments in the hashn function for how using only 2
//	// hash functions rather than a function per row still fullfils
//	// the pairwise indendent hash functions requirement for CMS
//	h1, h2 := hashn(event)
//	for i := 0; i < s.depth; i++ {
//		pos := s.getPos(h1, h2, i)
//		s.counters[i][pos] += uint32(count)
//	}
//}

//func (s *SketchBF) increment(event string) {
//	s.add(event, 1)
//}

// conservativeAdd adds the count (conservatively) for the given input.
// Conservative counting is described in https://dl.acm.org/doi/pdf/10.1145/633025.633056
// and https://theory.stanford.edu/~matias/papers/sbf-sigmod-03.pdf. For more details you can read
// https://arxiv.org/pdf/2203.14549.pdf as well. The tl; dr, we only update the counters with a
// value that's less than Count(h) + count rather than all counters that h hashed to.
//func (s *SketchBF) conservativeAdd(event string, count uint32) uint32 {
//	val := s.count(event)
//	val += count
//
//	h1, h2 := hashn(event)
//	for i := 0; i < s.depth; i++ {
//		pos := s.getPos(h1, h2, i)
//		v := s.counters[i][pos]
//		if v < val {
//			s.counters[i][pos] = val
//		}
//	}
//	return val
//}
//
//func (s *SketchBF) conservativeIncrement(event string) {
//	s.conservativeAdd(event, 1)
//}
//
//// Count returns the approximate min count for the given input.
//func (s *SketchBF) count(event string) uint32 {
//	min := uint32(math.MaxUint32)
//	h1, h2 := hashn(event)
//
//	var pos uint32
//	for i := 0; i < s.depth; i++ {
//		pos = s.getPos(h1, h2, i)
//		if s.counters[i][pos] < min {
//			min = s.counters[i][pos]
//		}
//	}
//	return min
//}

func (s *SketchBF) Observe(event string) {
	s.hll.Insert([]byte(event))
	//estimate := s.cms.ConservativeIncrement(event)
	//estimate := s.cms.Count(event)

	// check if the event is already in the topk, if it is we should update it's count
	// loop copied from cms conservative add
	val := s.cms.Count(event)
	val++

	ok := true
	h1, h2 := hashn(event)
	// update the counter in the CMS but also check all the
	// bloom filter counters to see if the event is already in the heap
	for i := 0; i < s.depth; i++ {
		pos := s.getPos(h1, h2, i)
		v := s.cms.counters[i][pos]
		if v < val {
			s.cms.counters[i][pos] = val
		}
		if s.bf[i][pos] != true {
			ok = false
		}
	}
	estimate := val
	//return val
	//h1, h2 := hashn(event)
	//for r := range s.bf {
	//	pos := s.getPos(h1, h2, r)
	//	if s.bf[r][pos] != true {
	//		ok = false
	//		break
	//	}
	//}

	// avoid updating the heap, we don't need to
	if ok {
		return
	}

	// update the heap and bloom filter values
	for i := 0; i < len(s.heap); i++ {
		e := s.heap[i].event
		s.heap[i].count = s.cms.Count(e)
		updateH1, updateH2 := hashn(e)
		for r := range s.bf {
			pos := s.getPos(updateH1, updateH2, r)
			s.bf[r][pos] = true
		}
		//heap.Fix(&s.heap, i)
	}

	// reheapify
	for i := s.heap.Len() / 2; i > 0; i-- {
		e := s.heap[i].event
		s.heap[i].count = s.cms.Count(e)
		heap.Fix(&s.heap, i)
	}

	// check if the event is in the heap now
	ok = true
	//h1, h2 := hashn(event)
	// update the counter in the CMS but also check all the
	// bloom filter counters to see if the event is already in the heap
	for i := 0; i < s.depth; i++ {
		pos := s.getPos(h1, h2, i)
		if s.bf[i][pos] != true {
			ok = false
		}
	}
	if ok {
		//fmt.pr	/
		return
	}
	//estimate := val

	// when we get here, the event is not in the heap currently

	//if estimate < s.heap.Peek().(*node).count {
	//	return
	//}

	if s.heap.Len() < s.max {
		heap.Push(&s.heap, &node{event: event, count: estimate})
		for r := range s.bf {
			pos := s.getPos(h1, h2, r)
			s.bf[r][pos] = true
		}
		//t.currentTop[event] = estimate
		return
	}

	//// do an initial check if the estimate is > the heaps min value
	//if estimate > s.heap.Peek().(*node).count {
	//	// lets ensure all the values of the events in the heap are up to date
	//	for i := 0; i < len(s.heap); i++ {
	//		e := s.heap[i].event
	//		s.heap[i].count = s.cms.Count(e)
	//		heap.Fix(&s.heap, i)
	//		r1, r2 := hashn(e)
	//
	//		for r := range s.bf {
	//			pos := s.getPos(r1, r2, r)
	//			s.bf[r][pos] = true
	//		}
	//		// the paper also updates all the bf counters here
	//		// to be 1, but they should already be? not necessarily if
	//		// we popped something from the heap that had a collision with
	//		// this event
	//
	//	}
	//}

	// if the estimate is still greater than the updated min, we can do the dance
	if estimate > s.heap.Peek().(*node).count {
		var r1, r2 uint32
		removed := false
		var removedEvent string
		if len(s.heap) == s.max {
			e := heap.Pop(&s.heap).(*node)
			removedEvent = e.event
			// we need to update the bf for the thing we popped
			//r1, r2 = hashn(e.event)
			removed = true
			//for r := range s.bf {
			//	pos := s.getPos(r1, r2, r)
			//	s.bf[r][pos] = false
			//}
			// todo: refactor so we have a nicer way of not calling pop if the
			// heap length is 0
			//if min != nil {
			//	delete(t.currentTop, min.event)
			//}
		}

		ele := node{event: event, count: estimate}
		heap.Push(&s.heap, &ele)
		if removed {
			r1, r2 = hashn(removedEvent)
		}
		for r := range s.bf {
			// do the removed position first just incase the new added
			// position overlaps
			if removed {
				rPos := s.getPos(r1, r2, r)
				s.bf[r][rPos] = false
			}
			pos := s.getPos(h1, h2, r)
			s.bf[r][pos] = true
		}
		//t.currentTop[event] = estimate
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
