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
		p:   p,
		rng: rand.New(rand.NewSource(0)), // always use deterministic seed so identical instantiations sample the same way
	}, nil
}

// Sampler is a probabilistic sampler.
type ProbabilisticSampler struct {
	p   float64
	rng *rand.Rand
}

func (s *ProbabilisticSampler) Sample() bool {
	scale := 1e6
	x := s.rng.Intn(int(scale))
	return float64(x) < s.p*scale

}
