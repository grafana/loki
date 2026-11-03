//go:build !slicelabels && !dedupelabels

package log

import (
	"github.com/cespare/xxhash/v2"
	"github.com/prometheus/prometheus/model/labels"
)

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
	return labels.StableHash(lbs)
}

// hashSep matches the separator byte labels.StableHash uses internally
// (vendor/github.com/prometheus/prometheus/model/labels/labels_common.go's
// sep = '\xff') - duplicated here because it's not public.
const hashSep = 0xff

// HashSorted computes the same hash Hash(labels.New(buf...)) would,
// directly from buf - which must already be sorted by Name, matching
// what labels.New's own sort would produce.
func (h *hasher) HashSorted(buf []labels.Label) uint64 {
	b := make([]byte, 0, 1024)
	for i, v := range buf {
		if len(b)+len(v.Name)+len(v.Value)+2 >= cap(b) {
			// If labels entry is 1KB+, switch to Write API - mirrors
			// StableHash's own fallback for the same reason.
			d := xxhash.New()
			_, _ = d.Write(b)
			for _, v := range buf[i:] {
				_, _ = d.WriteString(v.Name)
				_, _ = d.Write([]byte{hashSep})
				_, _ = d.WriteString(v.Value)
				_, _ = d.Write([]byte{hashSep})
			}
			return d.Sum64()
		}
		b = append(b, v.Name...)
		b = append(b, hashSep)
		b = append(b, v.Value...)
		b = append(b, hashSep)
	}
	return xxhash.Sum64(b)
}
