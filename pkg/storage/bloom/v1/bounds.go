package v1

import (
	"fmt"
	"hash"
	"math"
	"strings"
	"unsafe"

	"github.com/pkg/errors"
	"github.com/prometheus/common/model"
	"golang.org/x/exp/slices"

	iter "github.com/grafana/loki/v3/pkg/iter/v2"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/util/encoding"
)

type BoundsCheck uint8

const (
	Before BoundsCheck = iota
	Overlap
	After
)

var (
	// FullBounds is the bounds that covers the entire fingerprint space
	FullBounds = NewBounds(0, model.Fingerprint(math.MaxUint64))
)

type FingerprintBounds struct {
	Min, Max model.Fingerprint
}

// Proto compat
// compiler check ensuring equal repr of underlying types
var _ FingerprintBounds = FingerprintBounds(logproto.FPBounds{})

func BoundsFromProto(pb logproto.FPBounds) FingerprintBounds {
	return FingerprintBounds(pb)
}

// Unsafe cast to avoid allocation. This _requires_ that the underlying types are the same
// which is checked by the compiler above
func MultiBoundsFromProto(pb []logproto.FPBounds) MultiFingerprintBounds {
	//nolint:unconvert
	return MultiFingerprintBounds(*(*MultiFingerprintBounds)(unsafe.Pointer(&pb)))
}

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

func (b FingerprintBounds) Hash(h hash.Hash32) error {
	var enc encoding.Encbuf
	enc.PutBE64(uint64(b.Min))
	enc.PutBE64(uint64(b.Max))
	_, err := h.Write(enc.Get())
	return errors.Wrap(err, "writing FingerprintBounds")
}

// Addr returns the string representation of the fingerprint bounds for use in
// content addressable storage.
// TODO(owen-d): incorporate this into the schema so we can change it,
// similar to `{,Parse}ExternalKey`
func (b FingerprintBounds) String() string {
	return fmt.Sprintf("%016x-%016x", uint64(b.Min), uint64(b.Max))
}

func (b FingerprintBounds) Less(other FingerprintBounds) bool {
	if b.Min != other.Min {
		return b.Min < other.Min
	}
	return b.Max <= other.Max
}

// Cmp returns the fingerprint's position relative to the bounds
func (b FingerprintBounds) Cmp(fp model.Fingerprint) BoundsCheck {
	if fp < b.Min {
		return Before
	} else if fp > b.Max {
		return After
	}
	return Overlap
}

// Overlaps returns whether the bounds (partially) overlap with the target bounds
func (b FingerprintBounds) Overlaps(target FingerprintBounds) bool {
	return b.Cmp(target.Min) != After && b.Cmp(target.Max) != Before
}

// Match implements TSDBs FingerprintFilter interface
func (b FingerprintBounds) Match(fp model.Fingerprint) bool {
	return b.Cmp(fp) == Overlap
}

// GetFromThrough implements TSDBs FingerprintFilter interface,
// NB(owen-d): adjusts to return `[from,through)` instead of `[from,through]` which the
// fingerprint bounds struct tracks.
func (b FingerprintBounds) GetFromThrough() (model.Fingerprint, model.Fingerprint) {
	from, through := b.Bounds()
	return from, max(through+1, through)
}

// Bounds returns the inclusive bounds [from,through]
func (b FingerprintBounds) Bounds() (model.Fingerprint, model.Fingerprint) {
	return b.Min, b.Max
}

// Slice returns a new fingerprint bounds clipped to the target bounds or nil if there is no overlap
func (b FingerprintBounds) Slice(min, max model.Fingerprint) *FingerprintBounds {
	return b.Intersection(FingerprintBounds{Min: min, Max: max})
}

// Within returns whether the fingerprint is fully within the target bounds
func (b FingerprintBounds) Within(target FingerprintBounds) bool {
	return b.Min >= target.Min && b.Max <= target.Max
}

// Returns whether the fingerprint bounds is equal to the target bounds
func (b FingerprintBounds) Equal(target FingerprintBounds) bool {
	return b.Min == target.Min && b.Max == target.Max
}

// Intersection returns the intersection of the two bounds
func (b FingerprintBounds) Intersection(target FingerprintBounds) *FingerprintBounds {
	if !b.Overlaps(target) {
		return nil
	}

	return &FingerprintBounds{
		Min: max(b.Min, target.Min),
		Max: min(b.Max, target.Max),
	}
}

// Union returns the union of the two bounds
func (b FingerprintBounds) Union(target FingerprintBounds) (res []FingerprintBounds) {
	if !b.Overlaps(target) {
		if target.Less(b) {
			b, target = target, b
		}

		// special case: if the bounds are contiguous, merge them
		if b.Max+1 == target.Min {
			return []FingerprintBounds{
				{
					Min: min(b.Min, target.Min),
					Max: max(b.Max, target.Max),
				},
			}
		}

		return []FingerprintBounds{b, target}
	}

	return []FingerprintBounds{
		{
			Min: min(b.Min, target.Min),
			Max: max(b.Max, target.Max),
		},
	}
}

// Unless returns the subspace of itself which does not intersect with the target bounds
func (b FingerprintBounds) Unless(target FingerprintBounds) (res []FingerprintBounds) {
	if !b.Overlaps(target) {
		return []FingerprintBounds{b}
	}

	if b.Within(target) {
		return nil
	}

	if b.Min < target.Min {
		res = append(res, FingerprintBounds{Min: b.Min, Max: min(b.Max, target.Min-1)})
	}
	if target.Max < b.Max {
		res = append(res, FingerprintBounds{Min: max(b.Min, target.Max+1), Max: b.Max})
	}
	return res
}

// Range returns the number of fingerprints in the bounds
func (b FingerprintBounds) Range() uint64 {
	return uint64(b.Max - b.Min)
}

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
	iter.Iterator[V]
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

func NewBoundedIter[V any](itr iter.Iterator[V], cmp func(V) BoundsCheck) *BoundedIter[V] {
	return &BoundedIter[V]{Iterator: itr, cmp: cmp}
}
