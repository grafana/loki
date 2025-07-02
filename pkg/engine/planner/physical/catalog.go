package physical

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

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

// Catalog is an interface that provides methods for interacting with
// storage metadata. In traditional database systems there are system tables
// providing this information (e.g. pg_catalog, ...) whereas in Loki there
// is the Metastore.
type Catalog interface {
	// ResolveDataObj returns a list of data object paths,
	// a list of stream IDs for each data data object,
	// and a list of sections for each data object
	ResolveDataObj(Expression, time.Time, time.Time) ([]DataObjLocation, [][]int64, [][]int, error)
	ResolveDataObjWithShard(Expression, ShardInfo, time.Time, time.Time) ([]DataObjLocation, [][]int64, [][]int, error)
	ResolveSections(Expression, Expression) ([]metastore.DataobjSectionDescriptor, error)
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
func (c *MetastoreCatalog) ResolveDataObj(selector Expression, from, through time.Time) ([]DataObjLocation, [][]int64, [][]int, error) {
	return c.ResolveDataObjWithShard(selector, noShard, from, through)
}

func (c *MetastoreCatalog) ResolveDataObjWithShard(selector Expression, shard ShardInfo, from, through time.Time) ([]DataObjLocation, [][]int64, [][]int, error) {
	if c.metastore == nil {
		return nil, nil, nil, errors.New("no metastore to resolve objects")
	}

	matchers, err := expressionToMatchers(selector)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to convert selector expression into matchers: %w", err)
	}

<<<<<<< HEAD:pkg/engine/planner/physical/catalog.go
	paths, streamIDs, numSections, err := c.metastore.StreamIDs(c.ctx, from, through, matchers...)
=======
	sections, err := c.metastore.StreamIDsBySections(c.ctx, c.from, c.through, matchers...)
>>>>>>> 98d7d9dc9d (WIP):pkg/engine/planner/physical/context.go
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to resolve data object locations: %w", err)
	}
	fmt.Printf("sections=%d\n", len(sections))

	return filterDescriptorsForShard(shard, sections)

	/*
		 	paths, streamIDs, numSections, err := c.metastore.StreamIDs(c.ctx, c.from, c.through, matchers...)
			if err != nil {
				return nil, nil, nil, fmt.Errorf("failed to resolve data object locations: %w", err)
			}

			locations, streams, objectSections, err := filterForShard(shard, paths, streamIDs, numSections)
			if err != nil {
				return nil, nil, nil, fmt.Errorf("failed to filter for shard: %w", err)
			}

			sectionsCount := 0
			for _, section := range objectSections {
				sectionsCount += len(section)
			}
			fmt.Printf("StreamIDs: sectionsCount=%d\n", sectionsCount)

			return locations, streams, objectSections, nil
	*/
}

func filterForShard(shard ShardInfo, paths []string, streamIDs [][]int64, numSections []int) ([]DataObjLocation, [][]int64, [][]int, error) {
	locations := make([]DataObjLocation, 0, len(paths))
	streams := make([][]int64, 0, len(paths))
	sections := make([][]int, 0, len(paths))

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
			locations = append(locations, DataObjLocation(paths[i]))
			streams = append(streams, streamIDs[i])
			sections = append(sections, sec)
		}
	}

	return locations, streams, sections, nil
}

// ResolveDataObj resolves DataObj locations and streams IDs based on a given
// [Expression]. The expression is required to be a (tree of) [BinaryExpression]
// with a [ColumnExpression] on the left and a [LiteralExpression] on the right.
func (c *Context) ResolveSections(selector Expression, shard ShardInfo) ([]DataObjLocation, [][]int64, [][]int, error) {
	if c.metastore == nil {
		return nil, nil, nil, errors.New("no metastore to resolve objects")
	}

	matchers, err := expressionToMatchers(selector)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to convert selector expression into matchers: %w", err)
	}

	/* 	// TODO(benclive): Handle these ambiguous column cases in a less hacky way
	   	tableHintMatchers, err := expressionToMatchers(tableHint)
	   	if err != nil {
	   		return nil, nil, fmt.Errorf("failed to convert table hint expression into matchers: %w", err)
	   	} */

	sectionDescriptors, err := c.metastore.StreamIDsBySections(c.ctx, c.from, c.through, matchers...)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to resolve data object locations: %w", err)
	}

	return filterDescriptorsForShard(shard, sectionDescriptors)
}

// filterDescriptorsForShard filters the section descriptors for a given shard.
// It returns the locations, streams, and sections for the shard.
// It loses information because it doesn't map streamIDs to sections.
func filterDescriptorsForShard(shard ShardInfo, sectionDescriptors []metastore.DataobjSectionDescriptor) ([]DataObjLocation, [][]int64, [][]int, error) {
	index := make(map[DataObjLocation]int)

	locations := make([]DataObjLocation, 0, len(sectionDescriptors))
	streams := make([][]int64, 0, len(sectionDescriptors))
	sections := make([][]int, 0, len(sectionDescriptors))

	for _, desc := range sectionDescriptors {
		idx, ok := index[DataObjLocation(desc.ObjectPath)]
		if !ok {
			idx = len(index)
			index[DataObjLocation(desc.ObjectPath)] = idx
			locations = append(locations, DataObjLocation(desc.ObjectPath))
			streams = append(streams, []int64{})
			sections = append(sections, []int{})
		}

		if int(desc.SectionIdx)%int(shard.Of) == int(shard.Shard) {
			streams[idx] = append(streams[idx], desc.StreamIDs...)
			sections[idx] = append(sections[idx], int(desc.SectionIdx))
		}
	}

	for i := range streams {
		sort.Slice(streams[i], func(j, k int) bool { return streams[i][j] < streams[i][k] })
		sort.Slice(sections[i], func(j, k int) bool { return sections[i][j] < sections[i][k] })
		streams[i] = uniquelong(streams[i])
		sections[i] = uniqueint(sections[i])
	}

	return locations, streams, sections, nil
}

func uniqueint(s []int) []int {
	next := 0
	for i := range s {
		if s[i] != s[next] {
			next++
			s[next] = s[i]
		}
	}
	return s[:next+1]
}

func uniquelong(s []int64) []int64 {
	next := 0
	for i := range s {
		if s[i] != s[next] {
			next++
			s[next] = s[i]
		}
	}
	return s[:next+1]
}

func expressionToMatchers(selector Expression) ([]*labels.Matcher, error) {
	if selector == nil {
		return nil, nil
	}

	switch expr := selector.(type) {
	case *BinaryExpr:
		switch expr.Op {
		case types.BinaryOpAnd:
			lhs, err := expressionToMatchers(expr.Left)
			if err != nil {
				return nil, err
			}
			rhs, err := expressionToMatchers(expr.Right)
			if err != nil {
				return nil, err
			}
			return append(lhs, rhs...), nil
		case types.BinaryOpEq, types.BinaryOpNeq, types.BinaryOpMatchRe, types.BinaryOpNotMatchRe:
			op, err := convertBinaryOp(expr.Op)
			if err != nil {
				return nil, err
			}
			name, err := convertColumnRef(expr.Left)
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

func convertColumnRef(expr Expression) (string, error) {
	ref, ok := expr.(*ColumnExpr)
	if !ok {
		return "", fmt.Errorf("expected column expression, got %T", expr)
	}
	if ref.Ref.Type != types.ColumnTypeLabel {
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
