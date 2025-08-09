//go:build slicelabels

package log

import "github.com/prometheus/prometheus/model/labels"

type hasher struct {
	buf []byte // buffer for computing hash without bytes slice allocation.
}

// newHasher returns a hasher that computes hashes for labels by reusing the same buffer.
func newHasher() *hasher {
	return &hasher{
		buf: make([]byte, 0, 1024),
	}
}

// Hash computes a hash of lbs.
// It is not guaranteed to be stable across different Loki processes or versions.
func (h *hasher) Hash(lbs labels.Labels) uint64 {
	var hash uint64
	hash, h.buf = lbs.HashWithoutLabels(h.buf)
	return hash
}
