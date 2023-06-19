package sketch

import (
	"fmt"
	"math"
)

type CountMinSketch struct {
	depth, width int
	counters     [][]uint32
}

// NewCountMinSketch creates a new CMS for a given width and depth.
func NewCountMinSketch(w, d int) (*CountMinSketch, error) {
	if d < 1 || w < 1 {
		return nil, fmt.Errorf("sketch dimensions must be positive, w: %d, d: %d", w, d)
	}

	return &CountMinSketch{
		depth:    d,
		width:    w,
		counters: make2dslice(w, d),
	}, nil
}

func make2dslice(col, row int) [][]uint32 {
	ret := make([][]uint32, row)
	for i := range ret {
		ret[i] = make([]uint32, col)
	}
	return ret
}

func (s *CountMinSketch) getPos(h1, h2 uint32, row int) uint32 {
	pos := (h1 + uint32(row)*h2) % uint32(s.width)
	return pos
}

// Add 'count' occurences of the given input.
func (s *CountMinSketch) Add(event string, count int) {
	// see the comments in the hashn function for how using only 2
	// hash functions rather than a function per row still fullfils
	// the pairwise indendent hash functions requirement for CMS
	h1, h2 := hashn(event)
	for i := 0; i < s.depth; i++ {
		pos := s.getPos(h1, h2, i)
		s.counters[i][pos] += uint32(count)
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
func (s *CountMinSketch) ConservativeAdd(event string, count uint32) uint32 {
	val := s.Count(event)
	val += count

	h1, h2 := hashn(event)
	for i := 0; i < s.depth; i++ {
		pos := s.getPos(h1, h2, i)
		v := s.counters[i][pos]
		if v < val {
			s.counters[i][pos] = val
		}
	}
	return val
}

func (s *CountMinSketch) ConservativeIncrement(event string) {
	s.ConservativeAdd(event, 1)
}

// Count returns the approximate min count for the given input.
func (s *CountMinSketch) Count(event string) uint32 {
	min := uint32(math.MaxUint32)
	h1, h2 := hashn(event)

	var pos uint32
	for i := 0; i < s.depth; i++ {
		pos = s.getPos(h1, h2, i)
		if s.counters[i][pos] < min {
			min = s.counters[i][pos]
		}
	}
	return min
}
