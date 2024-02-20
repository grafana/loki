// This file contains a bunch of utility functions for bloom components.

package bloomutils

import (
	"fmt"
	"math"
	"sort"

	"github.com/grafana/dskit/ring"
	"github.com/prometheus/common/model"
	"golang.org/x/exp/constraints"

	v1 "github.com/grafana/loki/pkg/storage/bloom/v1"
)

var (
	Uint32Range = Range[uint32]{Min: 0, Max: math.MaxUint32}
	Uint64Range = Range[uint64]{Min: 0, Max: math.MaxUint64}
)

type Range[T constraints.Unsigned] struct {
	Min, Max T
}

func (r Range[T]) String() string {
	return fmt.Sprintf("%016x-%016x", r.Min, r.Max)
}

func (r Range[T]) Less(other Range[T]) bool {
	if r.Min != other.Min {
		return r.Min < other.Min
	}
	return r.Max <= other.Max
}

func (r Range[T]) Cmp(t T) v1.BoundsCheck {
	if t < r.Min {
		return v1.Before
	} else if t > r.Max {
		return v1.After
	}
	return v1.Overlap
}

func NewRange[T constraints.Unsigned](min, max T) Range[T] {
	return Range[T]{Min: min, Max: max}
}

func NewTokenRange(min, max uint32) Range[uint32] {
	return Range[uint32]{Min: min, Max: max}
}

type InstanceWithTokenRange struct {
	Instance   ring.InstanceDesc
	TokenRange Range[uint32]
}

func (i InstanceWithTokenRange) Cmp(token uint32) v1.BoundsCheck {
	return i.TokenRange.Cmp(token)
}

type InstancesWithTokenRange []InstanceWithTokenRange

func (i InstancesWithTokenRange) Contains(token uint32) bool {
	for _, instance := range i {
		if instance.Cmp(token) == v1.Overlap {
			return true
		}
	}
	return false
}

// TODO(owen-d): use https://github.com/grafana/loki/pull/11975 after merge
func KeyspacesFromTokenRanges(tokenRanges ring.TokenRanges) []v1.FingerprintBounds {
	keyspaces := make([]v1.FingerprintBounds, 0, len(tokenRanges)/2)
	for i := 0; i < len(tokenRanges)-1; i += 2 {
		keyspaces = append(keyspaces, v1.FingerprintBounds{
			Min: model.Fingerprint(tokenRanges[i]) << 32,
			Max: model.Fingerprint(tokenRanges[i+1])<<32 | model.Fingerprint(math.MaxUint32),
		})
	}
	return keyspaces
}

// NewInstanceSortMergeIterator creates an iterator that yields instanceWithToken elements
// where the token of the elements are sorted in ascending order.
func NewInstanceSortMergeIterator(instances []ring.InstanceDesc) v1.Iterator[InstanceWithTokenRange] {
	tokenIters := make([]v1.PeekingIterator[v1.IndexedValue[uint32]], 0, len(instances))
	for i, inst := range instances {
		sort.Slice(inst.Tokens, func(a, b int) bool { return inst.Tokens[a] < inst.Tokens[b] })
		itr := v1.NewIterWithIndex(v1.NewSliceIter[uint32](inst.Tokens), i)
		tokenIters = append(tokenIters, v1.NewPeekingIter[v1.IndexedValue[uint32]](itr))
	}

	heapIter := v1.NewHeapIterator[v1.IndexedValue[uint32]](
		func(iv1, iv2 v1.IndexedValue[uint32]) bool {
			return iv1.Value() < iv2.Value()
		},
		tokenIters...,
	)

	prevToken := -1
	return v1.NewDedupingIter[v1.IndexedValue[uint32], InstanceWithTokenRange](
		func(iv v1.IndexedValue[uint32], iwtr InstanceWithTokenRange) bool {
			return false
		},
		func(iv v1.IndexedValue[uint32]) InstanceWithTokenRange {
			minToken, maxToken := uint32(prevToken+1), iv.Value()
			prevToken = int(maxToken)
			return InstanceWithTokenRange{
				Instance:   instances[iv.Index()],
				TokenRange: NewTokenRange(minToken, maxToken),
			}
		},
		func(iv v1.IndexedValue[uint32], iwtr InstanceWithTokenRange) InstanceWithTokenRange {
			panic("must not be called, because Eq() is always false")
		},
		v1.NewPeekingIter(heapIter),
	)
}
