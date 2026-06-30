package postings

import (
	"context"
	"fmt"

	"github.com/apache/arrow-go/v18/arrow/scalar"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/compute"
	"github.com/grafana/loki/v3/pkg/memory"
	"github.com/grafana/loki/v3/pkg/xcap"
)

// timeBounds is the timestamp bounds over all rows a scan visited. MinNS and
// MaxNS are unix nanos and only meaningful once Has is true.
type timeBounds struct {
	MinNS, MaxNS int64
	Has          bool
}

// MatchedStreams is one logs section's result from a name and value scan.
type MatchedStreams struct {
	Matched *memory.Bitmap
	timeBounds
}

// LabelStreams is one logs section's result from the name-only scan.
type LabelStreams struct {
	Present *memory.Bitmap
	Matched *memory.Bitmap
	timeBounds
}

// Scanner scans one postings Section. It holds no mutable state — each scan
// uses its own reader and is safe to share across goroutines.
type Scanner struct {
	sec *Section
}

// NewScanner returns a Scanner bound to sec.
func NewScanner(sec *Section) *Scanner { return &Scanner{sec: sec} }

// MatchLabels scans the section once for all cms (kind=Label AND OR-of per
// matcher (name=cm.Name AND cm.valuePredicate)), attributing each row back to
// every matcher it satisfies. Returns, per logs section, one MatchedStreams per
// input matcher (indexed positionally), so the caller can intersect across
// matchers. Returns nil when the section lacks the required columns or cms is
// empty.
func (s *Scanner) MatchLabels(ctx context.Context, cms []CompiledMatcher) (map[SectionRef][]MatchedStreams, error) {
	kindCol := sectionColumn(s.sec, ColumnTypeKind)
	nameCol := sectionColumn(s.sec, ColumnTypeColumnName)
	valueCol := sectionColumn(s.sec, ColumnTypeLabelValue)
	if kindCol == nil || nameCol == nil || valueCol == nil {
		return nil, nil
	}
	if len(cms) == 0 {
		return nil, nil
	}

	byName := make(map[string][]int, len(cms))
	for i, cm := range cms {
		n := cm.matcher.Name
		byName[n] = append(byName[n], i)
	}

	pred := matchLabelsPredicate(kindCol, nameCol, valueCol, cms)

	out := make(map[SectionRef][]MatchedStreams)
	err := s.eachRow(ctx, pred, func(row Row) {
		cands := byName[row.ColumnName]
		if len(cands) == 0 {
			return
		}
		ref := refOf(row)
		perMatcher := out[ref]
		if perMatcher == nil {
			perMatcher = make([]MatchedStreams, len(cms))
			out[ref] = perMatcher
		}
		bits := memory.BitmapFrom(row.StreamIDBitmap, len(row.StreamIDBitmap)*8, 0)
		for _, i := range cands {
			if !cms[i].matcher.Matches(row.LabelValue) {
				continue
			}
			perMatcher[i].Matched = unionStreams(perMatcher[i].Matched, bits)
			perMatcher[i].widen(row)
		}
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// matchLabelsPredicate folds cms into kind=Label AND OR-of per matcher
// (name=cm.Name AND cm.valuePredicate).
func matchLabelsPredicate(kindCol, nameCol, valueCol *Column, cms []CompiledMatcher) Predicate {
	var branches Predicate
	for _, cm := range cms {
		branch := AndPredicate{
			Left:  EqualPredicate{Column: nameCol, Value: scalar.NewStringScalar(cm.matcher.Name)},
			Right: cm.valuePredicate(valueCol),
		}
		if branches == nil {
			branches = branch
			continue
		}
		branches = OrPredicate{Left: branches, Right: branch}
	}
	return AndPredicate{
		Left:  EqualPredicate{Column: kindCol, Value: scalar.NewInt64Scalar(int64(KindLabel))},
		Right: branches,
	}
}

// LabelStreams scans the section once for all cms' label names only (no value
// pushdown), attributing each row to every matcher sharing its column name.
// Returns, per logs section, one LabelStreams per input matcher (indexed
// positionally).
func (s *Scanner) LabelStreams(ctx context.Context, cms []CompiledMatcher) (map[SectionRef][]LabelStreams, error) {
	kindCol := sectionColumn(s.sec, ColumnTypeKind)
	nameCol := sectionColumn(s.sec, ColumnTypeColumnName)
	if kindCol == nil || nameCol == nil {
		return nil, nil
	}
	if len(cms) == 0 {
		return nil, nil
	}

	byName := make(map[string][]int, len(cms))
	for i, cm := range cms {
		byName[cm.matcher.Name] = append(byName[cm.matcher.Name], i)
	}

	out := make(map[SectionRef][]LabelStreams)
	err := s.eachRow(ctx, labelNamesPredicate(kindCol, nameCol, byName), func(row Row) {
		cands := byName[row.ColumnName]
		if len(cands) == 0 {
			return
		}
		ref := refOf(row)
		perMatcher := out[ref]
		if perMatcher == nil {
			perMatcher = make([]LabelStreams, len(cms))
			out[ref] = perMatcher
		}
		bits := memory.BitmapFrom(row.StreamIDBitmap, len(row.StreamIDBitmap)*8, 0)
		for _, i := range cands {
			perMatcher[i].Present = unionStreams(perMatcher[i].Present, bits)
			if cms[i].matcher.Matches(row.LabelValue) {
				perMatcher[i].Matched = unionStreams(perMatcher[i].Matched, bits)
			}
			perMatcher[i].widen(row)
		}
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// labelNamesPredicate selects KindLabel rows whose name is any of the matcher
// names. names is the matcher-by-name index; its keys are the distinct names.
func labelNamesPredicate(kindCol, nameCol *Column, names map[string][]int) Predicate {
	values := make([]scalar.Scalar, 0, len(names))
	for name := range names {
		values = append(values, scalar.NewStringScalar(name))
	}
	return AndPredicate{
		Left:  EqualPredicate{Column: kindCol, Value: scalar.NewInt64Scalar(int64(KindLabel))},
		Right: InPredicate{Column: nameCol, Values: values},
	}
}

func (s *Scanner) eachRow(ctx context.Context, pred Predicate, fn func(Row)) error {
	rr := NewRowReader(ctx, s.sec, pred)
	defer rr.Close()
	for rr.Next() {
		row := rr.At()
		if len(row.StreamIDBitmap) == 0 {
			continue
		}
		fn(row)
	}
	return rr.Err()
}

// widen extends the bounds to include row's [MinTimestamp,MaxTimestamp].
func (b *timeBounds) widen(row Row) {
	if !b.Has {
		b.MinNS, b.MaxNS, b.Has = row.MinTimestamp, row.MaxTimestamp, true
		return
	}
	if row.MinTimestamp < b.MinNS {
		b.MinNS = row.MinTimestamp
	}
	if row.MaxTimestamp > b.MaxNS {
		b.MaxNS = row.MaxTimestamp
	}
}

func refOf(row Row) SectionRef {
	return SectionRef{ObjectPath: row.ObjectPath, SectionIndex: row.SectionIndex}
}

// unionStreams returns the bitwise OR of acc and bits, treating a nil acc as
// empty. Per-row stream bitmaps vary in length, so the shorter operand is
// zero-extended to the longer before delegating to compute.Or, which requires
// equal-length operands.
//
// The result uses the nil (GC-managed) allocator on purpose: it escapes the
// scan in the returned per-section map, so it must not be backed by a
// reclaimable memory.Allocator region that the scan would free.
func unionStreams(acc *memory.Bitmap, bits memory.Bitmap) *memory.Bitmap {
	if acc == nil {
		acc = &memory.Bitmap{}
	}

	left, right := *acc, bits
	if n := max(left.Len(), right.Len()); left.Len() != right.Len() {
		left = extendBitmap(left, n)
		right = extendBitmap(right, n)
	}

	result, err := compute.Or(
		nil,
		columnar.NewBool(left, memory.Bitmap{}),
		columnar.NewBool(right, memory.Bitmap{}),
		memory.Bitmap{},
	)
	if err != nil {
		panic("unionStreams: " + err.Error())
	}
	out := result.(*columnar.Bool).Values()
	return &out
}

// extendBitmap returns a length-n copy of b with the trailing bits cleared,
// leaving b unmodified.
func extendBitmap(b memory.Bitmap, n int) memory.Bitmap {
	out := memory.NewBitmap(nil, n)
	out.AppendBitmap(b)
	out.AppendCount(false, n-b.Len())
	return out
}

// MatcherHits scans the section against [matchers]. The first return is the
// per-section (name,value) bloom hits. The second is the per-section set of
// matcher names that occur as a stream label. Returns nil maps when the section
// lacks the required columns.
func (s *Scanner) MatcherHits(ctx context.Context, matchers []*labels.Matcher) (map[SectionRef]map[PredicateValue]struct{}, map[SectionRef]map[string]struct{}, error) {
	kindCol := sectionColumn(s.sec, ColumnTypeKind)
	nameCol := sectionColumn(s.sec, ColumnTypeColumnName)
	bloomCol := sectionColumn(s.sec, ColumnTypeBloomFilter)
	if kindCol == nil || nameCol == nil || bloomCol == nil {
		return nil, nil, nil
	}

	// the bloom match predicate assumes no two matchers will match on the same
	// name, which is guaranteed upstream by only passing equality matchers.
	byName := make(map[string]*labels.Matcher, len(matchers))
	for _, p := range matchers {
		if existing, dup := byName[p.Name]; dup {
			return nil, nil, fmt.Errorf("MatcherHits: duplicate equal-predicate name %q (%q, %q); MatcherHits assumes distinct names",
				p.Name, existing.Value, p.Value)
		}
		byName[p.Name] = p
	}

	matched := make(map[SectionRef]map[PredicateValue]struct{})
	var bloomRowsRead int64
	if pred := s.bloomMatchPredicate(kindCol, nameCol, bloomCol, matchers); pred != nil {
		br := NewRowReader(ctx, s.sec, pred)
		for br.Next() {
			bloomRowsRead++
			row := br.At()
			p := byName[row.ColumnName]
			if p == nil {
				continue
			}
			ref := refOf(row)
			pv := PredicateValue{Name: p.Name, Value: p.Value}
			vals := matched[ref]
			if vals == nil {
				vals = make(map[PredicateValue]struct{})
				matched[ref] = vals
			}
			vals[pv] = struct{}{}
		}
		if err := br.Err(); err != nil {
			_ = br.Close()
			return nil, nil, err
		}
		_ = br.Close()
	}
	xcap.RegionFromContext(ctx).Record(StatPostingsBloomRowsRead.Observe(bloomRowsRead))

	// matchers are predicates from outside the stream selector, so a name may
	// resolve to either a stream label or a parsed/metadata label. Report which
	// ones occur as stream labels here so the caller can resolve the collision.
	matchersInLabelNames, err := s.labelNameHits(ctx, kindCol, nameCol, matchers)
	if err != nil {
		return nil, nil, err
	}
	return matched, matchersInLabelNames, nil
}

// bloomMatchPredicate folds matchers into a single OR of per-matcher
// (kind=Bloom AND name=p.Name AND bloom~p.Value) branches. Returns nil when
// there are no matchers.
func (s *Scanner) bloomMatchPredicate(kindCol, nameCol, bloomCol *Column, matchers []*labels.Matcher) Predicate {
	var pred Predicate
	for _, p := range matchers {
		branch := AndPredicate{
			Left: AndPredicate{
				Left:  EqualPredicate{Column: kindCol, Value: scalar.NewInt64Scalar(int64(KindBloom))},
				Right: EqualPredicate{Column: nameCol, Value: scalar.NewStringScalar(p.Name)},
			},
			Right: BloomMatchPredicate{Column: bloomCol, Value: []byte(p.Value)},
		}
		if pred == nil {
			pred = branch
			continue
		}
		pred = OrPredicate{Left: pred, Right: branch}
	}
	return pred
}

// labelNameHits scans the section once for all matcher names (kind=Label AND
// name IN names), returning per section the matcher names that appear as a
// stream label there.
func (s *Scanner) labelNameHits(ctx context.Context, kindCol, nameCol *Column, matchers []*labels.Matcher) (map[SectionRef]map[string]struct{}, error) {
	names := make([]scalar.Scalar, 0, len(matchers))
	wanted := make(map[string]struct{}, len(matchers))
	for _, p := range matchers {
		if _, ok := wanted[p.Name]; ok {
			continue
		}
		wanted[p.Name] = struct{}{}
		names = append(names, scalar.NewStringScalar(p.Name))
	}

	lr := NewRowReader(ctx, s.sec, AndPredicate{
		Left:  EqualPredicate{Column: kindCol, Value: scalar.NewInt64Scalar(int64(KindLabel))},
		Right: InPredicate{Column: nameCol, Values: names},
	})
	defer lr.Close()

	hits := make(map[SectionRef]map[string]struct{})
	for lr.Next() {
		row := lr.At()
		ref := refOf(row)
		set := hits[ref]
		if set == nil {
			set = make(map[string]struct{})
			hits[ref] = set
		}
		set[row.ColumnName] = struct{}{}
	}
	return hits, lr.Err()
}

// sectionColumn returns the section's column of the given type, or nil if absent.
func sectionColumn(sec *Section, ct ColumnType) *Column {
	for _, c := range sec.Columns() {
		if c.Type == ct {
			return c
		}
	}
	return nil
}
