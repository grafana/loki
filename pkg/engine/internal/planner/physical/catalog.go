package physical

import (
	"fmt"
	"time"

	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
)

var (
	binOpToMatchTypeMapping = map[types.BinaryOp]labels.MatchType{
		types.BinaryOpEq:         labels.MatchEqual,
		types.BinaryOpNeq:        labels.MatchNotEqual,
		types.BinaryOpMatchRe:    labels.MatchRegexp,
		types.BinaryOpNotMatchRe: labels.MatchNotRegexp,
	}

	noShard = ShardInfo{Shard: 0, Of: 1}
)

type ShardInfo struct {
	Shard uint32
	Of    uint32
}

func (s ShardInfo) String() string {
	return fmt.Sprintf("%d_of_%d", s.Shard, s.Of)
}

// TimeRange describes a time range where Start and End are inclusive.
type TimeRange struct {
	Start time.Time
	End   time.Time
}

func newTimeRange(start, end time.Time) (TimeRange, error) {
	if end.Before(start) {
		return TimeRange{}, fmt.Errorf("cannot have end time (%v) before start time (%v)", end, start)
	}
	return TimeRange{Start: start, End: end}, nil
}

func (t *TimeRange) Overlaps(secondRange TimeRange) bool {
	return !t.Start.After(secondRange.End) && !secondRange.Start.After(t.End)
}

func (t *TimeRange) IsZero() bool {
	return t.Start.IsZero() && t.End.IsZero()
}

func (t *TimeRange) Merge(secondRange TimeRange) TimeRange {
	var out TimeRange
	if secondRange.Start.IsZero() || !t.Start.IsZero() && t.Start.Before(secondRange.Start) {
		out.Start = t.Start
	} else {
		out.Start = secondRange.Start
	}
	if secondRange.End.IsZero() || !t.End.IsZero() && t.End.After(secondRange.End) {
		out.End = t.End
	} else {
		out.End = secondRange.End
	}
	return out
}

type DataObjSections struct {
	Location  DataObjLocation
	Streams   []int64
	Sections  []int
	TimeRange TimeRange
}

// Catalog is an interface that provides methods for interacting with
// storage metadata. In traditional database systems there are system tables
// providing this information (e.g. pg_catalog, ...) whereas in Loki there
// is the Metastore.
type Catalog interface {
	// ResolveDataObjSections returns a list of
	// DataObjSections objects, which each include:
	// a data object path, a list of stream IDs for
	// each data object path, a list of sections for
	// each data object path, and a time range.
	ResolveDataObjSections(Expression, []Expression, ShardInfo, time.Time, time.Time) ([]DataObjSections, error)
}

type MetastoreSectionsResolver func(time.Time, time.Time, []*labels.Matcher, []*labels.Matcher) ([]*metastore.DataobjSectionDescriptor, error)

// MetastoreCatalog is the default implementation of [Catalog].
type MetastoreCatalog struct {
	sectionsResolver MetastoreSectionsResolver
}

// NewMetastoreCatalog creates a new instance of [MetastoreCatalog] for query planning.
func NewMetastoreCatalog(sectionsResolver MetastoreSectionsResolver) *MetastoreCatalog {
	return &MetastoreCatalog{
		sectionsResolver: sectionsResolver,
	}
}

// ResolveDataObjSections resolves an array of DataObjSections
// objects based on a given [Expression]. The expression is required
// to be a (tree of) [BinaryExpression] with a [ColumnExpression]
// on the left and a [LiteralExpression] on the right.
func (c *MetastoreCatalog) ResolveDataObjSections(selector Expression, predicates []Expression, shard ShardInfo, from, through time.Time) ([]DataObjSections, error) {
	selectorMatchers, err := expressionToMatchers(selector, false)
	if err != nil {
		return nil, fmt.Errorf("failed to convert selector expression into selector matchers: %w", err)
	}

	predicateMatchers := make([]*labels.Matcher, 0, len(predicates))
	for _, predicate := range predicates {
		matchers, err := expressionToMatchers(predicate, true)
		if err != nil {
			// Not all predicates are supported by the metastore, so some will be skipped
			continue
		}
		predicateMatchers = append(predicateMatchers, matchers...)
	}

	msSections, err := c.sectionsResolver(from, through, selectorMatchers, predicateMatchers)
	if err != nil {
		return nil, fmt.Errorf("resolve metastore sections: %w", err)
	}

	return filterForShard(shard, msSections)
}

// filterForShard filters the section descriptors for a given shard.
// It returns the locations, streams, and sections for the shard.
// TODO: Improve filtering: this method could be improved because it doesn't resolve the stream IDs to sections, even though this information is available. Instead, it resolves streamIDs to the whole object.
func filterForShard(shard ShardInfo, sections []*metastore.DataobjSectionDescriptor) ([]DataObjSections, error) {
	result := make([]DataObjSections, 0, len(sections))

	for _, s := range sections {
		ds := DataObjSections{}
		ds.Location = DataObjLocation(s.ObjectPath)

		if int(s.SectionIdx)%int(shard.Of) == int(shard.Shard) {
			ds.Streams = s.StreamIDs
			ds.Sections = []int{int(s.SectionIdx)}
			tr, err := newTimeRange(s.Start, s.End)
			if err != nil {
				return nil, err
			}
			ds.TimeRange = tr
			result = append(result, ds)
		}
	}

	return result, nil
}

// expressionToMatchers converts a selector expression to a list of matchers.
// The selector expression is required to be a (tree of) [BinaryExpression]
// with a [ColumnExpression] on the left and a [LiteralExpression] on the right.
// It optionally supports ambiguous column references. Non-ambiguous column references are label matchers.
func expressionToMatchers(selector Expression, allowAmbiguousColumnRefs bool) ([]*labels.Matcher, error) {
	if selector == nil {
		return nil, nil
	}

	switch expr := selector.(type) {
	case *BinaryExpr:
		switch expr.Op {
		case types.BinaryOpAnd:
			lhs, err := expressionToMatchers(expr.Left, allowAmbiguousColumnRefs)
			if err != nil {
				return nil, err
			}
			rhs, err := expressionToMatchers(expr.Right, allowAmbiguousColumnRefs)
			if err != nil {
				return nil, err
			}
			return append(lhs, rhs...), nil
		case types.BinaryOpEq, types.BinaryOpNeq, types.BinaryOpMatchRe, types.BinaryOpNotMatchRe:
			op, err := convertBinaryOp(expr.Op)
			if err != nil {
				return nil, err
			}
			name, err := convertColumnRef(expr.Left, allowAmbiguousColumnRefs)
			if err != nil {
				return nil, err
			}
			value, err := convertLiteralToString(expr.Right)
			if err != nil {
				return nil, err
			}
			lhs, err := labels.NewMatcher(op, name, value)
			if err != nil {
				return nil, err
			}
			return []*labels.Matcher{lhs}, nil
		default:
			return nil, fmt.Errorf("invalid binary expression in stream selector expression: %v", expr.Op.String())
		}
	default:
		return nil, fmt.Errorf("invalid expression type in stream selector expression: %T", expr)
	}
}

func convertLiteralToString(expr Expression) (string, error) {
	l, ok := expr.(*LiteralExpr)
	if !ok {
		return "", fmt.Errorf("expected literal expression, got %T", expr)
	}
	if l.ValueType() != types.Loki.String {
		return "", fmt.Errorf("literal type is not a string, got %v", l.ValueType())
	}
	return l.Value().(string), nil
}

func convertColumnRef(expr Expression, allowAmbiguousColumnRefs bool) (string, error) {
	ref, ok := expr.(*ColumnExpr)
	if !ok {
		return "", fmt.Errorf("expected column expression, got %T", expr)
	}
	if !allowAmbiguousColumnRefs && ref.Ref.Type != types.ColumnTypeLabel {
		return "", fmt.Errorf("column type is not a label, got %v", ref.Ref.Type)
	}
	return ref.Ref.Column, nil
}

func convertBinaryOp(t types.BinaryOp) (labels.MatchType, error) {
	ty, ok := binOpToMatchTypeMapping[t]
	if !ok {
		return -1, fmt.Errorf("invalid binary operator for matcher: %v", t)
	}
	return ty, nil
}

var _ Catalog = (*MetastoreCatalog)(nil)
