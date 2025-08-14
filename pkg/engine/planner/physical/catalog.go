package physical

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"time"

	"github.com/grafana/dskit/tenant"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/engine/internal/datatype"
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

type TimeRange struct {
	Start time.Time
	End   time.Time
}

func (t *TimeRange) Overlaps(secondRange TimeRange) bool {
	return (secondRange.Start.Before(t.End) && t.Start.Before(secondRange.End))
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
	// ResolveDataObj returns a list of data object paths,
	// a list of stream IDs for each data data object,
	// and a list of sections for each data object
	ResolveDataObj(Expression, time.Time, time.Time) ([]FilteredShardDescriptor, error)
	ResolveDataObjWithShard(Expression, []Expression, ShardInfo, time.Time, time.Time) ([]FilteredShardDescriptor, error)
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

// ResolveDataObj resolves DataObj locations and streams IDs based on a given
// [Expression]. The expression is required to be a (tree of) [BinaryExpression]
// with a [ColumnExpression] on the left and a [LiteralExpression] on the right.
func (c *MetastoreCatalog) ResolveDataObj(selector Expression, from, through time.Time) ([]FilteredShardDescriptor, error) {
	return c.ResolveDataObjWithShard(selector, nil, noShard, from, through)
}

func (c *MetastoreCatalog) ResolveDataObjWithShard(selector Expression, predicates []Expression, shard ShardInfo, from, through time.Time) ([]FilteredShardDescriptor, error) {
	if c.metastore == nil {
		return nil, errors.New("no metastore to resolve objects")
	}

	tenants, err := tenant.TenantIDs(c.ctx)
	if err != nil {
		return nil, err
	}

	switch c.metastore.ResolveStrategy(tenants) {
	case metastore.ResolveStrategyTypeDirect:
		return c.resolveDataObj(selector, shard, from, through)
	case metastore.ResolveStrategyTypeIndex:
		return c.resolveDataObjWithIndex(selector, predicates, shard, from, through)
	default:
		return nil, fmt.Errorf("invalid catalogue type: %d", c.metastore.ResolveStrategy(tenants))
	}
}

func (c *MetastoreCatalog) resolveDataObj(selector Expression, shard ShardInfo, from, through time.Time) ([]FilteredShardDescriptor, error) {
	matchers, err := expressionToMatchers(selector, false)
	if err != nil {
		return nil, fmt.Errorf("failed to convert selector expression into matchers: %w", err)
	}

	paths, streamIDs, numSections, err := c.metastore.StreamIDs(c.ctx, from, through, matchers...)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve data object locations: %w", err)
	}

	return filterForShard(shard, paths, streamIDs, numSections, from, through)
}

func filterForShard(shard ShardInfo, paths []string, streamIDs [][]int64, numSections []int, from, through time.Time) ([]FilteredShardDescriptor, error) {
	filteredDescriptors := make([]FilteredShardDescriptor, 0, len(paths))

	var count int
	for i := range paths {
		sec := make([]int, 0, numSections[i])

		for j := range numSections[i] {
			if count%int(shard.Of) == int(shard.Shard) {
				sec = append(sec, j)
			}
			count++
		}

		if len(sec) > 0 {
			filteredDescriptor := FilteredShardDescriptor{}
			filteredDescriptor.Location = DataObjLocation(paths[i])
			filteredDescriptor.Sections = sec
			filteredDescriptor.Streams = streamIDs[i]
			filteredDescriptor.TimeRange.Start = from
			filteredDescriptor.TimeRange.End = through
			filteredDescriptors = append(filteredDescriptors, filteredDescriptor)
		}
	}

	return filteredDescriptors, nil
}

// resolveDataobjWithIndex expects the metastore to initially point to index objects, not the log objects directly.
func (c *MetastoreCatalog) resolveDataObjWithIndex(selector Expression, predicates []Expression, shard ShardInfo, from, through time.Time) ([]FilteredShardDescriptor, error) {
	if c.metastore == nil {
		return nil, errors.New("no metastore to resolve objects")
	}

	matchers, err := expressionToMatchers(selector, false)
	if err != nil {
		return nil, fmt.Errorf("failed to convert selector expression into matchers: %w", err)
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
	index := make(map[DataObjLocation]int)

	filteredDescriptors := make([]FilteredShardDescriptor, 0, len(sectionDescriptors))

	for _, desc := range sectionDescriptors {
		filteredDescriptor := FilteredShardDescriptor{}
		filteredDescriptor.Location = DataObjLocation(desc.ObjectPath)
		_, ok := index[filteredDescriptor.Location]
		if !ok {
			idx := len(index)
			index[filteredDescriptor.Location] = idx
			filteredDescriptor.Streams = []int64{}
		}

		if int(desc.SectionIdx)%int(shard.Of) == int(shard.Shard) {
			filteredDescriptor.Streams = desc.StreamIDs
			filteredDescriptor.Sections = []int{int(desc.SectionIdx)}
			filteredDescriptor.TimeRange.Start = desc.Start
			filteredDescriptor.TimeRange.End = desc.End
		}
		filteredDescriptors = append(filteredDescriptors, filteredDescriptor)
	}

	for _, desc := range filteredDescriptors {
		sort.Slice(desc.Streams, func(j, k int) bool { return desc.Streams[j] < desc.Streams[k] })
		desc.Streams = slices.Compact(desc.Streams)
	}

	return filteredDescriptors, nil
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
	if l.ValueType() != datatype.Loki.String {
		return "", fmt.Errorf("literal type is not a string, got %v", l.ValueType())
	}
	return l.Any().(string), nil
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
