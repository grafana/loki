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
	hash, h.buf = lbs.HashWithoutLabels(h.buf, []string(nil)...)
	return hash
}

// HashSorted computes the same hash Hash(labels.New(buf...)) would,
// directly from buf - which must already be sorted by Name, matching
// what labels.New's own sort would produce.
func (h *hasher) HashSorted(buf []labels.Label) uint64 {
	return h.Hash(labels.Labels(buf))
}
