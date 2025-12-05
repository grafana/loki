package physical

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
)

// MetaqueryRequestKind describes what metadata the planner needs.
type MetaqueryRequestKind uint8

const (
	// MetaqueryRequestKindSections requests section descriptors (current behavior).
	MetaqueryRequestKindSections MetaqueryRequestKind = iota
)

// MetaqueryRequest captures the information required to resolve metadata prior
// to physical planning.
type MetaqueryRequest struct {
	Kind MetaqueryRequestKind

	Selector   Expression
	Predicates []Expression

	Shard ShardInfo

	From    time.Time
	Through time.Time
}

// MetaqueryResponse contains the payload returned by a metaquery execution.
type MetaqueryResponse struct {
	Kind MetaqueryRequestKind

	Sections []*metastore.DataobjSectionDescriptor
}

// MetaqueryRunner executes metaqueries and returns their responses.
type MetaqueryRunner interface {
	Run(ctx context.Context, req MetaqueryRequest) (MetaqueryResponse, error)
}

// LocalMetaqueryRunner makes metaquery requests by calling the metastore
// directly on the coordinator.
type LocalMetaqueryRunner struct {
	Metastore metastore.Metastore
}

// Run executes the request against the metastore.
func (r *LocalMetaqueryRunner) Run(ctx context.Context, req MetaqueryRequest) (MetaqueryResponse, error) {
	if r.Metastore == nil {
		return MetaqueryResponse{}, fmt.Errorf("metastore is required for local metaqueries")
	}

	switch req.Kind {
	case MetaqueryRequestKindSections:
		return r.runSections(ctx, req)
	default:
		return MetaqueryResponse{}, fmt.Errorf("unsupported metaquery kind %d", req.Kind)
	}
}

func (r *LocalMetaqueryRunner) runSections(ctx context.Context, req MetaqueryRequest) (MetaqueryResponse, error) {
	matchers, err := expressionToMatchers(req.Selector, false)
	if err != nil {
		return MetaqueryResponse{}, fmt.Errorf("converting selector into matchers: %w", err)
	}

	var predicateMatchers []*labels.Matcher
	for _, predicate := range req.Predicates {
		m, err := expressionToMatchers(predicate, true)
		if err != nil {
			// Not all predicates are supported by the metastore; ignore them and
			// rely on downstream filtering when necessary.
			continue
		}
		predicateMatchers = append(predicateMatchers, m...)
	}

	sections, err := r.Metastore.Sections(ctx, req.From, req.Through, matchers, predicateMatchers)
	if err != nil {
		return MetaqueryResponse{}, err
	}

	return MetaqueryResponse{
		Kind:     MetaqueryRequestKindSections,
		Sections: sections,
	}, nil
}

type metaqueryRequestKey struct {
	kind       MetaqueryRequestKind
	selector   string
	predicates string
	from       int64
	through    int64
	shard      uint32
	shardOf    uint32
}

func makeMetaqueryKey(req MetaqueryRequest) metaqueryRequestKey {
	return metaqueryRequestKey{
		kind:       req.Kind,
		selector:   expressionSignature(req.Selector),
		predicates: predicatesSignature(req.Predicates),
		from:       req.From.UnixNano(),
		through:    req.Through.UnixNano(),
		shard:      req.Shard.Shard,
		shardOf:    req.Shard.Of,
	}
}

func expressionSignature(expr Expression) string {
	if expr == nil {
		return ""
	}
	return expr.String()
}

func predicatesSignature(exprs []Expression) string {
	if len(exprs) == 0 {
		return ""
	}
	parts := make([]string, len(exprs))
	for i, expr := range exprs {
		parts[i] = expressionSignature(expr)
	}
	return strings.Join(parts, ";")
}

// MetaqueryCollectorCatalog records catalog lookups made during planning so
// that they can be executed as metaqueries ahead of time.
type MetaqueryCollectorCatalog struct {
	requests map[metaqueryRequestKey]MetaqueryRequest
}

func NewMetaqueryCollectorCatalog() *MetaqueryCollectorCatalog {
	return &MetaqueryCollectorCatalog{
		requests: make(map[metaqueryRequestKey]MetaqueryRequest),
	}
}

func (c *MetaqueryCollectorCatalog) ResolveShardDescriptors(selector Expression, from, through time.Time) ([]FilteredShardDescriptor, error) {
	return c.ResolveShardDescriptorsWithShard(selector, nil, noShard, from, through)
}

func (c *MetaqueryCollectorCatalog) ResolveShardDescriptorsWithShard(selector Expression, predicates []Expression, shard ShardInfo, from, through time.Time) ([]FilteredShardDescriptor, error) {
	req := MetaqueryRequest{
		Kind:       MetaqueryRequestKindSections,
		Selector:   cloneExpression(selector),
		Predicates: cloneExpressionSlice(predicates),
		Shard:      shard,
		From:       from,
		Through:    through,
	}

	key := makeMetaqueryKey(req)
	if _, ok := c.requests[key]; !ok {
		c.requests[key] = req
	}
	return nil, nil
}

// Requests returns the accumulated metaquery requests.
func (c *MetaqueryCollectorCatalog) Requests() []MetaqueryRequest {
	out := make([]MetaqueryRequest, 0, len(c.requests))
	for _, req := range c.requests {
		out = append(out, req)
	}
	return out
}

// MetaqueryPreparedCatalog serves catalog responses for previously satisfied metaqueries.
type MetaqueryPreparedCatalog struct {
	responses map[metaqueryRequestKey]MetaqueryResponse
}

func NewMetaqueryPreparedCatalog() *MetaqueryPreparedCatalog {
	return &MetaqueryPreparedCatalog{
		responses: make(map[metaqueryRequestKey]MetaqueryResponse),
	}
}

func (c *MetaqueryPreparedCatalog) Store(req MetaqueryRequest, resp MetaqueryResponse) error {
	c.responses[makeMetaqueryKey(req)] = resp
	return nil
}

func (c *MetaqueryPreparedCatalog) ResolveShardDescriptors(selector Expression, from, through time.Time) ([]FilteredShardDescriptor, error) {
	return c.ResolveShardDescriptorsWithShard(selector, nil, noShard, from, through)
}

func (c *MetaqueryPreparedCatalog) ResolveShardDescriptorsWithShard(selector Expression, predicates []Expression, shard ShardInfo, from, through time.Time) ([]FilteredShardDescriptor, error) {
	req := MetaqueryRequest{
		Kind:       MetaqueryRequestKindSections,
		Selector:   selector,
		Predicates: predicates,
		Shard:      shard,
		From:       from,
		Through:    through,
	}
	resp, ok := c.responses[makeMetaqueryKey(req)]
	if !ok {
		return nil, fmt.Errorf("metaquery responses missing for selector %q", expressionSignature(selector))
	}

	switch resp.Kind {
	case MetaqueryRequestKindSections:
		filtered, err := filterDescriptorsForShard(req.Shard, resp.Sections)
		if err != nil {
			return nil, fmt.Errorf("filter descriptions for shard: %w", err)
		}
		return cloneFilteredShardDescriptors(filtered), nil
	default:
		// TODO(ivkalita): validate when storing the results, panic (?) here
		return nil, fmt.Errorf("unsupported metaquery result kind %d", resp.Kind)
	}
}

func cloneExpression(expr Expression) Expression {
	if expr == nil {
		return nil
	}
	return expr.Clone()
}

func cloneExpressionSlice(exprs []Expression) []Expression {
	if len(exprs) == 0 {
		return nil
	}
	out := make([]Expression, len(exprs))
	for i, expr := range exprs {
		out[i] = cloneExpression(expr)
	}
	return out
}

func cloneFilteredShardDescriptors(descs []FilteredShardDescriptor) []FilteredShardDescriptor {
	if len(descs) == 0 {
		return nil
	}
	cloned := make([]FilteredShardDescriptor, len(descs))
	for i, desc := range descs {
		cloned[i] = FilteredShardDescriptor{
			Location:  desc.Location,
			Streams:   append([]int64(nil), desc.Streams...),
			Sections:  append([]int(nil), desc.Sections...),
			TimeRange: desc.TimeRange,
		}
	}
	return cloned
}
