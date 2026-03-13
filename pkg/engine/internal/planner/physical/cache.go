package physical

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/grafana/loki/v3/pkg/engine/internal/util/dag"
)

// TaskCacheType indicates which backing store should be used for a [Cache] node.
type TaskCacheType = string

const (
	// TaskCacheTypeDataObjScan selects the dataobj cache (for DATAOBJSCAN tasks).
	TaskCacheTypeDataObjScan TaskCacheType = "dataobj"
	// TaskCacheTypePointersScan selects the metastore cache (for POINTERSSCAN tasks).
	TaskCacheTypePointersScan TaskCacheType = "metastore"
)

// Cache is a plan node that wraps the root of a cacheable task fragment.
// When executed, it transparently stores and retrieves results from a cache.
type Cache struct {
	NodeID ulid.ULID
	Key    string
	Cache  TaskCacheType
}

// ID returns the ULID that uniquely identifies the node in the plan.
func (c *Cache) ID() ulid.ULID { return c.NodeID }

// Type returns [NodeTypeCache].
func (*Cache) Type() NodeType { return NodeTypeCache }

// Clone returns a deep copy of the node with a new unique ID.
func (c *Cache) Clone() Node {
	return &Cache{NodeID: ulid.Make(), Key: c.Key, Cache: c.Cache}
}

// WrapWithCacheIfSupported computes a cache key for plan and, if the plan is
// cacheable, inserts a [Cache] node as the new root. It modifies plan in-place.
// Returns the new Cache root node and true on a cache wrap, nil and false otherwise.
func WrapWithCacheIfSupported(ctx context.Context, tenantID string, plan *Plan) (Node, bool, error) {
	key, cacheType := TaskCacheKey(ctx, tenantID, plan)
	if key == "" {
		// This plan does not support caching.
		return nil, false, nil
	}

	root, err := plan.Root()
	if err != nil {
		return nil, false, err
	}
	node := &Cache{NodeID: ulid.Make(), Key: key, Cache: cacheType}
	plan.graph.Add(node)
	if err := plan.graph.AddEdge(dag.Edge[Node]{Parent: node, Child: root}); err != nil {
		return nil, false, err
	}
	return node, true, nil
}

// TaskCacheKey computes a cache key for the entire plan by DFS-walking all nodes
// (pre-order) and concatenating their CacheKey values with " |>>| " separator.
//
// A plan is cacheable only if:
//   - It has exactly one root.
//   - No node returns "" for CacheKey (planning artifacts such as Parallelize
//     and ScanSet return "" and are never present in final task fragments).
//   - At least one [DataObjScan] or [PointersScan] is present; other non-scanning tasks are not cached.
//
// The result is prefixed with the tenantID.
// The returned TaskCacheType is TaskCacheTypePointersScan when the plan contains a
// PointersScan and no DataObjScan; otherwise it is TaskCacheTypeDataObjScan.
func TaskCacheKey(ctx context.Context, tenantID string, plan *Plan) (string, TaskCacheType) {
	roots := plan.Roots()
	if len(roots) != 1 {
		return "", ""
	}
	root := roots[0]

	var parts []string
	var hasDataObjScan, hasPointersScan bool
	var nonCacheable bool
	_ = plan.DFSWalk(root, func(n Node) error {
		key := n.CacheKey(ctx)
		if key == "" {
			nonCacheable = true
			return fmt.Errorf("non-cacheable node: %s", n.Type())
		}
		switch n.(type) {
		case *DataObjScan:
			hasDataObjScan = true
		case *PointersScan:
			hasPointersScan = true
		}
		parts = append(parts, key)
		return nil
	}, dag.PreOrderWalk)

	hasScan := hasDataObjScan || hasPointersScan
	if nonCacheable || !hasScan || len(parts) == 0 {
		return "", ""
	}

	cacheType := TaskCacheTypeDataObjScan
	if hasPointersScan && !hasDataObjScan {
		cacheType = TaskCacheTypePointersScan
	}

	const separator = " |>>| "
	return tenantID + separator + strings.Join(parts, separator), cacheType
}

// CacheKey returns a fixed token. The Key field is not part of the cache key
// because it is derived from the keys of all child nodes.
func (*Cache) CacheKey(_ context.Context) string { return "Cache" }

func (*Batching) CacheKey(_ context.Context) string { return "Batching" }

func (*Merge) CacheKey(_ context.Context) string { return "Merge" }

func (*Parallelize) CacheKey(_ context.Context) string { return "" }

func (*ScanSet) CacheKey(_ context.Context) string { return "" }

func (*Join) CacheKey(_ context.Context) string { return "" }

func (l *Limit) CacheKey(_ context.Context) string {
	return fmt.Sprintf("Limit{skip=%d,fetch=%d}", l.Skip, l.Fetch)
}

func (t *TopK) CacheKey(_ context.Context) string {
	return fmt.Sprintf("TopK{sort_by=%s,ascending=%v,nulls_first=%v,k=%d}",
		t.SortBy, t.Ascending, t.NullsFirst, t.K)
}

func (f *Filter) CacheKey(_ context.Context) string {
	predicates := make([]string, len(f.Predicates))
	for i, p := range f.Predicates {
		predicates[i] = p.String()
	}
	return fmt.Sprintf("Filter{predicates=[%s]}", strings.Join(predicates, ","))
}

func (p *Projection) CacheKey(_ context.Context) string {
	exprs := make([]string, len(p.Expressions))
	for i, e := range p.Expressions {
		exprs[i] = e.String()
	}
	var mode string
	switch {
	case p.Expand:
		mode = "expand"
	case p.Drop:
		mode = "drop"
	}
	return fmt.Sprintf("Projection{all=%v,mode=%s,expressions=[%s]}", p.All, mode, strings.Join(exprs, ","))
}

func (v *VectorAggregation) CacheKey(_ context.Context) string {
	return fmt.Sprintf("VectorAggregation{operation=%s,grouping=%s,max_series=%d}",
		v.Operation, groupingCacheKey(v.Grouping), v.MaxQuerySeries)
}

func (r *RangeAggregation) CacheKey(_ context.Context) string {
	return fmt.Sprintf("RangeAggregation{operation=%s,start=%s,end=%s,step=%s,range=%s,grouping=%s,max_series=%d}",
		r.Operation,
		r.Start.UTC().Format(time.RFC3339Nano),
		r.End.UTC().Format(time.RFC3339Nano),
		r.Step,
		r.Range,
		groupingCacheKey(r.Grouping),
		r.MaxQuerySeries)
}

func (m *ColumnCompat) CacheKey(_ context.Context) string {
	collisions := make([]string, len(m.Collisions))
	for i, c := range m.Collisions {
		collisions[i] = c.String()
	}
	return fmt.Sprintf("ColumnCompat{src=%s,dst=%s,collisions=[%s]}",
		m.Source, m.Destination, strings.Join(collisions, ","))
}

func (s *DataObjScan) CacheKey(_ context.Context) string {
	streamIDs := make([]string, len(s.StreamIDs))
	for i, id := range s.StreamIDs {
		streamIDs[i] = fmt.Sprintf("%d", id)
	}
	projections := make([]string, len(s.Projections))
	for i, p := range s.Projections {
		projections[i] = p.String()
	}
	predicates := make([]string, len(s.Predicates))
	for i, p := range s.Predicates {
		predicates[i] = p.String()
	}
	return fmt.Sprintf("DataObjScan{location=%s,section=%d,stream_ids=[%s],projections=[%s],predicates=[%s]}",
		s.Location, s.Section,
		strings.Join(streamIDs, ","),
		strings.Join(projections, ","),
		strings.Join(predicates, ","))
}

func (s *PointersScan) CacheKey(_ context.Context) string {
	predicates := make([]string, len(s.Predicates))
	for i, p := range s.Predicates {
		predicates[i] = p.String()
	}
	var selector string
	if s.Selector != nil {
		selector = s.Selector.String()
	}
	return fmt.Sprintf("PointersScan{location=%s,selector=%s,predicates=[%s],start=%s,end=%s}",
		s.Location, selector,
		strings.Join(predicates, ","),
		s.Start.UTC().Format(time.RFC3339Nano),
		s.End.UTC().Format(time.RFC3339Nano))
}

// groupingCacheKey returns a deterministic string for a Grouping.
func groupingCacheKey(g Grouping) string {
	cols := make([]string, len(g.Columns))
	for i, c := range g.Columns {
		cols[i] = c.String()
	}
	if g.Without {
		return fmt.Sprintf("without=[%s]", strings.Join(cols, ","))
	}
	return fmt.Sprintf("by=[%s]", strings.Join(cols, ","))
}
