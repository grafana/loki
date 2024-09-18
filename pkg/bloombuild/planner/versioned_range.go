package planner

import (
	"sort"

	"github.com/prometheus/common/model"

	v1 "github.com/grafana/loki/v3/pkg/storage/bloom/v1"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/bloomshipper"
)

type tsdbToken struct {
	through model.Fingerprint // inclusive
	version int               // TSDB version
}

// a ring of token ranges used to identify old metas.
// each token represents that a TSDB version has covered the entire range
// up to that point from the previous token.
type tsdbTokenRange []tsdbToken

func (t tsdbTokenRange) Len() int {
	return len(t)
}

func (t tsdbTokenRange) Less(i, j int) bool {
	return t[i].through < t[j].through
}

func (t tsdbTokenRange) Swap(i, j int) {
	t[i], t[j] = t[j], t[i]
}

// Add ensures a versioned set of bounds is added to the range. If the bounds are already
// covered by a more up to date version, it returns false.
func (t tsdbTokenRange) Add(version int, bounds v1.FingerprintBounds) (res tsdbTokenRange, added bool) {
	// allows attempting to join neighboring token ranges with identical versions
	// that aren't known until the end of the function
	var shouldReassemble bool
	var reassembleFrom int
	defer func() {
		if shouldReassemble {
			res = res.reassemble(reassembleFrom)
		}
	}()

	// special case: first token
	if len(t) == 0 {
		tok := tsdbToken{through: bounds.Max, version: version}
		// special case: first token is included in bounds, no need to fill negative space
		if bounds.Min == 0 {
			return append(t, tok), true
		}
		// Use a negative version to indicate that the range is not covered by any version.
		return append(t, tsdbToken{through: bounds.Min - 1, version: -1}, tok), true
	}

	// For non-nil token ranges, we continually update the range with newer versions.
	for {
		// find first token that covers the start of the range
		i := sort.Search(len(t), func(i int) bool {
			return t[i].through >= bounds.Min
		})

		if i == len(t) {
			tok := tsdbToken{through: bounds.Max, version: version}

			// edge case: there is no gap between the previous token range
			// and the new one;
			// skip adding a negative token
			if t[len(t)-1].through == bounds.Min-1 {
				return append(t, tok), true
			}

			// the range is not covered by any version and we are at the end of the range.
			// Add a negative token and the new token.
			negative := tsdbToken{through: bounds.Min - 1, version: -1}
			return append(t, negative, tok), true
		}

		// Otherwise, we've found a token that covers the start of the range.
		newer := t[i].version < version
		preExisting := t.boundsForToken(i)
		if !newer {
			if bounds.Within(preExisting) {
				// The range is already covered by a more up to date version, no need
				// to add anything, but honor if an earlier token was added
				return t, added
			}

			// The range is partially covered by a more up to date version;
			// update the range we need to check and continue
			bounds = v1.NewBounds(preExisting.Max+1, bounds.Max)
			continue
		}

		// If we need to update the range, there are 5 cases:
		// 1. `equal`: the incoming range equals an existing range ()
		//      ------        # addition
		//      ------        # src
		// 2. `subset`: the incoming range is a subset of an existing range
		//     ------        # addition
		//     --------       # src
		// 3. `overflow_both_sides`: the incoming range is a superset of an existing range. This is not possible
		// because the first token in the ring implicitly covers the left bound (zero) of all possible fps.
		// Therefore, we can skip this case.
		//     ------        # addition
		//       ----      # src
		// 4. `right_overflow`: the incoming range overflows the right side of an existing range
		//      ------       # addition
		//    ------          # src
		// 5. `left_overflow`: the incoming range overflows the left side of an existing range. This can be skipped
		// for the same reason as `superset`.
		//     ------        # addition
		//       ------      # src

		// 1) (`equal`): we're replacing the same bounds
		if bounds.Equal(preExisting) {
			t[i].version = version
			return t, true
		}

		// 2) (`subset`): the incoming range is a subset of an existing range
		if bounds.Within(preExisting) {
			// 2a) the incoming range touches the existing range's minimum bound
			if bounds.Min == preExisting.Min {
				tok := tsdbToken{through: bounds.Max, version: version}
				t = append(t, tsdbToken{})
				copy(t[i+1:], t[i:])
				t[i] = tok
				return t, true
			}
			// 2b) the incoming range touches the existing range's maximum bound
			if bounds.Max == preExisting.Max {
				t[i].through = bounds.Min - 1
				tok := tsdbToken{through: bounds.Max, version: version}
				t = append(t, tsdbToken{})
				copy(t[i+2:], t[i+1:])
				t[i+1] = tok
				return t, true
			}

			// 2c) the incoming range is does not touch either edge;
			// add two tokens (the new one and a new left-bound for the old range)
			tok := tsdbToken{through: bounds.Max, version: version}
			t = append(t, tsdbToken{}, tsdbToken{})
			copy(t[i+2:], t[i:])
			t[i+1] = tok
			t[i].through = bounds.Min - 1
			return t, true
		}

		// 4) (`right_overflow`): the incoming range overflows the right side of an existing range

		// 4a) shortcut: the incoming range is a right-overlapping superset of the existing range.
		// replace the existing token's version, update reassembly targets for merging neighboring ranges
		// w/ the same version, and continue
		if preExisting.Min == bounds.Min {
			t[i].version = version
			bounds.Min = preExisting.Max + 1
			added = true
			if !shouldReassemble {
				reassembleFrom = i
				shouldReassemble = true
			}
			continue
		}

		// 4b) the incoming range overlaps the right side of the existing range but
		// does not touch the left side;
		// add a new token for the right side of the existing range then update the reassembly targets
		// and continue
		overlap := tsdbToken{through: t[i].through, version: version}
		t[i].through = bounds.Min - 1
		t = append(t, tsdbToken{})
		copy(t[i+2:], t[i+1:])
		t[i+1] = overlap
		added = true
		bounds.Min = overlap.through + 1
		if !shouldReassemble {
			reassembleFrom = i + 1
			shouldReassemble = true
		}
		continue
	}
}

func (t tsdbTokenRange) boundsForToken(i int) v1.FingerprintBounds {
	if i == 0 {
		return v1.FingerprintBounds{Min: 0, Max: t[i].through}
	}
	return v1.FingerprintBounds{Min: t[i-1].through + 1, Max: t[i].through}
}

// reassemble merges neighboring tokens with the same version
func (t tsdbTokenRange) reassemble(from int) tsdbTokenRange {
	reassembleTo := from
	for i := from; i < len(t)-1; i++ {
		if t[i].version != t[i+1].version {
			break
		}
		reassembleTo = i + 1
	}

	if reassembleTo == from {
		return t
	}
	t[from].through = t[reassembleTo].through
	copy(t[from+1:], t[reassembleTo+1:])
	return t[:len(t)-(reassembleTo-from)]
}

func outdatedMetas(metas []bloomshipper.Meta) ([]bloomshipper.Meta, []bloomshipper.Meta) {
	var outdated []bloomshipper.Meta
	var upToDate []bloomshipper.Meta

	// Sort metas descending by most recent source when checking
	// for outdated metas (older metas are discarded if they don't change the range).
	sort.Slice(metas, func(i, j int) bool {
		a, aExists := metas[i].MostRecentSource()
		b, bExists := metas[j].MostRecentSource()

		if !aExists && !bExists {
			// stable sort two sourceless metas by their bounds (easier testing)
			return metas[i].Bounds.Less(metas[j].Bounds)
		}

		if !aExists {
			// If a meta has no sources, it's out of date by definition.
			// By convention we sort it to the beginning of the list and will mark it for removal later
			return true
		}

		if !bExists {
			// if a exists but b does not, mark b as lesser, sorting b to the
			// front
			return false
		}
		return !a.TS.Before(b.TS)
	})

	var (
		tokenRange tsdbTokenRange
		added      bool
	)

	for _, meta := range metas {
		mostRecent, exists := meta.MostRecentSource()
		if !exists {
			// if the meta exists but does not reference a TSDB, it's out of date
			// TODO(owen-d): this shouldn't happen, figure out why
			outdated = append(outdated, meta)
		}
		version := int(model.TimeFromUnixNano(mostRecent.TS.UnixNano()))
		tokenRange, added = tokenRange.Add(version, meta.Bounds)
		if !added {
			outdated = append(outdated, meta)
			continue
		}

		upToDate = append(upToDate, meta)
	}

	// We previously sorted the input metas by their TSDB source TS, therefore, they may not be sorted by FP anymore.
	// We need to re-sort them by their FP to match the original order.
	sort.Slice(upToDate, func(i, j int) bool {
		return upToDate[i].Bounds.Less(upToDate[j].Bounds)
	})

	return upToDate, outdated
}
