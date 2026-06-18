package postings

import (
	"context"
	"fmt"
	"time"

	"github.com/apache/arrow-go/v18/arrow/scalar"
	"github.com/prometheus/prometheus/model/labels"
	"golang.org/x/sync/errgroup"
)

// StreamResolver resolves SectionRefs from one or more postings sections that
// match stream matchers, a time range, and structured-metadata predicates.
type StreamResolver struct {
	matchers        []*labels.Matcher
	equalPredicates []*labels.Matcher
	start, end      time.Time
}

// StreamRef identifies a single stream within an object. It is globally unique
// across objects, so accumulator maps keyed on it union safely across sections.
type StreamRef struct {
	ObjectPath string
	StreamID   int64
}

// Key identifies a single section within an object. The bloom phase scopes
// resolved refs to the section keys that satisfy every remaining predicate.
type Key struct {
	ObjectPath   string
	SectionIndex int64
}

// NewStreamResolver builds a resolver. predicates arrives unfiltered; only
// MatchEqual predicates are retained for bloom filtering.
func NewStreamResolver(matchers, predicates []*labels.Matcher, start, end time.Time) *StreamResolver {
	var eq []*labels.Matcher
	for _, p := range predicates {
		if p != nil && p.Type == labels.MatchEqual {
			eq = append(eq, p)
		}
	}
	return &StreamResolver{
		matchers:        matchers,
		equalPredicates: eq,
		start:           start,
		end:             end,
	}
}

// maxMatchers bounds how many matchers the bitset-based accumulator can track.
// A query with more than 64 stream matchers is implausible; Resolve rejects it
// rather than silently truncating the matched-bit set.
const maxMatchers = 64

// sectionAccumulator holds the per-section state produced by the label phase.
// Each section scans into its own instance; instances are merged before
// finalize. Maps key on (ObjectPath, StreamID) via StreamRef, which is globally
// unique, so merges are plain set/bitwise unions. Section refs key on
// (ObjectPath, SectionIndex, StreamID) and merge time bounds on collision.
//
// matchedBits[ref] is a bitset over matcher indices: bit i is set when the
// stream satisfied matcher i. A stream matches the query when its bits equal
// the full matcher mask. Whether a stream carries a given matcher's label name
// (needed for missing-label semantics) and the per-stream AmbiguousLabels set
// are both derived from labelNamesByStream rather than stored separately.
type sectionAccumulator struct {
	matchedBits        map[StreamRef]uint64
	labelNamesByStream map[StreamRef]map[string]struct{}
	refBounds          map[refKey]bounds
}

type refKey struct {
	objectPath   string
	sectionIndex int64
	streamID     int64
}

type bounds struct {
	min, max  int64
	hasBounds bool
}

func newSectionAccumulator() *sectionAccumulator {
	return &sectionAccumulator{
		matchedBits:        make(map[StreamRef]uint64),
		labelNamesByStream: make(map[StreamRef]map[string]struct{}),
		refBounds:          make(map[refKey]bounds),
	}
}

// Resolve scans the already-opened sections and returns matching SectionRefs.
// The caller owns opening and closing the sections; Resolve opens and closes
// its own RowReaders per section per phase.
func (r *StreamResolver) Resolve(ctx context.Context, sections []*Section) ([]SectionRef, error) {
	if len(r.matchers) == 0 {
		return nil, nil
	}

	startNanos, endNanos := r.start.UnixNano(), r.end.UnixNano()
	activeMatchers := r.activeMatchers()
	if len(activeMatchers) > maxMatchers {
		return nil, fmt.Errorf("too many matchers: got=%d max=%d", len(activeMatchers), maxMatchers)
	}

	accs := make([]*sectionAccumulator, len(sections))
	g, ctx := errgroup.WithContext(ctx)
	for i, sec := range sections {
		g.Go(func() error {
			acc := newSectionAccumulator()
			if err := r.scanLabels(ctx, sec, acc, activeMatchers, startNanos, endNanos); err != nil {
				return fmt.Errorf("scanning labels: %w", err)
			}
			accs[i] = acc
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}

	merged := mergeAccumulators(accs)
	matching := r.matchingStreams(merged, activeMatchers)

	// Bloom phase is a no-op until Task 6: keep all matching refs.
	return r.assemble(merged, matching, nil /* no bloom drop */), nil
}

func (r *StreamResolver) activeMatchers() []*labels.Matcher {
	active := make([]*labels.Matcher, 0, len(r.matchers))
	for _, m := range r.matchers {
		if m != nil {
			active = append(active, m)
		}
	}
	return active
}

// scanLabels drains sec's KindLabel rows into acc.
func (r *StreamResolver) scanLabels(
	ctx context.Context,
	sec *Section,
	acc *sectionAccumulator,
	matchers []*labels.Matcher,
	startNanos, endNanos int64,
) error {
	kindCol := sectionColumn(sec, ColumnTypeKind)
	if kindCol == nil {
		return nil // section has no kind column; nothing to scan
	}
	rr := NewRowReader(ctx, sec, EqualPredicate{
		Column: kindCol,
		Value:  scalar.NewInt64Scalar(int64(KindLabel)),
	})
	defer func() { _ = rr.Close() }()

	for rr.Next() {
		observeLabelRow(rr.At(), acc, matchers, startNanos, endNanos)
	}
	return rr.Err()
}

// sectionColumn returns the section column of the given type, or nil.
func sectionColumn(sec *Section, ct ColumnType) *Column {
	for _, c := range sec.Columns() {
		if c.Type == ct {
			return c
		}
	}
	return nil
}

// observeLabelRow folds one KindLabel Row into acc: records the row's label
// name per stream, sets the matched-bit for every matcher this (name, value)
// satisfies, and updates time-pruned section-ref bounds for every stream in the
// row's bitmap.
func observeLabelRow(row Row, acc *sectionAccumulator, matchers []*labels.Matcher, startNanos, endNanos int64) {
	name, value := row.ColumnName, row.LabelValue

	var matchedBits uint64
	for i, m := range matchers {
		if m.Name == name && m.Matches(value) {
			matchedBits |= 1 << uint(i)
		}
	}

	// Time overlap for ref bounds. Null bounds (both zero) are kept.
	hasBounds := row.MinTimestamp != 0 || row.MaxTimestamp != 0
	overlaps := !hasBounds || (row.MaxTimestamp >= startNanos && row.MinTimestamp <= endNanos)

	for streamID := range row.StreamIDs() {
		ref := StreamRef{ObjectPath: row.ObjectPath, StreamID: streamID}

		if name != "" {
			names := acc.labelNamesByStream[ref]
			if names == nil {
				names = make(map[string]struct{})
				acc.labelNamesByStream[ref] = names
			}
			names[name] = struct{}{}
		}
		if matchedBits != 0 {
			acc.matchedBits[ref] |= matchedBits
		} else if _, seen := acc.matchedBits[ref]; !seen {
			// Ensure the stream is tracked even before it satisfies a matcher,
			// so missing-label semantics can find it at finalize.
			acc.matchedBits[ref] = 0
		}

		if overlaps {
			k := refKey{objectPath: row.ObjectPath, sectionIndex: row.SectionIndex, streamID: streamID}
			b := acc.refBounds[k]
			mergeBounds(&b, row.MinTimestamp, row.MaxTimestamp, hasBounds)
			acc.refBounds[k] = b
		}
	}
}

func mergeBounds(b *bounds, minTs, maxTs int64, hasBounds bool) {
	if !hasBounds {
		return
	}
	if !b.hasBounds {
		b.min, b.max, b.hasBounds = minTs, maxTs, true
		return
	}
	if minTs < b.min {
		b.min = minTs
	}
	if maxTs > b.max {
		b.max = maxTs
	}
}

// mergeAccumulators unions per-section accumulators into one. Matched-bit sets
// merge by bitwise OR; label-name sets and ref bounds merge by union.
func mergeAccumulators(accs []*sectionAccumulator) *sectionAccumulator {
	out := newSectionAccumulator()
	for _, acc := range accs {
		if acc == nil {
			continue
		}
		for ref, bits := range acc.matchedBits {
			out.matchedBits[ref] |= bits
		}
		for ref, names := range acc.labelNamesByStream {
			dst := out.labelNamesByStream[ref]
			if dst == nil {
				dst = make(map[string]struct{})
				out.labelNamesByStream[ref] = dst
			}
			for n := range names {
				dst[n] = struct{}{}
			}
		}
		for k, b := range acc.refBounds {
			existing, ok := out.refBounds[k]
			if !ok {
				out.refBounds[k] = b
				continue
			}
			mergeBounds(&existing, b.min, b.max, b.hasBounds)
			out.refBounds[k] = existing
		}
	}
	return out
}

// matchingStreams returns the streams that satisfied every matcher, after
// applying missing-label semantics. A stream matches when its matched-bit set
// equals the full matcher mask.
func (r *StreamResolver) matchingStreams(acc *sectionAccumulator, matchers []*labels.Matcher) map[StreamRef]struct{} {
	if len(matchers) == 0 {
		return map[StreamRef]struct{}{}
	}
	applyMissingLabelSemantics(acc, matchers)

	fullMask := uint64(1)<<uint(len(matchers)) - 1
	out := make(map[StreamRef]struct{})
	for ref, bits := range acc.matchedBits {
		if bits == fullMask {
			out[ref] = struct{}{}
		}
	}
	return out
}

// applyMissingLabelSemantics: a matcher that matches the empty value also
// matches streams that never carried its label name. Whether a stream carries
// matcher i's name is derived from labelNamesByStream.
func applyMissingLabelSemantics(acc *sectionAccumulator, matchers []*labels.Matcher) {
	for i, m := range matchers {
		if !matcherMatchesEmpty(m) {
			continue
		}
		bit := uint64(1) << uint(i)
		for ref, bits := range acc.matchedBits {
			if bits&bit != 0 {
				continue // already matched on a value
			}
			if _, has := acc.labelNamesByStream[ref][m.Name]; has {
				continue // carries the name but did not match -> stays unmatched
			}
			acc.matchedBits[ref] = bits | bit
		}
	}
}

func matcherMatchesEmpty(m *labels.Matcher) bool {
	switch m.Type {
	case labels.MatchEqual:
		return m.Value == ""
	case labels.MatchNotEqual:
		return m.Value != ""
	case labels.MatchRegexp, labels.MatchNotRegexp:
		return m.Matches("")
	default:
		return false
	}
}

// assemble scopes refs to matching streams, drops refs whose section key failed
// bloom matching (when bloomKeep is non-nil), and populates AmbiguousLabels.
func (r *StreamResolver) assemble(
	acc *sectionAccumulator,
	matching map[StreamRef]struct{},
	bloomKeep map[Key]struct{},
) []SectionRef {
	predNames := make(map[string]struct{}, len(r.equalPredicates))
	for _, p := range r.equalPredicates {
		predNames[p.Name] = struct{}{}
	}

	var out []SectionRef
	for k, b := range acc.refBounds {
		ref := StreamRef{ObjectPath: k.objectPath, StreamID: k.streamID}
		if _, ok := matching[ref]; !ok {
			continue
		}
		if bloomKeep != nil {
			if _, ok := bloomKeep[Key{ObjectPath: k.objectPath, SectionIndex: k.sectionIndex}]; !ok {
				continue
			}
		}
		out = append(out, SectionRef{
			ObjectPath:      k.objectPath,
			SectionIndex:    k.sectionIndex,
			StreamID:        k.streamID,
			MinTimestamp:    b.min,
			MaxTimestamp:    b.max,
			AmbiguousLabels: ambiguousLabelsFor(acc.labelNamesByStream[ref], predNames),
		})
	}
	return out
}

func ambiguousLabelsFor(streamLabels, predNames map[string]struct{}) []string {
	if len(streamLabels) == 0 || len(predNames) == 0 {
		return nil
	}
	var out []string
	for name := range streamLabels {
		if _, ok := predNames[name]; ok {
			out = append(out, name)
		}
	}
	return out
}
