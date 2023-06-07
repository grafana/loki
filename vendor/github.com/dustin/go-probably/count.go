package probably

import (
	"fmt"
	"hash/fnv"
	"math"
	"sort"
)

// Sketch is a count-min sketcher.
type Sketch struct {
	sk        [][]uint32
	rowCounts []uint32
}

// NewSketch returns new count-min sketch with the given width and depth.
// Sketch dimensions must be positive.  A sketch with w=⌈ ℯ/𝜀 ⌉ and
// d=⌈ln (1/𝛿)⌉ answers queries within a factor of 𝜀 with probability 1-𝛿.
func NewSketch(w, d int) *Sketch {
	if d < 1 || w < 1 {
		panic("Dimensions must be positive")
	}

	s := &Sketch{}

	s.sk = make([][]uint32, d)
	for i := 0; i < d; i++ {
		s.sk[i] = make([]uint32, w)
	}

	s.rowCounts = make([]uint32, d)

	return s
}

func (s Sketch) String() string {
	return fmt.Sprintf("{Sketch %dx%d}", len(s.sk[0]), len(s.sk))
}

func hashn(s string) (h1, h2 uint32) {

	// This construction comes from
	// http://www.eecs.harvard.edu/~michaelm/postscripts/tr-02-05.pdf
	// "Building a Better Bloom Filter", by Kirsch and Mitzenmacher. Their
	// proof that this is allowed for count-min requires the h functions to
	// be from the 2-universal hash family, w be a prime and d be larger
	// than the traditional CM-sketch requirements.

	// Empirically, though, this seems to work "just fine".

	// TODO(dgryski): Switch to something that is actually validated by the literature.

	fnv1a := fnv.New32a()
	fnv1a.Write([]byte(s))
	h1 = fnv1a.Sum32()

	// inlined jenkins one-at-a-time hash
	h2 = uint32(0)
	for _, c := range s {
		h2 += uint32(c)
		h2 += h2 << 10
		h2 ^= h2 >> 6
	}
	h2 += (h2 << 3)
	h2 ^= (h2 >> 11)
	h2 += (h2 << 15)

	return h1, h2
}

// Reset clears all the values from the sketch.
func (s *Sketch) Reset() {

	// Compiler doesn't yet optimize this into memset: https://code.google.com/p/go/issues/detail?id=5373
	for _, w := range s.sk {
		for i := range w {
			w[i] = 0
		}
	}

	for i := range s.rowCounts {
		s.rowCounts[i] = 0
	}
}

// Add 'count' occurrences of the given input
func (s *Sketch) Add(h string, count uint32) (val uint32) {
	w := len(s.sk[0])
	d := len(s.sk)
	val = math.MaxUint32
	h1, h2 := hashn(h)
	for i := 0; i < d; i++ {
		pos := (h1 + uint32(i)*h2) % uint32(w)
		s.rowCounts[i] += count
		v := s.sk[i][pos] + count
		s.sk[i][pos] = v
		if v < val {
			val = v
		}
	}
	return val
}

// Del removes 'count' occurrences of the given input
func (s *Sketch) Del(h string, count uint32) (val uint32) {
	w := len(s.sk[0])
	d := len(s.sk)
	val = math.MaxUint32
	h1, h2 := hashn(h)
	for i := 0; i < d; i++ {
		pos := (h1 + uint32(i)*h2) % uint32(w)
		s.rowCounts[i] -= count
		v := s.sk[i][pos] - count
		if v > s.sk[i][pos] { // did we wrap-around?
			v = 0
		}
		s.sk[i][pos] = v
		if v < val {
			val = v
		}
	}
	return val
}

// Increment the count for the given input.
func (s *Sketch) Increment(h string) (val uint32) {
	return s.Add(h, 1)
}

// ConservativeIncrement increments the count (conservatively) for the given input.
func (s *Sketch) ConservativeIncrement(h string) (val uint32) {
	return s.ConservativeAdd(h, 1)
}

// ConservativeAdd adds the count (conservatively) for the given input.
func (s *Sketch) ConservativeAdd(h string, count uint32) (val uint32) {
	w := len(s.sk[0])
	d := len(s.sk)
	h1, h2 := hashn(h)
	val = math.MaxUint32
	for i := 0; i < d; i++ {
		pos := (h1 + uint32(i)*h2) % uint32(w)

		v := s.sk[i][pos]
		if v < val {
			val = v
		}
	}

	val += count

	// Conservative update means no counter is increased to more than the
	// size of the smallest counter plus the size of the increment.  This technique
	// first described in Cristian Estan and George Varghese. 2002. New directions in
	// traffic measurement and accounting. SIGCOMM Comput. Commun. Rev., 32(4).

	for i := 0; i < d; i++ {
		pos := (h1 + uint32(i)*h2) % uint32(w)
		v := s.sk[i][pos]
		if v < val {
			s.rowCounts[i] += (val - s.sk[i][pos])
			s.sk[i][pos] = val
		}
	}
	return val
}

// Count returns the estimated count for the given input.
func (s Sketch) Count(h string) uint32 {
	min := uint32(math.MaxUint32)
	w := len(s.sk[0])
	d := len(s.sk)

	h1, h2 := hashn(h)
	for i := 0; i < d; i++ {
		pos := (h1 + uint32(i)*h2) % uint32(w)

		v := s.sk[i][pos]
		if v < min {
			min = v
		}
	}
	return min
}

// Values returns the all the estimates for a given string
func (s Sketch) Values(h string) []uint32 {
	w := len(s.sk[0])
	d := len(s.sk)

	vals := make([]uint32, d)

	h1, h2 := hashn(h)
	for i := 0; i < d; i++ {
		pos := (h1 + uint32(i)*h2) % uint32(w)

		vals[i] = s.sk[i][pos]
	}

	return vals
}

/*
   CountMeanMin described in:
   Fan Deng and Davood Raﬁei. 2007. New estimation algorithms for streaming data: Count-min can do more.
   http://webdocs.cs.ualberta.ca/~fandeng/paper/cmm.pdf

   Sketch Algorithms for Estimating Point Queries in NLP
   Amit Goyal, Hal Daume III and Graham Cormode
   EMNLP-CONLL 2012
   http://www.umiacs.umd.edu/~amit/Papers/goyalPointQueryEMNLP12.pdf
*/

// CountMeanMin returns estimated count for the given input, using the count-min-mean
// heuristic.  This gives more accurate results than Count() for low-frequency
// counts at the cost of larger under-estimation error.  For tasks sensitive to
// under-estimation, use the regular Count() and only call ConservativeAdd()
// and ConservativeIncrement() when constructing your sketch.
func (s Sketch) CountMeanMin(h string) uint32 {
	min := uint32(math.MaxUint32)
	w := len(s.sk[0])
	d := len(s.sk)
	residues := make([]float64, d)
	h1, h2 := hashn(h)
	for i := 0; i < d; i++ {
		pos := (h1 + uint32(i)*h2) % uint32(w)
		v := s.sk[i][pos]
		noise := float64(s.rowCounts[i]-s.sk[i][pos]) / float64(w-1)
		residues[i] = float64(v) - noise
		// negative count doesn't make sense
		if residues[i] < 0 {
			residues[i] = 0
		}
		if v < min {
			min = v
		}
	}

	sort.Float64s(residues)
	var median uint32
	if d%2 == 1 {
		median = uint32(residues[(d+1)/2])
	} else {
		// integer average without overflow
		x := uint32(residues[d/2])
		y := uint32(residues[d/2+1])
		median = (x & y) + (x^y)/2
	}

	// count estimate over the upper-bound (min) doesn't make sense
	if min < median {
		return min
	}

	return median
}

// Merge the given sketch into this one.
// The sketches must have the same dimensions.
func (s *Sketch) Merge(from *Sketch) {
	if len(s.sk) != len(from.sk) || len(s.sk[0]) != len(from.sk[0]) {
		panic("Can't merge different sketches with different dimensions")
	}

	for i, l := range from.sk {
		for j, v := range l {
			s.sk[i][j] += v
		}
	}
}

// Clone returns a copy of this sketch
func (s *Sketch) Clone() *Sketch {

	w := len(s.sk[0])
	d := len(s.sk)

	clone := NewSketch(w, d)

	for i, l := range s.sk {
		copy(clone.sk[i], l)
	}

	copy(clone.rowCounts, s.rowCounts)

	return clone
}

/*
This is Algorithm 3 "Item Aggregation" from

Hokusai: Sketching Streams in Real Time (Sergiy Matusevych, Alex
Smola, Amr Ahmed, 2012)

Proceedings of the 28th International Conference on Conference on
Uncertainty in Artificial Intelligence (UAI)

http://www.auai.org/uai2012/papers/231.pdf
*/

// Compress reduces the space used by the sketch.  This also reduces
// the accuracy.  This routine panics if the width is not a power of
// two.
func (s *Sketch) Compress() {
	w := len(s.sk[0])

	if w&(w-1) != 0 {
		panic("width must be a power of two")
	}

	neww := w / 2

	for i, l := range s.sk {
		// We allocate a new array here so old space can actually be garbage collected.
		// TODO(dgryski): reslice and only reallocate every few compressions
		row := make([]uint32, neww)
		for j := 0; j < neww; j++ {
			row[j] = l[j] + l[j+neww]
		}
		s.sk[i] = row
	}
}
