package main

import (
	"errors"
	"math/rand"
)

type Sampler interface {
	Sample() bool
}

func NewProbabilisticSampler(p float64) (*ProbabilisticSampler, error) {
	if p < 0 || p > 1 {
		return &ProbabilisticSampler{}, errors.New("invalid probability, must be between 0 and 1")
	}

	return &ProbabilisticSampler{
		p:     p,
		scale: 1000,                        // precision of 0.1%
		rng:   rand.New(rand.NewSource(0)), // always use deterministic seed so identical instantiations sample the same way
	}, nil
}

// Sampler is a probabilistic sampler.
type ProbabilisticSampler struct {
	p     float64
	scale int // a multiplier used to calculate precision, e.g. 1000 for 0.1%
	rng   *rand.Rand
}

func (s *ProbabilisticSampler) Sample() bool {
	scale := 1000
	threshold := int(s.p * float64(scale))
	return s.rng.Intn(scale) < threshold
}
