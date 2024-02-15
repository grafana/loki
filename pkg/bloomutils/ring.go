// This file contains a bunch of utility functions for bloom components.

package bloomutils

import (
	"fmt"
	"math"
	"sort"

	"github.com/grafana/dskit/ring"
	"github.com/prometheus/common/model"
	"golang.org/x/exp/slices"

	v1 "github.com/grafana/loki/pkg/storage/bloom/v1"
)

type integer interface {
	~uint | ~uint32 | ~uint64 | ~int | ~int32 | ~int64
}

type Range[T integer] struct {
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

// GetInstanceWithTokenRange calculates the token range for a specific instance
// with given id based on the first token in the ring.
// This assumes that each instance in the ring is configured with only a single
// token.
func GetInstanceWithTokenRange(id string, instances []ring.InstanceDesc) (v1.FingerprintBounds, error) {

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
		return v1.FingerprintBounds{}, ring.ErrInstanceNotFound
	}

	i := uint64(idx)
	n := uint64(len(instances))
	step := math.MaxUint64 / n

	minToken := model.Fingerprint(step * i)
	maxToken := model.Fingerprint(step*i + step - 1)
	if i == n-1 {
		// extend the last token tange to MaxUint32
		maxToken = math.MaxUint64
	}

	return v1.NewBounds(minToken, maxToken), nil
}

// GetInstancesWithTokenRanges calculates the token ranges for a specific
// instance with given id based on all tokens in the ring.
// If the instances in the ring are configured with a single token, such as the
// bloom compactor, use GetInstanceWithTokenRange() instead.
func GetInstancesWithTokenRanges(id string, instances []ring.InstanceDesc) InstancesWithTokenRange {
	servers := make([]InstanceWithTokenRange, 0, len(instances))
	it := NewInstanceSortMergeIterator(instances)
	var firstInst ring.InstanceDesc
	var lastToken uint32
	for it.Next() {
		if firstInst.Id == "" {
			firstInst = it.At().Instance
		}
		if it.At().Instance.Id == id {
			servers = append(servers, it.At())
		}
		lastToken = it.At().TokenRange.Max
	}
	// append token range from lastToken+1 to MaxUint32
	// only if the instance with the first token is the current one
	if len(servers) > 0 && firstInst.Id == id {
		servers = append(servers, InstanceWithTokenRange{
			Instance:   servers[0].Instance,
			TokenRange: NewTokenRange(lastToken+1, math.MaxUint32),
		})
	}
	return servers
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
