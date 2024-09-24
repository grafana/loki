package sketch

import (
	"fmt"
	"math"
)

type CountMinSketch struct {
	Depth, Width uint32
	Counters     [][]uint32
}

// NewCountMinSketch creates a new CMS for a given width and depth.
func NewCountMinSketch(w, d uint32) (*CountMinSketch, error) {
	return &CountMinSketch{
		Depth:    d,
		Width:    w,
		Counters: make2dslice(w, d),
	}, nil
}

func NewCountMinSketchFromErroAndProbability(epsilon float64, delta float64) (*CountMinSketch, error) {
	width := math.Ceil(math.E / epsilon)
	depth := math.Ceil(math.Log(1.0 / delta))
	return NewCountMinSketch(uint32(width), uint32(depth))
}

func make2dslice(col, row uint32) [][]uint32 {
	ret := make([][]uint32, row)
	for i := range ret {
		ret[i] = make([]uint32, col)
	}
	return ret
}

func (s *CountMinSketch) getPos(h1, h2, row uint32) uint32 {
	pos := (h1 + row*h2) % s.Width
	return pos
}

// Add 'count' occurrences of the given input.
func (s *CountMinSketch) Add(event string, count int) {
	// see the comments in the hashn function for how using only 2
	// hash functions rather than a function per row still fullfils
	// the pairwise indendent hash functions requirement for CMS
	h1, h2 := hashn(event)
	for i := uint32(0); i < s.Depth; i++ {
		pos := s.getPos(h1, h2, i)
		s.Counters[i][pos] += uint32(count)
	}
}

func (s *CountMinSketch) Increment(event string) {
	s.Add(event, 1)
}

// ConservativeAdd adds the count (conservatively) for the given input.
// Conservative counting is described in https://dl.acm.org/doi/pdf/10.1145/633025.633056
// and https://theory.stanford.edu/~matias/papers/sbf-sigmod-03.pdf. For more details you can read
// https://arxiv.org/pdf/2203.14549.pdf as well. The tl; dr, we only update the counters with a
// value that's less than Count(h) + count rather than all counters that h hashed to.
// Returns the new estimate for the event as well as the both hashes which can be used
// to identify the event for other things that need a hash.
func (s *CountMinSketch) ConservativeAdd(event string, count uint32) (uint32, uint32, uint32) {
	min := uint32(math.MaxUint32)

	h1, h2 := hashn(event)
	// inline Count to save time/memory
	var pos uint32
	for i := uint32(0); i < s.Depth; i++ {
		pos = s.getPos(h1, h2, i)
		if s.Counters[i][pos] < min {
			min = s.Counters[i][pos]
		}
	}
	min += count
	for i := uint32(0); i < s.Depth; i++ {
		pos = s.getPos(h1, h2, i)
		v := s.Counters[i][pos]
		if v < min {
			s.Counters[i][pos] = min
		}
	}
	return min, h1, h2
}

func (s *CountMinSketch) ConservativeIncrement(event string) (uint32, uint32, uint32) {
	return s.ConservativeAdd(event, 1)
}

// Count returns the approximate min count for the given input.
func (s *CountMinSketch) Count(event string) uint32 {
	min := uint32(math.MaxUint32)
	h1, h2 := hashn(event)

	var pos uint32
	for i := uint32(0); i < s.Depth; i++ {
		pos = s.getPos(h1, h2, i)
		if s.Counters[i][pos] < min {
			min = s.Counters[i][pos]
		}
	}
	return min
}

// Merge the given sketch into this one.
// The sketches must have the same dimensions.
func (s *CountMinSketch) Merge(from *CountMinSketch) error {
	if s.Depth != from.Depth || s.Width != from.Width {
		return fmt.Errorf("Can't merge different sketches with different dimensions")
	}

	for i, l := range from.Counters {
		for j, v := range l {
			s.Counters[i][j] += v
		}
	}
	return nil
}
