// This file contains a bunch of utility functions for bloom components.
// TODO: Find a better location for this package

package bloomutils

import (
	"math"
	"sort"

	"github.com/grafana/dskit/ring"

	v1 "github.com/grafana/loki/pkg/storage/bloom/v1"
)

type InstanceWithTokenRange struct {
	Instance           ring.InstanceDesc
	MinToken, MaxToken uint32
}

func (i InstanceWithTokenRange) Cmp(token uint32) v1.BoundsCheck {
	if token < i.MinToken {
		return v1.Before
	} else if token > i.MaxToken {
		return v1.After
	}
	return v1.Overlap
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
		lastToken = it.At().MaxToken
	}
	// append token range from lastToken+1 to MaxUint32
	// only if the instance with the first token is the current one
	if len(servers) > 0 && firstInst.Id == id {
		servers = append(servers, InstanceWithTokenRange{
			MinToken: lastToken + 1,
			MaxToken: math.MaxUint32,
			Instance: servers[0].Instance,
		})
	}
	return servers
}

// NewInstanceSortMergeIterator creates an iterator that yields instanceWithToken elements
// where the token of the elements are sorted in ascending order.
func NewInstanceSortMergeIterator(instances []ring.InstanceDesc) v1.Iterator[InstanceWithTokenRange] {
	it := &sortMergeIterator[ring.InstanceDesc, uint32, InstanceWithTokenRange]{
		items: instances,
		transform: func(item ring.InstanceDesc, val uint32, prev *InstanceWithTokenRange) *InstanceWithTokenRange {
			var prevToken uint32
			if prev != nil {
				prevToken = prev.MaxToken + 1
			}
			return &InstanceWithTokenRange{Instance: item, MinToken: prevToken, MaxToken: val}
		},
	}
	sequences := make([]v1.PeekingIterator[v1.IndexedValue[uint32]], 0, len(instances))
	for i := range instances {
		sort.Slice(instances[i].Tokens, func(a, b int) bool {
			return instances[i].Tokens[a] < instances[i].Tokens[b]
		})
		iter := v1.NewIterWithIndex[uint32](v1.NewSliceIter(instances[i].Tokens), i)
		sequences = append(sequences, v1.NewPeekingIter[v1.IndexedValue[uint32]](iter))
	}
	it.heap = v1.NewHeapIterator(
		func(i, j v1.IndexedValue[uint32]) bool {
			return i.Value() < j.Value()
		},
		sequences...,
	)
	it.err = nil

	return it
}
