package metastore

import (
	"bytes"
	"context"
	"fmt"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"golang.org/x/sync/errgroup"

	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
	"github.com/grafana/loki/v3/pkg/memory"
)

// SectionStreams holds the matching streams within one logs section
type SectionStreams struct {
	Section        postings.SectionRef
	StreamBitmap   []byte // bit i set => stream i matches
	MinTimestamp   int64  // unix nanos; 0 means unknown
	MaxTimestamp   int64  // unix nanos; 0 means unknown
	AmbiguousNames []string
}

// streamSelector evaluates a LogQL stream selector against one or more postings
// sections via postings.Scanner
type streamSelector struct {
	matchers        []*labels.Matcher
	equalPredicates []*labels.Matcher
	start, end      time.Time
}

func newStreamSelector(matchers, predicates []*labels.Matcher, start, end time.Time) *streamSelector {
	var eq []*labels.Matcher
	for _, p := range predicates {
		if p != nil && p.Type == labels.MatchEqual {
			eq = append(eq, p)
		}
	}
	return &streamSelector{matchers: matchers, equalPredicates: eq, start: start, end: end}
}

// accum holds the per-logs-section state. result is the running intersection of
// every matcher's hit; timeOverlap is the streams whose rows fall within the
// query window. The timestamp envelope spans the overlapping rows.
type accum struct {
	result       *memory.Bitmap
	streamLabels map[string]struct{}
	timeOverlap  *memory.Bitmap
	minTS, maxTS int64
	hasTS        bool
}

// selectStreams scans the already-opened sections and returns matching
// SectionStreams, one per logs section with at least one matching stream. The
// caller owns opening and closing the sections.
func (s *streamSelector) selectStreams(ctx context.Context, sections []*postings.Section) ([]SectionStreams, error) {
	if len(s.matchers) == 0 {
		return nil, nil
	}

	positive, emptyCapable := s.partitionMatchers()
	if len(positive) == 0 {
		return nil, fmt.Errorf("stream selector requires at least one non-empty-capable matcher")
	}

	compiledPositive, err := compileAll(positive)
	if err != nil {
		return nil, err
	}
	compiledEmptyCapable, err := compileAll(emptyCapable)
	if err != nil {
		return nil, err
	}

	startNanos, endNanos := s.start.UnixNano(), s.end.UnixNano()

	accums, err := s.resolve(ctx, sections, compiledPositive, compiledEmptyCapable, startNanos, endNanos)
	if err != nil {
		return nil, err
	}

	survivors, ambiguousNamesByRef, err := s.admitSections(ctx, sections, accums)
	if err != nil {
		return nil, err
	}

	var out []SectionStreams
	for ref, acc := range accums {
		if survivors != nil {
			if _, ok := survivors[ref]; !ok {
				continue
			}
		}
		for name := range ambiguousNamesByRef[ref] {
			acc.streamLabels[name] = struct{}{}
		}
		if result, ok := s.finalize(ref, acc, startNanos, endNanos); ok {
			out = append(out, result)
		}
	}
	return out, nil
}

// resolve evaluates the matchers against every physical section and returns the
// surviving accumulators keyed by logs SectionRef. A logical section's label
// rows can be split across multiple physical sections (the builder splits by
// column name and size), so each matcher's hits are unioned across all physical
// sections before the cross-matcher intersection. ANDing per physical section
// would wrongly drop a stream whose matched labels live in different physical
// sections.
func (s *streamSelector) resolve(
	ctx context.Context,
	sections []*postings.Section,
	positive []postings.CompiledMatcher,
	emptyCapable []postings.CompiledMatcher,
	startNanos, endNanos int64,
) (map[postings.SectionRef]*accum, error) {
	accums := make(map[postings.SectionRef]*accum)

	first := true
	for _, cm := range positive {
		hits, err := s.matchAcrossSections(ctx, sections, cm, accums, startNanos, endNanos)
		if err != nil {
			return nil, err
		}
		combine(accums, hits, first)
		first = false
		if len(accums) == 0 {
			return nil, nil
		}
	}

	for _, cm := range emptyCapable {
		hits, err := s.labelStreamsAcrossSections(ctx, sections, cm, accums, startNanos, endNanos)
		if err != nil {
			return nil, err
		}
		combine(accums, hits, false)
		if len(accums) == 0 {
			return nil, nil
		}
	}

	return accums, nil
}

// matchAcrossSections scans every physical section for cm's name AND value,
// unioning the matched-stream bitmaps per logs SectionRef. It also records the
// matcher's label name and folds the time overlap into each ref's accumulator.
func (s *streamSelector) matchAcrossSections(
	ctx context.Context,
	sections []*postings.Section,
	cm postings.CompiledMatcher,
	accums map[postings.SectionRef]*accum,
	startNanos, endNanos int64,
) (map[postings.SectionRef]*memory.Bitmap, error) {
	hits := make(map[postings.SectionRef]*memory.Bitmap)
	for _, sec := range sections {
		matches, err := postings.NewScanner(sec).MatchLabel(ctx, cm)
		if err != nil {
			return nil, err
		}
		for ref, ms := range matches {
			acc := accumFor(accums, ref)
			acc.streamLabels[cm.Matcher().Name] = struct{}{}
			foldTimeOverlap(acc, ms.Matched, ms.Min, ms.Max, ms.Has, startNanos, endNanos)
			h := hits[ref]
			memory.OrInto(&h, ms.Matched)
			hits[ref] = h
		}
	}
	return hits, nil
}

// labelStreamsAcrossSections scans every physical section for cm's name only,
// unioning the present and matched bitmaps per logs SectionRef, then computes
// the empty-capable hit (matched streams plus streams lacking the label) against
// the running intersection in each ref's accumulator.
func (s *streamSelector) labelStreamsAcrossSections(
	ctx context.Context,
	sections []*postings.Section,
	cm postings.CompiledMatcher,
	accums map[postings.SectionRef]*accum,
	startNanos, endNanos int64,
) (map[postings.SectionRef]*memory.Bitmap, error) {
	name := cm.Matcher().Name
	present := make(map[postings.SectionRef]*memory.Bitmap)
	matched := make(map[postings.SectionRef]*memory.Bitmap)
	for _, sec := range sections {
		streams, err := postings.NewScanner(sec).LabelStreams(ctx, cm)
		if err != nil {
			return nil, err
		}
		for ref, ls := range streams {
			if _, tracked := accums[ref]; !tracked {
				continue
			}
			acc := accums[ref]
			acc.streamLabels[name] = struct{}{}
			foldTimeOverlap(acc, ls.Present, ls.Min, ls.Max, ls.Has, startNanos, endNanos)
			p := present[ref]
			memory.OrInto(&p, ls.Present)
			present[ref] = p
			m := matched[ref]
			memory.OrInto(&m, ls.Matched)
			matched[ref] = m
		}
	}

	hits := make(map[postings.SectionRef]*memory.Bitmap, len(accums))
	for ref, acc := range accums {
		missing := acc.result.AndNot(memory.OrEmpty(present[ref]))
		hit := matched[ref]
		memory.OrInto(&hit, missing)
		hits[ref] = memory.OrEmpty(hit)
	}
	return hits, nil
}

// accumFor returns ref's accumulator, creating it on first use.
func accumFor(accums map[postings.SectionRef]*accum, ref postings.SectionRef) *accum {
	acc, ok := accums[ref]
	if !ok {
		acc = &accum{streamLabels: make(map[string]struct{})}
		accums[ref] = acc
	}
	return acc
}

// combine intersects each ref's running result with this round's hits, pruning
// refs that drop to empty. Deleting from accums mid-range is safe (Go skips keys
// removed before they are reached) and the pruning is intentional: a ref that
// fails any matcher is dead and should not be scanned by later rounds.
func combine(accums map[postings.SectionRef]*accum, hits map[postings.SectionRef]*memory.Bitmap, first bool) {
	for ref, acc := range accums {
		hit := hits[ref]
		if hit == nil || hit.SetCount() == 0 {
			delete(accums, ref)
			continue
		}
		if first {
			acc.result = hit
			continue
		}
		if acc.result == nil {
			delete(accums, ref)
			continue
		}
		acc.result = acc.result.And(hit)
		if acc.result.SetCount() == 0 {
			delete(accums, ref)
		}
	}
}

// foldTimeOverlap folds overlap into acc's time-overlap bitmap, and the scanned
// rows' [min,max] bounds into acc's bounds, when that aggregated envelope
// overlaps the query window. has reports whether the scan visited any row.
func foldTimeOverlap(acc *accum, overlap *memory.Bitmap, min, max int64, has bool, startNanos, endNanos int64) {
	if !has {
		return
	}
	if max < startNanos || min > endNanos {
		return
	}
	memory.OrInto(&acc.timeOverlap, memory.OrEmpty(overlap))
	if !acc.hasTS {
		acc.minTS, acc.maxTS, acc.hasTS = min, max, true
		return
	}
	if min < acc.minTS {
		acc.minTS = min
	}
	if max > acc.maxTS {
		acc.maxTS = max
	}
}

// finalize applies time pruning and emits the SectionStreams for one section.
func (s *streamSelector) finalize(ref postings.SectionRef, acc *accum, startNanos, endNanos int64) (SectionStreams, bool) {
	result := acc.result
	if result == nil || result.SetCount() == 0 {
		return SectionStreams{}, false
	}
	if startNanos > 0 || endNanos > 0 {
		if acc.timeOverlap == nil {
			return SectionStreams{}, false
		}
		result = result.And(acc.timeOverlap)
		if result.SetCount() == 0 {
			return SectionStreams{}, false
		}
	}
	data, _ := result.BytesTrimmed()
	return SectionStreams{
		Section:        ref,
		StreamBitmap:   bytes.Clone(data),
		MinTimestamp:   acc.minTS,
		MaxTimestamp:   acc.maxTS,
		AmbiguousNames: s.ambiguousNames(acc),
	}, true
}

// admitSections applies blooms and collects ambiguous names in a single pass.
func (s *streamSelector) admitSections(ctx context.Context, sections []*postings.Section, accums map[postings.SectionRef]*accum) (map[postings.SectionRef]struct{}, map[postings.SectionRef]map[string]struct{}, error) {
	if len(s.equalPredicates) == 0 {
		return nil, nil, nil
	}

	streamLabelsByRef := make(map[postings.SectionRef]map[string]struct{}, len(accums))
	for ref, acc := range accums {
		streamLabelsByRef[ref] = acc.streamLabels
	}

	bloomHits := make([]map[postings.SectionRef]map[postings.PredicateValue]struct{}, len(sections))
	ambiguousHits := make([]map[postings.SectionRef]map[string]struct{}, len(sections))
	g, ctx := errgroup.WithContext(ctx)
	for i := range sections {
		g.Go(func() error {
			matched, ambiguous, err := postings.NewScanner(sections[i]).MatcherHits(ctx, s.equalPredicates)
			if err != nil {
				return err
			}
			bloomHits[i] = matched
			ambiguousHits[i] = ambiguous
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, nil, err
	}

	bloomHitsByRef := make(map[postings.SectionRef]map[postings.PredicateValue]struct{})
	for _, matched := range bloomHits {
		for ref, values := range matched {
			dst := bloomHitsByRef[ref]
			if dst == nil {
				dst = make(map[postings.PredicateValue]struct{})
				bloomHitsByRef[ref] = dst
			}
			for pv := range values {
				dst[pv] = struct{}{}
			}
		}
	}

	ambiguousNamesByRef := make(map[postings.SectionRef]map[string]struct{})
	for _, names := range ambiguousHits {
		for ref, set := range names {
			dst := ambiguousNamesByRef[ref]
			if dst == nil {
				dst = make(map[string]struct{})
				ambiguousNamesByRef[ref] = dst
			}
			for name := range set {
				dst[name] = struct{}{}
			}
		}
	}

	keep := make(map[postings.SectionRef]struct{})
	for ref := range streamLabelsByRef {
		if s.refAdmitsPredicates(combineLabels(streamLabelsByRef[ref], ambiguousNamesByRef[ref]), bloomHitsByRef[ref]) {
			keep[ref] = struct{}{}
		}
	}
	return keep, ambiguousNamesByRef, nil
}

// combineLabels combines the matcher-recorded stream labels with the ambiguous
// names without mutating either input.
func combineLabels(streamLabels, ambiguousNames map[string]struct{}) map[string]struct{} {
	if len(ambiguousNames) == 0 {
		return streamLabels
	}
	out := make(map[string]struct{}, len(streamLabels)+len(ambiguousNames))
	for name := range streamLabels {
		out[name] = struct{}{}
	}
	for name := range ambiguousNames {
		out[name] = struct{}{}
	}
	return out
}

// refAdmitsPredicates reports whether every equal-predicate is satisfied for a
// section: each predicate is either a stream label there or tests positive
// against a bloom there.
func (s *streamSelector) refAdmitsPredicates(streamLabels map[string]struct{}, bloomHits map[postings.PredicateValue]struct{}) bool {
	for _, p := range s.equalPredicates {
		if _, isStreamLabel := streamLabels[p.Name]; isStreamLabel {
			continue
		}
		if _, hit := bloomHits[postings.PredicateValue{Name: p.Name, Value: p.Value}]; !hit {
			return false
		}
	}
	return true
}

// partitionMatchers splits matchers into positive (value-selecting, seed the
// result) and empty-capable (also match streams lacking the label name). Mirrors
// LogQL's util.SplitFiltersAndMatchers, treating a `.*` regex as positive.
func (s *streamSelector) partitionMatchers() (positive, emptyCapable []*labels.Matcher) {
	for _, m := range s.matchers {
		if m == nil {
			continue
		}
		if isEmptyCapable(m) {
			emptyCapable = append(emptyCapable, m)
		} else {
			positive = append(positive, m)
		}
	}
	return positive, emptyCapable
}

// isEmptyCapable reports whether a matcher also matches streams lacking its label
// name. Mirrors util.SplitFiltersAndMatchers: a matcher that matches "" is
// empty-capable, except a `.*` regex which selects every stream and is positive.
func isEmptyCapable(m *labels.Matcher) bool {
	if m.Type == labels.MatchRegexp && m.Value == ".*" {
		return false
	}
	return m.Matches("")
}

// compileAll compiles every matcher once for reuse across sections.
func compileAll(matchers []*labels.Matcher) ([]postings.CompiledMatcher, error) {
	out := make([]postings.CompiledMatcher, 0, len(matchers))
	for _, m := range matchers {
		cm, err := postings.CompileMatcher(m)
		if err != nil {
			return nil, err
		}
		out = append(out, cm)
	}
	return out, nil
}

// ambiguousNames returns the equal-predicate names that are also stream labels in
// the section.
func (s *streamSelector) ambiguousNames(acc *accum) []string {
	var out []string
	for _, p := range s.equalPredicates {
		if _, ok := acc.streamLabels[p.Name]; ok {
			out = append(out, p.Name)
		}
	}
	return out
}
