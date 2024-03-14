// This file contains a bunch of utility functions for bloom components.

package bloomutils

import (
	"errors"
	"fmt"
	"math"
	"sort"

	"github.com/grafana/dskit/ring"
	"github.com/prometheus/common/model"
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

func TokenRangesForInstance(id string, instances []ring.InstanceDesc) (ranges ring.TokenRanges, err error) {
	var ownedTokens map[uint32]struct{}

	// lifted from grafana/dskit/ring/model.go <*Desc>.GetTokens()
	toks := make([][]uint32, 0, len(instances))
	for _, instance := range instances {
		if instance.Id == id {
			ranges = make(ring.TokenRanges, 0, 2*(len(instance.Tokens)+1))
			ownedTokens = make(map[uint32]struct{}, len(instance.Tokens))
			for _, tok := range instance.Tokens {
				ownedTokens[tok] = struct{}{}
			}
		}

		// Tokens may not be sorted for an older version which, so we enforce sorting here.
		tokens := instance.Tokens
		if !sort.IsSorted(ring.Tokens(tokens)) {
			sort.Sort(ring.Tokens(tokens))
		}

		toks = append(toks, tokens)
	}

	if cap(ranges) == 0 {
		return nil, fmt.Errorf("instance %s not found", id)
	}

	allTokens := ring.MergeTokens(toks)
	if len(allTokens) == 0 {
		return nil, errors.New("no tokens in the ring")
	}

	// mostly lifted from grafana/dskit/ring/token_range.go <*Ring>.GetTokenRangesForInstance()

	// non-zero value means we're now looking for start of the range. Zero value means we're looking for next end of range (ie. token owned by this instance).
	rangeEnd := uint32(0)

	// if this instance claimed the first token, it owns the wrap-around range, which we'll break into two separate ranges
	firstToken := allTokens[0]
	_, ownsFirstToken := ownedTokens[firstToken]

	if ownsFirstToken {
		// we'll start by looking for the beginning of the range that ends with math.MaxUint32
		rangeEnd = math.MaxUint32
	}

	// walk the ring backwards, alternating looking for ends and starts of ranges
	for i := len(allTokens) - 1; i > 0; i-- {
		token := allTokens[i]
		_, owned := ownedTokens[token]

		if rangeEnd == 0 {
			// we're looking for the end of the next range
			if owned {
				rangeEnd = token - 1
			}
		} else {
			// we have a range end, and are looking for the start of the range
			if !owned {
				ranges = append(ranges, rangeEnd, token)
				rangeEnd = 0
			}
		}
	}

	// finally look at the first token again
	// - if we have a range end, check if we claimed token 0
	//   - if we don't, we have our start
	//   - if we do, the start is 0
	// - if we don't have a range end, check if we claimed token 0
	//   - if we don't, do nothing
	//   - if we do, add the range of [0, token-1]
	//     - BUT, if the token itself is 0, do nothing, because we don't own the tokens themselves (we should be covered by the already added range that ends with MaxUint32)

	if rangeEnd == 0 {
		if ownsFirstToken && firstToken != 0 {
			ranges = append(ranges, firstToken-1, 0)
		}
	} else {
		if ownsFirstToken {
			ranges = append(ranges, rangeEnd, 0)
		} else {
			ranges = append(ranges, rangeEnd, firstToken)
		}
	}

	// Ensure returned ranges are sorted.
	slices.Sort(ranges)

	return ranges, nil
}
