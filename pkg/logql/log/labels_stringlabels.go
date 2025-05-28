//go:build stringlabels

package log

import "github.com/prometheus/prometheus/model/labels"

type hasher struct{}

// newHasher returns a hasher that computes hashes for labels.
func newHasher() *hasher {
	return &hasher{}
}

// Hash computes a hash of lbs.
// It is not guaranteed to be stable across different Loki processes or versions.
func (h *hasher) Hash(lbs labels.Labels) uint64 {
	// We use Hash() here because there's no performance advantage to using HashWithoutLabels() with stringlabels.
	// The results from Hash(l) and HashWithoutLabels(l, []string{}) are different with stringlabels, so using Hash
	// here also simplifies our tests.
	return lbs.Hash()
}
