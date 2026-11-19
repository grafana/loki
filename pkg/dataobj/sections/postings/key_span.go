package postings

import (
	"fmt"

	"github.com/apache/arrow-go/v18/arrow/scalar"
)

// KeySpan is the half-open range [start, end) of posting keys. A span without
// a start holds every key below its end, and a span without an end holds every
// key from its start. The zero value holds every key.
//
// A KeySpan selects keys, for example the keys that one task merges. A
// [KeyRange] bounds the keys that an object holds.
type KeySpan struct {
	start, end       Key
	hasStart, hasEnd bool
}

// NewKeySpan returns the span [start, end). A nil bound leaves that side of the
// span open. It returns an error if start does not sort below end, or if the
// kind of a bound does not fit in one byte.
func NewKeySpan(start, end *Key) (KeySpan, error) {
	var span KeySpan
	for _, bound := range []*Key{start, end} {
		if bound != nil && (bound.Kind < 0 || bound.Kind > 255) {
			return KeySpan{}, fmt.Errorf("posting key span: kind %d does not fit in one byte", bound.Kind)
		}
	}
	if start != nil {
		span.start, span.hasStart = *start, true
	}
	if end != nil {
		span.end, span.hasEnd = *end, true
	}
	if span.hasStart && span.hasEnd && span.start.Compare(span.end) >= 0 {
		return KeySpan{}, fmt.Errorf("posting key span: start %v does not sort below end %v", span.start, span.end)
	}
	return span, nil
}

// DecodeKeySpan returns the span whose bounds have the encodings start and
// end, which [KeySpan.Encode] returned. An empty encoding is an open bound.
func DecodeKeySpan(start, end []byte) (KeySpan, error) {
	var startKey, endKey *Key
	if len(start) > 0 {
		key, err := decodeKey(start)
		if err != nil {
			return KeySpan{}, fmt.Errorf("decoding start: %w", err)
		}
		startKey = &key
	}
	if len(end) > 0 {
		key, err := decodeKey(end)
		if err != nil {
			return KeySpan{}, fmt.Errorf("decoding end: %w", err)
		}
		endKey = &key
	}
	return NewKeySpan(startKey, endKey)
}

// Encode returns the encodings of the bounds of s. An open bound has an empty
// encoding.
func (s KeySpan) Encode() (start, end []byte) {
	if s.hasStart {
		start = s.start.appendEncoding(nil)
	}
	if s.hasEnd {
		end = s.end.appendEncoding(nil)
	}
	return start, end
}

// String returns s as "[start, end)". An open bound shows as "open".
func (s KeySpan) String() string {
	bound := func(key Key, ok bool) string {
		if !ok {
			return "open"
		}
		return fmt.Sprintf("%v", key)
	}
	return fmt.Sprintf("[%s, %s)", bound(s.start, s.hasStart), bound(s.end, s.hasEnd))
}

// IsAll reports whether s holds every key.
func (s KeySpan) IsAll() bool { return !s.hasStart && !s.hasEnd }

// Start returns the smallest key of s and true. It returns false if s has no
// start.
func (s KeySpan) Start() (Key, bool) { return s.start, s.hasStart }

// End returns the smallest key above s and true. It returns false if s has no
// end.
func (s KeySpan) End() (Key, bool) { return s.end, s.hasEnd }

// Contains reports whether key is in s.
func (s KeySpan) Contains(key Key) bool {
	return !s.startsAfter(key) && !s.endsAtOrBelow(key)
}

// Overlaps reports whether s holds a key of the closed range [minKey, maxKey].
// minKey must not sort above maxKey.
func (s KeySpan) Overlaps(minKey, maxKey Key) bool {
	return !s.startsAfter(maxKey) && !s.endsAtOrBelow(minKey)
}

func (s KeySpan) startsAfter(key Key) bool { return s.hasStart && key.Compare(s.start) < 0 }

func (s KeySpan) endsAtOrBelow(key Key) bool { return s.hasEnd && key.Compare(s.end) >= 0 }

// HoldsRange reports whether every key of r is in s. The zero KeyRange has no
// bounds, so only a span that holds every key holds it.
func (s KeySpan) HoldsRange(r KeyRange) bool {
	if r.IsZero() {
		return s.IsAll()
	}
	return s.Contains(r.Min()) && s.Contains(r.Max())
}

// Predicate returns a predicate over the kind, column_name and label_value
// columns of section that keeps exactly the rows whose posting key is in s. A
// null label value sorts as the empty string, as in Key. The predicate also
// lets a reader skip pages outside s. It returns nil if s holds every key, and
// an error if section lacks one of the columns.
func (s KeySpan) Predicate(section *Section) (Predicate, error) {
	if s.IsAll() {
		return nil, nil
	}
	columns := make(map[ColumnType]*Column, 3)
	for _, column := range section.Columns() {
		switch column.Type {
		case ColumnTypeKind, ColumnTypeColumnName, ColumnTypeLabelValue:
			columns[column.Type] = column
		}
	}
	for _, columnType := range []ColumnType{ColumnTypeKind, ColumnTypeColumnName, ColumnTypeLabelValue} {
		if columns[columnType] == nil {
			return nil, fmt.Errorf("postings section lacks the %s column", columnType)
		}
	}
	kind, name, value := columns[ColumnTypeKind], columns[ColumnTypeColumnName], columns[ColumnTypeLabelValue]

	// fromKey keeps the keys at or above key, and belowKey keeps the keys
	// below key. Both compare kind, then column name, then label value, as
	// Key.Compare does.
	fromKey := func(key Key) Predicate {
		return OrPredicate{
			Left: GreaterThanPredicate{Column: kind, Value: scalar.NewInt64Scalar(int64(key.Kind))},
			Right: AndPredicate{
				Left: EqualPredicate{Column: kind, Value: scalar.NewInt64Scalar(int64(key.Kind))},
				Right: OrPredicate{
					Left: GreaterThanPredicate{Column: name, Value: scalar.NewStringScalar(key.ColumnName)},
					Right: AndPredicate{
						Left:  EqualPredicate{Column: name, Value: scalar.NewStringScalar(key.ColumnName)},
						Right: NotPredicate{Inner: LessThanPredicate{Column: value, Value: scalar.NewStringScalar(key.LabelValue)}},
					},
				},
			},
		}
	}
	belowKey := func(key Key) Predicate {
		return OrPredicate{
			Left: LessThanPredicate{Column: kind, Value: scalar.NewInt64Scalar(int64(key.Kind))},
			Right: AndPredicate{
				Left: EqualPredicate{Column: kind, Value: scalar.NewInt64Scalar(int64(key.Kind))},
				Right: OrPredicate{
					Left: LessThanPredicate{Column: name, Value: scalar.NewStringScalar(key.ColumnName)},
					Right: AndPredicate{
						Left:  EqualPredicate{Column: name, Value: scalar.NewStringScalar(key.ColumnName)},
						Right: LessThanPredicate{Column: value, Value: scalar.NewStringScalar(key.LabelValue)},
					},
				},
			},
		}
	}

	switch {
	case s.hasStart && s.hasEnd:
		return AndPredicate{Left: fromKey(s.start), Right: belowKey(s.end)}, nil
	case s.hasStart:
		return fromKey(s.start), nil
	default:
		return belowKey(s.end), nil
	}
}
