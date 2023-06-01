package sketch

import (
	"errors"
	"fmt"
	"math"
	"sort"
)

type lhhCounter struct {
	name  string
	count int64
}

type LHHTopK struct {
	k      int
	sketch [][]int64
	// space optimization, only have one row of LHH counters
	lhhEstimates []lhhCounter
}

func NewLHHTopk(k int, numEntries int) (LHHTopK, error) {
	// this is the amount we're okay with overcounting by in the sketch
	// for example; if a keys real value was 100 we might receive an estimate of 105
	// but if a keys real value is 0 we also might receive an estimate of 5
	diff := 5

	// we need to choose the sketch size
	// todo: derive the diff from input length?
	eps := float64(diff) / float64(numEntries/2)
	// we want a 99.9% probability of being within the range of exact value to exact value + diff
	delta := 0.001

	sk, err := newLHHSketch(eps, delta)
	if err != nil {
		return LHHTopK{}, err
	}
	return LHHTopK{sketch: sk, lhhEstimates: make([]lhhCounter, len(sk[0])), k: k}, nil
}

func newLHHSketch(epsilon, delta float64) ([][]int64, error) {
	if epsilon <= 0 || epsilon >= 1 {
		return nil, errors.New("countminsketch: value of epsilon should be in range of (0, 1)")
	}
	if delta <= 0 || delta >= 1 {
		return nil, errors.New("countminsketch: value of delta should be in range of (0, 1)")
	}
	rowLen := int(math.Ceil(math.E / epsilon))
	numHashFuncs := int(math.Ceil(math.Log(1 / delta)))
	sketch := make([][]int64, numHashFuncs)
	for i := 0; i < numHashFuncs; i++ {
		sketch[i] = make([]int64, rowLen)
	}
	return sketch, nil
}

func (t *LHHTopK) Observe(event string) {
	h1, h2 := hashn(event)
	for i := 0; i < len(t.sketch); i++ {
		pos := (h1 + uint32(i)*h2) % uint32(len(t.sketch[0]))
		t.sketch[i][pos] += 1

		// space optimization, only have one row of LHH counters
		if i == 0 {
			if t.lhhEstimates[pos].name == event {
				t.lhhEstimates[pos].count += 1
				continue
			}
			t.lhhEstimates[pos].count -= 1
			if t.lhhEstimates[pos].count <= 0 {
				t.lhhEstimates[pos].count = 1
				t.lhhEstimates[pos].name = event
			}
		}

	}
}

func (t *LHHTopK) TopK() TopKResult {
	res := make(TopKResult, 0) //t.k)

	// paper suggests using the following function
	fracEpsilon := t.k * 10
	threshold := (int64((len(t.sketch[0]) / int(t.k)) - (len(t.sketch[0]) / int(fracEpsilon)))) / 2
	// since each event we insert will be mapped to every row in the matrix,
	// and every row in the matrix is independent, we can find the topk
	// by looking at just the 1st row of the matrix
	for i := 0; i < len(t.sketch[0]); i++ {
		if t.lhhEstimates[i].count > threshold || t.min(t.lhhEstimates[i].name) > threshold {
			res = append(res, element{Event: t.lhhEstimates[i].name, Count: t.min(t.lhhEstimates[i].name)})
		}
	}
	sort.Sort(res)
	return res[:t.k]
}

func (t *LHHTopK) min(event string) int64 {
	min := int64(math.MaxInt64)
	h1, h2 := hashn(event)
	for i := 0; i < len(t.sketch); i++ {
		pos := (h1 + uint32(i)*h2) % uint32(len(t.sketch[0]))

		v := t.sketch[i][pos]
		if v < min {
			min = v
		}
	}
	return min
}

// Merge merges t2 into t, returns an error if the sketches are not of the same dimensions.
func (t *LHHTopK) Merge(t2 LHHTopK) error {
	if len(t.sketch) != len(t2.sketch) || len(t.sketch[0]) != len(t2.sketch[0]) {
		return fmt.Errorf("sketch dimensions did not match, cannot merge")
	}

	for i := 0; i < len(t.lhhEstimates); i++ {
		if t2.lhhEstimates[i].name != t.lhhEstimates[i].name {
			t.lhhEstimates[i].count -= t2.lhhEstimates[i].count
			if t.lhhEstimates[i].count < 0 {
				t.lhhEstimates[i].name = t2.lhhEstimates[i].name
				t.lhhEstimates[i].count *= -1
			}
		}
	}

	for i := 0; i < len(t.sketch); i++ {
		for j := 0; j < len(t.sketch[0]); j++ {
			t.sketch[i][j] += t2.sketch[i][j]
		}
	}
	return nil
}
