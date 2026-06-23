package postings

import (
	"bytes"
	"context"
	"fmt"
	"time"

	"github.com/apache/arrow-go/v18/arrow/scalar"
	"github.com/prometheus/prometheus/model/labels"
	"golang.org/x/sync/errgroup"

	"github.com/grafana/loki/v3/pkg/memory"
)

// StreamResolver resolves SectionResults from one or more postings sections that
// match stream matchers, a time range, and structured-metadata predicates.
type StreamResolver struct {
	matchers        []*labels.Matcher
	equalPredicates []*labels.Matcher
	start, end      time.Time
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

// sectionKey identifies a logical section within an object. A single physical
// postings section interleaves rows for many logical sections, and a stream-ID
// bit is only meaningful within one logical section, so all accumulation is
// keyed on it.
type sectionKey struct {
	objectPath   string
	sectionIndex int64
}

// keyAccum holds the per-logical-section bitmaps. Bit position = stream ID.
// result is the running intersection of every matcher's hit; timeOverlap is the
// streams whose rows fall within the query window. The timestamp envelope spans
// the overlapping rows.
type keyAccum struct {
	result       *memory.Bitmap
	streamLabels map[string]struct{}
	timeOverlap  *memory.Bitmap
	minTS, maxTS int64
	hasTS        bool
}

// orInto unions src into the bitmap at dst. The result is always a fresh bitmap
// owned by dst, so callers never alias src's backing array.
func orInto(dst **memory.Bitmap, src *memory.Bitmap) {
	if *dst == nil {
		*dst = orEmpty(nil).Or(src)
	} else {
		*dst = (*dst).Or(src)
	}
}

// orEmpty returns b, or an empty bitmap when b is nil, so set-algebra operands
// are never nil.
func orEmpty(b *memory.Bitmap) *memory.Bitmap {
	if b == nil {
		return &memory.Bitmap{}
	}
	return b
}

// Resolve scans the already-opened sections and returns matching SectionResults,
// one per logical section that has at least one matching stream. The caller owns
// opening and closing the sections; Resolve opens and closes its own RowReaders.
func (r *StreamResolver) Resolve(ctx context.Context, sections []*Section) ([]SectionResult, error) {
	if len(r.matchers) == 0 {
		return nil, nil
	}

	positive, emptyCapable := r.partitionMatchers()
	if len(positive) == 0 {
		// LogQL requires at least one positive matcher; without one the stream
		// universe is unbounded and missing-label semantics cannot be resolved.
		return nil, fmt.Errorf("postings resolver requires at least one non-empty-capable matcher")
	}

	// Compile each positive matcher's value predicate once, shared across all
	// sections, so regex matchers are not recompiled per section.
	compiledPositive, err := compileMatchers(positive)
	if err != nil {
		return nil, err
	}

	startNanos, endNanos := r.start.UnixNano(), r.end.UnixNano()

	perSection := make([]map[sectionKey]*keyAccum, len(sections))
	g, ctx := errgroup.WithContext(ctx)
	for i := range sections {
		g.Go(func() error {
			accums, err := r.resolveSection(ctx, sections[i], compiledPositive, emptyCapable, startNanos, endNanos)
			if err != nil {
				return fmt.Errorf("resolving section: %w", err)
			}
			perSection[i] = accums
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}

	bloomSurvivors, err := r.bloomGate(ctx, sections, perSection)
	if err != nil {
		return nil, err
	}

	var out []SectionResult
	for _, accums := range perSection {
		for key, acc := range accums {
			if bloomSurvivors != nil {
				if _, ok := bloomSurvivors[key]; !ok {
					continue
				}
			}
			if result, ok := r.finalize(key, acc, startNanos, endNanos); ok {
				out = append(out, result)
			}
		}
	}
	return out, nil
}

// resolveSection scans one physical section and returns, per logical section
// key, the matching stream bitmap. Positive matchers are pushed into the scan
// and intersected; empty-capable matchers are applied via their absent-stream
// complement.
func (r *StreamResolver) resolveSection(
	ctx context.Context,
	sec *Section,
	positive []compiledMatcher,
	emptyCapable []*labels.Matcher,
	startNanos, endNanos int64,
) (map[sectionKey]*keyAccum, error) {
	kindCol := sectionColumn(sec, ColumnTypeKind)
	nameCol := sectionColumn(sec, ColumnTypeColumnName)
	valueCol := sectionColumn(sec, ColumnTypeLabelValue)
	if kindCol == nil || nameCol == nil || valueCol == nil {
		return nil, nil
	}

	matchers := make([]matcher, 0, len(positive)+len(emptyCapable))
	for _, cm := range positive {
		matchers = append(matchers, positiveMatcher{cm: cm})
	}
	for _, m := range emptyCapable {
		matchers = append(matchers, emptyCapableMatcher{m: m})
	}

	accums := make(map[sectionKey]*keyAccum)
	for i, m := range matchers {
		hits, err := m.scan(ctx, r, sec, kindCol, nameCol, valueCol, accums, startNanos, endNanos)
		if err != nil {
			return nil, err
		}
		combine(accums, hits, i == 0)
		if len(accums) == 0 {
			return nil, nil
		}
	}

	if err := r.recordPredicateStreamLabels(ctx, sec, kindCol, nameCol, accums); err != nil {
		return nil, err
	}
	return accums, nil
}

// combine folds a matcher's per-key hits into accums. On the first matcher it
// seeds each key's result; afterwards it ANDs. Keys with no hit, an empty hit,
// or no prior result are dropped, so the next matcher's scan sees only live
// keys. This pruning is what lets an empty-capable scan safely skip keys absent
// from accums.
func combine(accums map[sectionKey]*keyAccum, hits map[sectionKey]*memory.Bitmap, first bool) {
	for key, acc := range accums {
		hit := hits[key]
		if hit == nil || hit.SetCount() == 0 {
			delete(accums, key)
			continue
		}
		if first {
			acc.result = hit
			continue
		}
		if acc.result == nil {
			delete(accums, key)
			continue
		}
		acc.result = acc.result.And(hit)
		if acc.result.SetCount() == 0 {
			delete(accums, key)
		}
	}
}

// recordPredicateStreamLabels marks any equal-predicate name that exists as a
// stream label in each surviving key. Such predicates are resolved by label
// matching, so the bloom gate drops them and they surface as ambiguous names.
func (r *StreamResolver) recordPredicateStreamLabels(ctx context.Context, sec *Section, kindCol, nameCol *Column, accums map[sectionKey]*keyAccum) error {
	for _, p := range r.equalPredicates {
		pending := false
		for _, acc := range accums {
			if _, known := acc.streamLabels[p.Name]; !known {
				pending = true
				break
			}
		}
		if !pending {
			continue
		}
		keys, err := keysWithLabelName(ctx, sec, kindCol, nameCol, p.Name)
		if err != nil {
			return err
		}
		for key := range keys {
			if acc, ok := accums[key]; ok {
				acc.streamLabels[p.Name] = struct{}{}
			}
		}
	}
	return nil
}

// keysWithLabelName returns the logical section keys that contain a KindLabel
// row for the given label name.
func keysWithLabelName(ctx context.Context, sec *Section, kindCol, nameCol *Column, name string) (map[sectionKey]struct{}, error) {
	rr := NewRowReader(ctx, sec, labelNamePredicate(kindCol, nameCol, name))
	defer func() { _ = rr.Close() }()
	keys := make(map[sectionKey]struct{})
	for rr.Next() {
		row := rr.At()
		keys[keyOf(row)] = struct{}{}
	}
	return keys, rr.Err()
}

// eachRow scans sec with pred, invoking fn for every row carrying streams.
func (r *StreamResolver) eachRow(ctx context.Context, sec *Section, pred Predicate, fn func(Row)) error {
	rr := NewRowReader(ctx, sec, pred)
	defer func() { _ = rr.Close() }()

	for rr.Next() {
		row := rr.At()
		if len(row.StreamIDBitmap) == 0 {
			continue
		}
		fn(row)
	}
	return rr.Err()
}

// finalize applies time pruning and emits the SectionResult for one key.
func (r *StreamResolver) finalize(key sectionKey, acc *keyAccum, startNanos, endNanos int64) (SectionResult, bool) {
	result := acc.result
	if result == nil || result.SetCount() == 0 {
		return SectionResult{}, false
	}

	if startNanos > 0 || endNanos > 0 {
		if acc.timeOverlap == nil {
			return SectionResult{}, false
		}
		result = result.And(acc.timeOverlap)
		if result.SetCount() == 0 {
			return SectionResult{}, false
		}
	}

	data, _ := result.BytesTrimmed()
	return SectionResult{
		ObjectPath:     key.objectPath,
		SectionIndex:   key.sectionIndex,
		StreamBitmap:   bytes.Clone(data),
		MinTimestamp:   acc.minTS,
		MaxTimestamp:   acc.maxTS,
		AmbiguousNames: r.ambiguousNames(acc),
	}, true
}

// foldTimestampOverlap folds the row into the key's timestamp envelope and
// overlap bitmap when it falls within the query window. Null-bounded rows are
// kept.
func (acc *keyAccum) foldTimestampOverlap(bits *memory.Bitmap, row Row, startNanos, endNanos int64) {
	hasBounds := row.MinTimestamp != 0 || row.MaxTimestamp != 0
	if hasBounds && (row.MaxTimestamp < startNanos || row.MinTimestamp > endNanos) {
		return
	}
	orInto(&acc.timeOverlap, bits)
	if !acc.hasTS {
		acc.minTS, acc.maxTS, acc.hasTS = row.MinTimestamp, row.MaxTimestamp, true
		return
	}
	if row.MinTimestamp < acc.minTS {
		acc.minTS = row.MinTimestamp
	}
	if row.MaxTimestamp > acc.maxTS {
		acc.maxTS = row.MaxTimestamp
	}
}

// bloomGate returns the logical section keys whose structured-metadata blooms
// admit every equal-predicate, or nil when there are no predicates to gate on.
// Equal-predicates whose name is a stream label in a key are resolved by label
// matching, not blooms, so they are dropped per key. Blooms and the label rows
// they gate live in distinct physical sections sharing a logical key, so the
// gate scans every section and correlates on that key.
func (r *StreamResolver) bloomGate(ctx context.Context, sections []*Section, perSection []map[sectionKey]*keyAccum) (map[sectionKey]struct{}, error) {
	if len(r.equalPredicates) == 0 {
		return nil, nil
	}

	streamLabelsByKey := make(map[sectionKey]map[string]struct{})
	for _, accums := range perSection {
		for key, acc := range accums {
			streamLabelsByKey[key] = acc.streamLabels
		}
	}

	hits := make([]map[sectionKey]map[predicateValue]struct{}, len(sections))
	g, ctx := errgroup.WithContext(ctx)
	for i := range sections {
		g.Go(func() error {
			matched, err := bloomHitsInSection(ctx, sections[i], r.equalPredicates)
			if err != nil {
				return err
			}
			hits[i] = matched
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}

	bloomHitsByKey := make(map[sectionKey]map[predicateValue]struct{})
	for _, matched := range hits {
		for key, values := range matched {
			dst := bloomHitsByKey[key]
			if dst == nil {
				dst = make(map[predicateValue]struct{})
				bloomHitsByKey[key] = dst
			}
			for pv := range values {
				dst[pv] = struct{}{}
			}
		}
	}

	keep := make(map[sectionKey]struct{})
	for key, streamLabels := range streamLabelsByKey {
		if r.keyAdmitsPredicates(streamLabels, bloomHitsByKey[key]) {
			keep[key] = struct{}{}
		}
	}
	return keep, nil
}

// predicateValue identifies one equal-predicate by name and value so that two
// predicates on the same name but different values are gated independently.
type predicateValue struct {
	name  string
	value string
}

// keyAdmitsPredicates reports whether every equal-predicate is satisfied for a
// section key: each predicate is either a stream label there or tests positive
// against a bloom there.
func (r *StreamResolver) keyAdmitsPredicates(streamLabels map[string]struct{}, bloomHits map[predicateValue]struct{}) bool {
	for _, p := range r.equalPredicates {
		if _, isStreamLabel := streamLabels[p.Name]; isStreamLabel {
			continue
		}
		if _, hit := bloomHits[predicateValue{p.Name, p.Value}]; !hit {
			return false
		}
	}
	return true
}

// bloomHitsInSection scans a section's KindBloom rows and returns, per logical
// section key, the equal-predicate (name, value) pairs that test positive
// against a bloom on that predicate's column.
func bloomHitsInSection(ctx context.Context, sec *Section, predicates []*labels.Matcher) (map[sectionKey]map[predicateValue]struct{}, error) {
	kindCol := sectionColumn(sec, ColumnTypeKind)
	nameCol := sectionColumn(sec, ColumnTypeColumnName)
	bloomCol := sectionColumn(sec, ColumnTypeBloomFilter)
	if kindCol == nil || nameCol == nil || bloomCol == nil {
		return nil, nil
	}

	matched := make(map[sectionKey]map[predicateValue]struct{})
	for _, p := range predicates {
		rr := NewRowReader(ctx, sec,
			AndPredicate{
				Left: AndPredicate{
					Left:  EqualPredicate{Column: kindCol, Value: scalar.NewInt64Scalar(int64(KindBloom))},
					Right: EqualPredicate{Column: nameCol, Value: scalar.NewStringScalar(p.Name)},
				},
				Right: BloomMatchPredicate{Column: bloomCol, Value: []byte(p.Value)},
			},
		)
		for rr.Next() {
			row := rr.At()
			key := keyOf(row)
			pv := predicateValue{p.Name, p.Value}
			values := matched[key]
			if values == nil {
				values = make(map[predicateValue]struct{})
				matched[key] = values
			}
			values[pv] = struct{}{}
		}
		if err := rr.Err(); err != nil {
			_ = rr.Close()
			return nil, err
		}
		_ = rr.Close()
	}
	return matched, nil
}

// partitionMatchers splits the resolver's matchers into positive matchers (which
// select on a value and seed the result) and empty-capable matchers (which also
// match streams lacking the label name). The split mirrors LogQL's
// util.SplitFiltersAndMatchers, including treating a `.*` regex as positive.
func (r *StreamResolver) partitionMatchers() (positive, emptyCapable []*labels.Matcher) {
	for _, m := range r.matchers {
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

// ambiguousNames returns the equal-predicate names that are also stream labels
// observed in the key.
func (r *StreamResolver) ambiguousNames(acc *keyAccum) []string {
	var out []string
	for _, p := range r.equalPredicates {
		if _, ok := acc.streamLabels[p.Name]; ok {
			out = append(out, p.Name)
		}
	}
	return out
}

func ensureAccum(accums map[sectionKey]*keyAccum, key sectionKey) *keyAccum {
	acc, ok := accums[key]
	if !ok {
		acc = &keyAccum{streamLabels: make(map[string]struct{})}
		accums[key] = acc
	}
	return acc
}

func keyOf(row Row) sectionKey {
	return sectionKey{objectPath: row.ObjectPath, sectionIndex: row.SectionIndex}
}

// labelNamePredicate selects the KindLabel rows for a single label name.
func labelNamePredicate(kindCol, nameCol *Column, name string) Predicate {
	return AndPredicate{
		Left:  EqualPredicate{Column: kindCol, Value: scalar.NewInt64Scalar(int64(KindLabel))},
		Right: EqualPredicate{Column: nameCol, Value: scalar.NewStringScalar(name)},
	}
}

// compiledMatcher is a positive matcher with its regex compiled once, ready to
// be turned into a per-section scan predicate.
type compiledMatcher struct {
	matcher *labels.Matcher
	regex   *labels.FastRegexMatcher // non-nil only for regex matcher types
}

// compileMatchers compiles each matcher's regex once so it is shared across all
// sections rather than recompiled per section.
func compileMatchers(matchers []*labels.Matcher) ([]compiledMatcher, error) {
	out := make([]compiledMatcher, 0, len(matchers))
	for _, m := range matchers {
		cm := compiledMatcher{matcher: m}
		if m.Type == labels.MatchRegexp || m.Type == labels.MatchNotRegexp {
			re, err := labels.NewFastRegexMatcher(m.Value)
			if err != nil {
				return nil, fmt.Errorf("compiling regex %q: %w", m.Value, err)
			}
			cm.regex = re
		}
		out = append(out, cm)
	}
	return out, nil
}

// valuePredicate translates the matcher's value selection into a scan predicate
// over the label-value column, reusing the precompiled regex.
func (cm compiledMatcher) valuePredicate(valueCol *Column) Predicate {
	m := cm.matcher
	switch m.Type {
	case labels.MatchEqual:
		return EqualPredicate{Column: valueCol, Value: scalar.NewStringScalar(m.Value)}
	case labels.MatchNotEqual:
		return NotPredicate{Inner: EqualPredicate{Column: valueCol, Value: scalar.NewStringScalar(m.Value)}}
	case labels.MatchRegexp:
		return RegexMatchPredicate{Column: valueCol, Matcher: cm.regex}
	case labels.MatchNotRegexp:
		return NotPredicate{Inner: RegexMatchPredicate{Column: valueCol, Matcher: cm.regex}}
	default:
		return FalsePredicate{}
	}
}

// matcher produces, per logical section key, the stream bitmap it selects in a
// section, folding stream labels and the timestamp envelope into accums as a
// side effect. Positive and empty-capable matchers differ only in how the
// bitmap is computed; both are combined identically by combine.
type matcher interface {
	scan(ctx context.Context, r *StreamResolver, sec *Section, kindCol, nameCol, valueCol *Column, accums map[sectionKey]*keyAccum, startNanos, endNanos int64) (map[sectionKey]*memory.Bitmap, error)
}

// positiveMatcher pushes its value predicate into the scan and selects the union
// of matching rows' stream bitmaps per key.
type positiveMatcher struct{ cm compiledMatcher }

func (pm positiveMatcher) scan(ctx context.Context, r *StreamResolver, sec *Section, kindCol, nameCol, valueCol *Column, accums map[sectionKey]*keyAccum, startNanos, endNanos int64) (map[sectionKey]*memory.Bitmap, error) {
	name := pm.cm.matcher.Name
	pred := AndPredicate{
		Left:  labelNamePredicate(kindCol, nameCol, name),
		Right: pm.cm.valuePredicate(valueCol),
	}

	hits := make(map[sectionKey]*memory.Bitmap)
	err := r.eachRow(ctx, sec, pred, func(row Row) {
		key := keyOf(row)
		acc := ensureAccum(accums, key)
		acc.streamLabels[name] = struct{}{}
		bits := bitmapOf(row)
		existing := hits[key]
		orInto(&existing, bits)
		hits[key] = existing
		acc.foldTimestampOverlap(bits, row, startNanos, endNanos)
	})
	return hits, err
}

// emptyCapableMatcher selects rows whose value matches, unioned with streams
// that lack the label name. "Lacks the name" is the complement of presence
// within the running result; result is read as input and never modified here.
type emptyCapableMatcher struct{ m *labels.Matcher }

func (em emptyCapableMatcher) scan(ctx context.Context, r *StreamResolver, sec *Section, kindCol, nameCol, _ *Column, accums map[sectionKey]*keyAccum, startNanos, endNanos int64) (map[sectionKey]*memory.Bitmap, error) {
	present := make(map[sectionKey]*memory.Bitmap)
	positive := make(map[sectionKey]*memory.Bitmap)
	err := r.eachRow(ctx, sec, labelNamePredicate(kindCol, nameCol, em.m.Name), func(row Row) {
		key := keyOf(row)
		acc, ok := accums[key]
		if !ok {
			return // no positive matcher seeded this key; it cannot survive
		}
		acc.streamLabels[em.m.Name] = struct{}{}
		bits := bitmapOf(row)
		p := present[key]
		orInto(&p, bits)
		present[key] = p
		if em.m.Matches(row.LabelValue) {
			h := positive[key]
			orInto(&h, bits)
			positive[key] = h
		}
		acc.foldTimestampOverlap(bits, row, startNanos, endNanos)
	})
	if err != nil {
		return nil, err
	}

	hits := make(map[sectionKey]*memory.Bitmap)
	for key, acc := range accums {
		missing := acc.result.AndNot(orEmpty(present[key]))
		hit := positive[key]
		orInto(&hit, missing)
		hits[key] = orEmpty(hit)
	}
	return hits, nil
}

func bitmapOf(row Row) *memory.Bitmap {
	b := memory.BitmapFrom(row.StreamIDBitmap, len(row.StreamIDBitmap)*8, 0)
	return &b
}

func sectionColumn(sec *Section, ct ColumnType) *Column {
	for _, c := range sec.Columns() {
		if c.Type == ct {
			return c
		}
	}
	return nil
}

// isEmptyCapable reports whether a matcher also matches streams lacking its label
// name. It mirrors util.SplitFiltersAndMatchers: a matcher that matches "" is
// empty-capable, except a `.*` regex which selects every stream and is treated
// as positive.
func isEmptyCapable(m *labels.Matcher) bool {
	if m.Type == labels.MatchRegexp && m.Value == ".*" {
		return false
	}
	return m.Matches("")
}
