//go:build !stringlabels && !dedupelabels

package log

import (
	"slices"
	"strings"

	"github.com/prometheus/prometheus/model/labels"
)

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

// BufferedLabelsBuilder is a simple builder that uses a label buffer passed in.
// It is used to avoid allocations when building labels.
type BufferedLabelsBuilder struct {
	buf labels.Labels
}

func NewBufferedLabelsBuilder(labels labels.Labels) *BufferedLabelsBuilder {
	return &BufferedLabelsBuilder{buf: labels[:0]}
}

func NewBufferedLabelsBuilderWithSize(size int) *BufferedLabelsBuilder {
	return NewBufferedLabelsBuilder(make(labels.Labels, 0, size))
}

func (b *BufferedLabelsBuilder) Reset() {
	b.buf = b.buf[:0]
}

func (b *BufferedLabelsBuilder) Add(label labels.Label) {
	b.buf = append(b.buf, label)
}

func (b *BufferedLabelsBuilder) Assign(labels labels.Labels) {
	b.buf = append(b.buf[:0], labels...)
}

func (b *BufferedLabelsBuilder) Labels() labels.Labels {
	//slices.SortFunc(b.buf, func(a, b labels.Label) int { return strings.Compare(a.Name, b.Name) })
	return b.buf
}

func (b *BufferedLabelsBuilder) Sort() {
	slices.SortFunc(b.buf, func(a, b labels.Label) int { return strings.Compare(a.Name, b.Name) })
}
