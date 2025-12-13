package physical

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
)

type ShardInfo struct {
	Shard uint32
	Of    uint32
}

func (s ShardInfo) String() string {
	return fmt.Sprintf("%d_of_%d", s.Shard, s.Of)
}

// TimeRange contains Start and End that are inclusive.
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
	// ResolveShardDescriptorsWithShard returns a list of
	// FilteredShardDescriptor objects, which each include:
	// a data object path, a list of stream IDs for
	// each data object path, a list of sections for
	// each data object path, and a time range.
	ResolveShardDescriptorsWithShard(Expression, []Expression, ShardInfo, time.Time, time.Time) ([]FilteredShardDescriptor, error)
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

func (c *MetastoreCatalog) ResolveShardDescriptorsWithShard(selector Expression, predicates []Expression, shard ShardInfo, from, through time.Time) ([]FilteredShardDescriptor, error) {
	if c.metastore == nil {
		return nil, errors.New("no metastore to resolve objects")
	}

	return c.resolveShardDescriptorsWithIndex(selector, predicates, shard, from, through)
}

// resolveShardDescriptorsWithIndex expects the metastore to initially point to index objects, not the log objects directly.
func (c *MetastoreCatalog) resolveShardDescriptorsWithIndex(selector Expression, predicates []Expression, shard ShardInfo, from, through time.Time) ([]FilteredShardDescriptor, error) {
	if c.metastore == nil {
		return nil, errors.New("no metastore to resolve objects")
	}

	matchers, err := ExpressionToMatchers(selector, false)
	if err != nil {
		return nil, fmt.Errorf("failed to convert selector expression into matchers: %w", err)
	}

	predicateMatchers := make([]*labels.Matcher, 0, len(predicates))
	for _, predicate := range predicates {
		matchers, err := ExpressionToMatchers(predicate, true)
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

	return FilterDescriptorsForShard(shard, sectionDescriptors)
}

// CatalogRequestKind describes what metadata the planner needs.
type CatalogRequestKind uint8

const (
	// CatalogRequestKindResolveShardDescriptorsWithShard requests section descriptors
	CatalogRequestKindResolveShardDescriptorsWithShard CatalogRequestKind = iota
)

// CatalogRequest captures catalog request required to finish physical planning.
type CatalogRequest struct {
	Kind CatalogRequestKind

	Selector   Expression
	Predicates []Expression

	Shard ShardInfo

	From    time.Time
	Through time.Time
}

// CatalogResponse contains catalog response.
type CatalogResponse struct {
	Kind CatalogRequestKind

	Descriptors []FilteredShardDescriptor
}

type catalogRequestKey struct {
	kind       CatalogRequestKind
	selector   string
	predicates string
	from       int64
	through    int64
	shard      uint32
	shardOf    uint32
}

func newCatalogRequestKey(req CatalogRequest) catalogRequestKey {
	return catalogRequestKey{
		kind:       req.Kind,
		selector:   expressionSignature(req.Selector),
		predicates: expressionsSignature(req.Predicates),
		from:       req.From.UnixNano(),
		through:    req.Through.UnixNano(),
		shard:      req.Shard.Shard,
		shardOf:    req.Shard.Of,
	}
}

func expressionsSignature(exprs []Expression) string {
	if len(exprs) == 0 {
		return ""
	}
	parts := make([]string, len(exprs))
	for i, expr := range exprs {
		parts[i] = expressionSignature(expr)
	}
	return strings.Join(parts, ";")
}

func expressionSignature(expr Expression) string {
	if expr == nil {
		return ""
	}
	return expr.String()
}

type UnresolvedCatalog struct {
	requests map[catalogRequestKey]CatalogRequest
}

func NewUnresolvedCatalog() UnresolvedCatalog {
	return UnresolvedCatalog{
		requests: make(map[catalogRequestKey]CatalogRequest),
	}
}

func (c UnresolvedCatalog) ResolveShardDescriptorsWithShard(selector Expression, predicates []Expression, shard ShardInfo, from, through time.Time) ([]FilteredShardDescriptor, error) {
	req := CatalogRequest{
		Kind:       CatalogRequestKindResolveShardDescriptorsWithShard,
		Selector:   cloneExpressions([]Expression{selector})[0],
		Predicates: cloneExpressions(predicates),
		Shard:      shard,
		From:       from,
		Through:    through,
	}

	key := newCatalogRequestKey(req)
	if _, ok := c.requests[key]; !ok {
		c.requests[key] = req
	}
	return nil, nil
}

func (c UnresolvedCatalog) RequestsCount() int {
	return len(c.requests)
}

func (c UnresolvedCatalog) Resolve(resolve func(CatalogRequest) (CatalogResponse, error)) (ResolvedCatalog, error) {
	resolved := ResolvedCatalog{
		responses: make(map[catalogRequestKey]CatalogResponse),
	}
	for key, req := range c.requests {
		resp, err := resolve(req)
		if err != nil {
			return resolved, fmt.Errorf("failed to resolve catalog response: %w", err)
		}
		resolved.responses[key] = resp
	}

	return resolved, nil
}

type ResolvedCatalog struct {
	responses map[catalogRequestKey]CatalogResponse
}

func (c ResolvedCatalog) ResolveShardDescriptorsWithShard(selector Expression, predicates []Expression, shard ShardInfo, from, through time.Time) ([]FilteredShardDescriptor, error) {
	req := CatalogRequest{
		Kind:       CatalogRequestKindResolveShardDescriptorsWithShard,
		Selector:   selector,
		Predicates: predicates,
		Shard:      shard,
		From:       from,
		Through:    through,
	}
	reqKey := newCatalogRequestKey(req)
	resp, ok := c.responses[reqKey]
	if !ok {
		return nil, fmt.Errorf("catalog response missing for request %+v", reqKey)
	}

	switch resp.Kind {
	case CatalogRequestKindResolveShardDescriptorsWithShard:
		return resp.Descriptors, nil
	default:
		return nil, fmt.Errorf("unsupported response kind %d", resp.Kind)
	}
}

// FilterDescriptorsForShard filters the section descriptors for a given shard.
// It returns the locations, streams, and sections for the shard.
// TODO: Improve filtering: this method could be improved because it doesn't resolve the stream IDs to sections, even though this information is available. Instead, it resolves streamIDs to the whole object.
func FilterDescriptorsForShard(shard ShardInfo, sectionDescriptors []*metastore.DataobjSectionDescriptor) ([]FilteredShardDescriptor, error) {
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

var _ Catalog = (*MetastoreCatalog)(nil)
var _ Catalog = UnresolvedCatalog{}
var _ Catalog = ResolvedCatalog{}
