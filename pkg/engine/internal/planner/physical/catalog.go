package physical

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
)

var (
	binOpToMatchTypeMapping = map[BinaryOp]labels.MatchType{
		BINARY_OP_EQ:           labels.MatchEqual,
		BINARY_OP_NEQ:          labels.MatchNotEqual,
		BINARY_OP_MATCH_RE:     labels.MatchRegexp,
		BINARY_OP_NOT_MATCH_RE: labels.MatchNotRegexp,
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

// Start and End are inclusive.
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

func (t *TimeRange) Merge(secondRange TimeRange) TimeRange {
	var out TimeRange
	if t.Start.Before(secondRange.Start) {
		out.Start = t.Start
	} else {
		out.Start = secondRange.Start
	}
	if t.End.After(secondRange.End) {
		out.End = t.End
	} else {
		out.End = secondRange.End
	}
	return out
}

type DataObjLocation string

type FilteredShardDescriptor struct {
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
	// ResolveShardDescriptors returns a list of
	// FilteredShardDescriptor objects, which each include:
	// a data object path, a list of stream IDs for
	// each data object path, a list of sections for
	// each data object path, and a time range.
	ResolveShardDescriptors(Expression, time.Time, time.Time) ([]FilteredShardDescriptor, error)
	ResolveShardDescriptorsWithShard(Expression, []*Expression, ShardInfo, time.Time, time.Time) ([]FilteredShardDescriptor, error)
}

// MetastoreCatalog is the default implementation of [Catalog].
type MetastoreCatalog struct {
	ctx       context.Context
	metastore metastore.Metastore
}

// NewMetastoreCatalog creates a new instance of [MetastoreCatalog] for query planning.
func NewMetastoreCatalog(ctx context.Context, ms metastore.Metastore) *MetastoreCatalog {
	return &MetastoreCatalog{
		ctx:       ctx,
		metastore: ms,
	}
}

// ResolveShardDescriptors resolves an array of FilteredShardDescriptor
// objects based on a given [Expression]. The expression is required
// to be a (tree of) [BinaryExpression] with a [ColumnExpression]
// on the left and a [LiteralExpression] on the right.
func (c *MetastoreCatalog) ResolveShardDescriptors(selector Expression, from, through time.Time) ([]FilteredShardDescriptor, error) {
	return c.ResolveShardDescriptorsWithShard(selector, nil, noShard, from, through)
}

func (c *MetastoreCatalog) ResolveShardDescriptorsWithShard(selector Expression, predicates []*Expression, shard ShardInfo, from, through time.Time) ([]FilteredShardDescriptor, error) {
	if c.metastore == nil {
		return nil, errors.New("no metastore to resolve objects")
	}

	return c.resolveShardDescriptorsWithIndex(selector, predicates, shard, from, through)
}

// resolveShardDescriptorsWithIndex expects the metastore to initially point to index objects, not the log objects directly.
func (c *MetastoreCatalog) resolveShardDescriptorsWithIndex(selector Expression, predicates []*Expression, shard ShardInfo, from, through time.Time) ([]FilteredShardDescriptor, error) {
	if c.metastore == nil {
		return nil, errors.New("no metastore to resolve objects")
	}

	matchers, err := expressionToMatchers(selector, false)
	if err != nil {
		return nil, fmt.Errorf("failed to convert selector expression into matchers: %w", err)
	}

	predicateMatchers := make([]*labels.Matcher, 0, len(predicates))
	for _, predicate := range predicates {
		matchers, err := expressionToMatchers(*predicate, true)
		if err != nil {
			// Not all predicates are supported by the metastore, so some will be skipped
			continue
		}
		predicateMatchers = append(predicateMatchers, matchers...)
	}

	sectionDescriptors, err := c.metastore.Sections(c.ctx, from, through, matchers, predicateMatchers)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve data object sections: %w", err)
	}

	return filterDescriptorsForShard(shard, sectionDescriptors)
}

// filterDescriptorsForShard filters the section descriptors for a given shard.
// It returns the locations, streams, and sections for the shard.
// TODO: Improve filtering: this method could be improved because it doesn't resolve the stream IDs to sections, even though this information is available. Instead, it resolves streamIDs to the whole object.
func filterDescriptorsForShard(shard ShardInfo, sectionDescriptors []*metastore.DataobjSectionDescriptor) ([]FilteredShardDescriptor, error) {
	filteredDescriptors := make([]FilteredShardDescriptor, 0, len(sectionDescriptors))

	for _, desc := range sectionDescriptors {
		filteredDescriptor := FilteredShardDescriptor{}
		filteredDescriptor.Location = DataObjLocation(desc.ObjectPath)

		if int(desc.SectionIdx)%int(shard.Of) == int(shard.Shard) {
			filteredDescriptor.Streams = desc.StreamIDs
			filteredDescriptor.Sections = []int{int(desc.SectionIdx)}
			tr, err := newTimeRange(desc.Start, desc.End)
			if err != nil {
				return nil, err
			}
			filteredDescriptor.TimeRange = tr
			filteredDescriptors = append(filteredDescriptors, filteredDescriptor)
		}
	}

	return filteredDescriptors, nil
}

// expressionToMatchers converts a selector expression to a list of matchers.
// The selector expression is required to be a (tree of) [BinaryExpression]
// with a [ColumnExpression] on the left and a [LiteralExpression] on the right.
// It optionally supports ambiguous column references. Non-ambiguous column references are label matchers.
func expressionToMatchers(selector Expression, allowAmbiguousColumnRefs bool) ([]*labels.Matcher, error) {

	switch expr := selector.Kind.(type) {
	case *Expression_BinaryExpression:
		switch expr.BinaryExpression.Op {
		case BINARY_OP_AND:
			lhs, err := expressionToMatchers(*expr.BinaryExpression.Left, allowAmbiguousColumnRefs)
			if err != nil {
				return nil, err
			}
			rhs, err := expressionToMatchers(*expr.BinaryExpression.Right, allowAmbiguousColumnRefs)
			if err != nil {
				return nil, err
			}
			return append(lhs, rhs...), nil
		case BINARY_OP_EQ, BINARY_OP_NEQ, BINARY_OP_MATCH_RE, BINARY_OP_NOT_MATCH_RE:
			op, err := convertBinaryOp(expr.BinaryExpression.Op)
			if err != nil {
				return nil, err
			}
			name, err := convertColumnRef(*expr.BinaryExpression.Left, allowAmbiguousColumnRefs)
			if err != nil {
				return nil, err
			}
			value, err := convertLiteralToString(*expr.BinaryExpression.Right)
			if err != nil {
				return nil, err
			}
			lhs, err := labels.NewMatcher(op, name, value)
			if err != nil {
				return nil, err
			}
			return []*labels.Matcher{lhs}, nil
		default:
			return nil, fmt.Errorf("invalid binary expression in stream selector expression: %v", expr.BinaryExpression.Op.String())
		}
	default:
		return nil, fmt.Errorf("invalid expression type in stream selector expression: %T", expr)
	}
}

func convertLiteralToString(expr Expression) (string, error) {
	l, ok := expr.Kind.(*Expression_LiteralExpression)
	if !ok {
		return "", fmt.Errorf("expected literal expression, got %T", expr)
	}
	_, ok = l.LiteralExpression.Kind.(*LiteralExpression_StringLiteral)
	if !ok {
		return "", fmt.Errorf("literal type is not a string, got %v", l.LiteralExpression.Kind)
	}
	return l.LiteralExpression.GetStringLiteral().Value, nil
}

func convertColumnRef(expr Expression, allowAmbiguousColumnRefs bool) (string, error) {
	ref, ok := expr.Kind.(*Expression_ColumnExpression)
	if !ok {
		return "", fmt.Errorf("expected column expression, got %T", expr)
	}
	if !allowAmbiguousColumnRefs && ref.ColumnExpression.Type != COLUMN_TYPE_LABEL {
		return "", fmt.Errorf("column type is not a label, got %v", ref.ColumnExpression.Type)
	}
	return ref.ColumnExpression.Name, nil
}

func convertBinaryOp(t BinaryOp) (labels.MatchType, error) {
	ty, ok := binOpToMatchTypeMapping[t]
	if !ok {
		return -1, fmt.Errorf("invalid binary operator for matcher: %v", t)
	}
	return ty, nil
}

var _ Catalog = (*MetastoreCatalog)(nil)
