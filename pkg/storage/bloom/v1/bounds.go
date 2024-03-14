package v1

import (
	"fmt"
	"strings"

	"github.com/prometheus/common/model"
	"golang.org/x/exp/slices"

	"github.com/grafana/loki/pkg/logproto"
)

// ParseBoundsFromAddr parses a fingerprint bounds from a string
func ParseBoundsFromAddr(s string) (FingerprintBounds, error) {
	parts := strings.Split(s, "-")
	return ParseBoundsFromParts(parts[0], parts[1])
}

// ParseBoundsFromParts parses a fingerprint bounds already separated strings
func ParseBoundsFromParts(a, b string) (FingerprintBounds, error) {
	minFingerprint, err := model.ParseFingerprint(a)
	if err != nil {
		return FingerprintBounds{}, fmt.Errorf("error parsing minFingerprint %s : %w", a, err)
	}
	maxFingerprint, err := model.ParseFingerprint(b)
	if err != nil {
		return FingerprintBounds{}, fmt.Errorf("error parsing maxFingerprint %s : %w", b, err)
	}

	return NewBounds(minFingerprint, maxFingerprint), nil
}

func NewBounds(min, max model.Fingerprint) FingerprintBounds {
	return FingerprintBounds{Min: min, Max: max}
}

// aliasing to avoid circular dependency
type FingerprintBounds = logproto.FingerprintBounds

type MultiFingerprintBounds []FingerprintBounds

func (mb MultiFingerprintBounds) Union(target FingerprintBounds) MultiFingerprintBounds {
	if len(mb) == 0 {
		return MultiFingerprintBounds{target}
	}
	if len(mb) == 1 {
		return mb[0].Union(target)
	}

	mb = append(mb, target)
	slices.SortFunc(mb, func(a, b FingerprintBounds) int {
		if a.Less(b) {
			return -1
		} else if a.Equal(b) {
			return 0
		}
		return 1
	})

	var union MultiFingerprintBounds
	for i := 0; i < len(mb); i++ {
		j := len(union) - 1 // index of last item of union
		if j >= 0 && union[j].Max >= mb[i].Min-1 {
			union[j] = NewBounds(union[j].Min, max(mb[i].Max, union[j].Max))
		} else {
			union = append(union, mb[i])
		}
	}

	mb = union
	return mb
}

// unused, but illustrative
type BoundedIter[V any] struct {
	Iterator[V]
	cmp func(V) BoundsCheck
}

func (bi *BoundedIter[V]) Next() bool {
	for bi.Iterator.Next() {
		switch bi.cmp(bi.Iterator.At()) {
		case Before:
			continue
		case After:
			return false
		default:
			return true
		}
	}
	return false
}

func NewBoundedIter[V any](itr Iterator[V], cmp func(V) BoundsCheck) *BoundedIter[V] {
	return &BoundedIter[V]{Iterator: itr, cmp: cmp}
}
