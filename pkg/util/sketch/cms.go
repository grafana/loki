package sketch

import (
	"errors"
	"math"

	"github.com/dustin/go-probably"
)

// NewCountMinSketch creates a new CMS for a given epsilon and delta value.
func NewCountMinSketch(epsilon, delta float64) (*probably.Sketch, error) {
	if epsilon <= 0 || epsilon >= 1 {
		return nil, errors.New("countminsketch: value of epsilon should be in range of (0, 1)")
	}
	if delta <= 0 || delta >= 1 {
		return nil, errors.New("countminsketch: value of delta should be in range of (0, 1)")
	}
	rowLen := int(math.Ceil(math.E / epsilon))
	numHashFuncs := int(math.Ceil(math.Log(1 / delta)))

	return probably.NewSketch(rowLen, numHashFuncs), nil
}
