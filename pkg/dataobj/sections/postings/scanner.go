package postings

import (
	"context"

	"github.com/apache/arrow-go/v18/arrow/scalar"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/memory"
	"github.com/grafana/loki/v3/pkg/xcap"
)

// timeBounds is the timestamp envelope over the rows a scan visited. Min and Max
// are unix nanos and only meaningful once Has is true.
type timeBounds struct {
	Min, Max int64
	Has      bool
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

// MatchLabel scans the section's label rows for cm's label name AND value.
// Returns per logs section the matched-stream bitmap and the timestamp envelope.
// Returns nil when the section lacks the required columns.
func (s *Scanner) MatchLabel(ctx context.Context, cm CompiledMatcher) (map[SectionRef]MatchedStreams, error) {
	kindCol := sectionColumn(s.sec, ColumnTypeKind)
	nameCol := sectionColumn(s.sec, ColumnTypeColumnName)
	valueCol := sectionColumn(s.sec, ColumnTypeLabelValue)
	if kindCol == nil || nameCol == nil || valueCol == nil {
		return nil, nil
	}

	pred := AndPredicate{
		Left:  labelNamePredicate(kindCol, nameCol, cm.matcher.Name),
		Right: cm.valuePredicate(valueCol),
	}

	out := make(map[SectionRef]MatchedStreams)
	err := s.eachRow(ctx, pred, func(row Row) {
		ref := refOf(row)
		ms := out[ref]
		bits := memory.BitmapFrom(row.StreamIDBitmap, len(row.StreamIDBitmap)*8, 0)
		memory.OrInto(&ms.Matched, &bits)
		ms.widen(row)
		out[ref] = ms
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// LabelStreams scans the section's label rows for cm's label name only (no value
// pushdown), returning per logs section the present streams (every stream
// carrying the name), the value-matched subset, and the timestamp envelope.
// Returns nil when the section lacks the required columns.
func (s *Scanner) LabelStreams(ctx context.Context, cm CompiledMatcher) (map[SectionRef]LabelStreams, error) {
	kindCol := sectionColumn(s.sec, ColumnTypeKind)
	nameCol := sectionColumn(s.sec, ColumnTypeColumnName)
	valueCol := sectionColumn(s.sec, ColumnTypeLabelValue)
	if kindCol == nil || nameCol == nil || valueCol == nil {
		return nil, nil
	}

	out := make(map[SectionRef]LabelStreams)
	err := s.eachRow(ctx, labelNamePredicate(kindCol, nameCol, cm.matcher.Name), func(row Row) {
		ref := refOf(row)
		ls := out[ref]
		bits := memory.BitmapFrom(row.StreamIDBitmap, len(row.StreamIDBitmap)*8, 0)
		memory.OrInto(&ls.Present, &bits)
		if cm.matcher.Matches(row.LabelValue) {
			memory.OrInto(&ls.Matched, &bits)
		}
		ls.widen(row)
		out[ref] = ls
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Scanner) eachRow(ctx context.Context, pred Predicate, fn func(Row)) error {
	rr := NewRowReader(ctx, s.sec, pred)
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

// widen extends the bounds to include row's [MinTimestamp,MaxTimestamp].
func (b *timeBounds) widen(row Row) {
	if !b.Has {
		b.Min, b.Max, b.Has = row.MinTimestamp, row.MaxTimestamp, true
		return
	}
	if row.MinTimestamp < b.Min {
		b.Min = row.MinTimestamp
	}
	if row.MaxTimestamp > b.Max {
		b.Max = row.MaxTimestamp
	}
}

func refOf(row Row) SectionRef {
	return SectionRef{ObjectPath: row.ObjectPath, SectionIndex: row.SectionIndex}
}

// MatcherHits scans the section against [matchers]. The first return is the
// per-section (name,value) bloom hits; the second is the per-section set of
// names resolved label matching on label name only. Returns nil maps when the
// section lacks the required columns.
//
// The label-name resolution for every matcher is collected in a single scan
// (kind=Label AND name IN names). The bloom hits keep one scan per matcher
// because each needs its own value pushed down to the bloom filter.
func (s *Scanner) MatcherHits(ctx context.Context, matchers []*labels.Matcher) (map[SectionRef]map[PredicateValue]struct{}, map[SectionRef]map[string]struct{}, error) {
	kindCol := sectionColumn(s.sec, ColumnTypeKind)
	nameCol := sectionColumn(s.sec, ColumnTypeColumnName)
	bloomCol := sectionColumn(s.sec, ColumnTypeBloomFilter)
	if kindCol == nil || nameCol == nil || bloomCol == nil {
		return nil, nil, nil
	}

	matched := make(map[SectionRef]map[PredicateValue]struct{})
	var bloomRowsRead int64
	for _, p := range matchers {
		br := NewRowReader(ctx, s.sec,
			AndPredicate{
				Left: AndPredicate{
					Left:  EqualPredicate{Column: kindCol, Value: scalar.NewInt64Scalar(int64(KindBloom))},
					Right: EqualPredicate{Column: nameCol, Value: scalar.NewStringScalar(p.Name)},
				},
				Right: BloomMatchPredicate{Column: bloomCol, Value: []byte(p.Value)},
			},
		)
		for br.Next() {
			bloomRowsRead++
			ref := refOf(br.At())
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
	xcap.RegionFromContext(ctx).Record(xcap.StatPostingsBloomRowsRead.Observe(bloomRowsRead))

	ambiguous, err := s.labelNameHits(ctx, kindCol, nameCol, matchers)
	if err != nil {
		return nil, nil, err
	}
	return matched, ambiguous, nil
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
	defer func() { _ = lr.Close() }()

	ambiguous := make(map[SectionRef]map[string]struct{})
	for lr.Next() {
		row := lr.At()
		ref := refOf(row)
		set := ambiguous[ref]
		if set == nil {
			set = make(map[string]struct{})
			ambiguous[ref] = set
		}
		set[row.ColumnName] = struct{}{}
	}
	return ambiguous, lr.Err()
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

// labelNamePredicate selects the KindLabel rows for a single label name.
func labelNamePredicate(kindCol, nameCol *Column, name string) Predicate {
	return AndPredicate{
		Left:  EqualPredicate{Column: kindCol, Value: scalar.NewInt64Scalar(int64(KindLabel))},
		Right: EqualPredicate{Column: nameCol, Value: scalar.NewStringScalar(name)},
	}
}
