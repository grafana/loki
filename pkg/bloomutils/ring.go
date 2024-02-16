// This file contains a bunch of utility functions for bloom components.

package bloomutils

import (
	"errors"
	"fmt"
	"math"
	"sort"

	"github.com/grafana/dskit/ring"
	"golang.org/x/exp/constraints"
	"golang.org/x/exp/slices"

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

func NewTokenRange(min, max uint32) Range[uint32] {
	return Range[uint32]{min, max}
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

// KeyRangeForInstance calculates the token range for a specific instance
// with given id based on the first token in the ring.
// This assumes that each instance in the ring is configured with only a single
// token.
func KeyRangeForInstance[T constraints.Unsigned](id string, instances []ring.InstanceDesc, keyspace Range[T]) (Range[T], error) {

	// Sort instances -- they may not be sorted
	// because they're usually accessed by looking up the tokens (which are sorted)
	sort.Slice(instances, func(i, j int) bool {
		return instances[i].Tokens[0] < instances[j].Tokens[0]
	})

	idx := slices.IndexFunc(instances, func(inst ring.InstanceDesc) bool {
		return inst.Id == id
	})

	// instance with Id == id not found
	if idx == -1 {
		return Range[T]{}, ring.ErrInstanceNotFound
	}

	diff := keyspace.Max - keyspace.Min
	i := T(idx)
	n := T(len(instances))

	if diff < n {
		return Range[T]{}, errors.New("keyspace is smaller than amount of instances")
	}

	step := diff / n
	min := step * i
	max := step*i + step - 1
	if i == n-1 {
		// extend the last token tange to MaxUint32
		max = (keyspace.Max - keyspace.Min)
	}

	return Range[T]{min, max}, nil
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
