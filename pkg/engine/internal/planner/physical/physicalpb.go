// Package physicalpb contains the protobuf definitions for physical plan nodes.
package physical

import (
	"errors"
	fmt "fmt"
	"iter"
	"slices"

	"github.com/grafana/loki/v3/pkg/engine/internal/util/dag"
	"github.com/grafana/loki/v3/pkg/engine/internal/util/ulid"
)

type NodeKind uint8

const (
	NodeKindInvalid         NodeKind = iota // NodeKindInvalid is an invalid NodeKind.
	NodeKindDataObjScan                     // NodeKindDataObjScan is used for [DataObjScan].
	NodeKindSortMerge                       // NodeKindSortMerge is used for [SortMerge].
	NodeKindProjection                      // NodeKindProjection is used for [Projection].
	NodeKindFilter                          // NodeKindFilter is used for [Filter].
	NodeKindLimit                           // NodeKindLimit is used for [Limit].
	NodeKindAggregateRange                  // NodeKindAggregateRange is used for [AggregateRange].
	NodeKindAggregateVector                 // NodeKindAggregateVector is used for [AggregateVector].
	NodeKindMerge                           // NodeKindMerge is used for [Merge].
	NodeKindParse                           // NodeKindParse is used for [Parse].
	NodeKindColumnCompat                    // NodeKindColumnCompat is used for [ColumnCompat].
	NodeKindTopK                            // NodeKindTopK is used for [TopK].
	NodeKindScanSet                         // NodeKindScanSet is used for [ScanSet].
	NodeKindParallelize                     // NodeKindParallelize is used for [Parallelize].
	NodeKindJoin                            // NodeKindJoin is used for [Join].
)

var nodeKinds = [...]string{
	NodeKindInvalid:         "invalid",
	NodeKindDataObjScan:     "DataObjScan",
	NodeKindSortMerge:       "SortMerge",
	NodeKindProjection:      "Projection",
	NodeKindFilter:          "Filter",
	NodeKindLimit:           "Limit",
	NodeKindAggregateRange:  "AggregateRange",
	NodeKindAggregateVector: "AggregateVector",
	NodeKindMerge:           "Merge",
	NodeKindParse:           "Parse",
	NodeKindColumnCompat:    "ColumnCompat",
	NodeKindTopK:            "TopK",
	NodeKindScanSet:         "ScanSet",
	NodeKindParallelize:     "Parallelize",
	NodeKindJoin:            "Join",
}

func (k NodeKind) String() string {
	if int(k) < len(nodeKinds) {
		return nodeKinds[k]
	}
	return fmt.Sprintf("NodeKind(%d)", k)
}

// Node represents a single operation in a physical execution plan. It defines
// the core interface that all physical plan nodes must implement.
type Node interface {
	// isNode is a marker interface to denote a Node so that only types within this package
	// can implement this interface.
	isNode()

	// ID returns a string that uniquely identifies a node in the plan.
	ID() string

	// ulid returns the ULID value that uniquely identifies a node in the plan.
	ulid() ulid.ULID

	// Kind returns the kind for this node.
	Kind() NodeKind

	// Accept allows the node to be visited by a [Visitor], calling back to the
	// appropriate Node-specific Visit method on the Visitor interface.
	Accept(Visitor) error

	// ToPlanNode converts the node to a PlanNode.
	ToPlanNode() *PlanNode

	// Clone returns a deep copy of the node (minus its ID).
	Clone() Node

	// CloneWithNewID returns a deep copy of the node with a new ID.
	CloneWithNewID() Node
}

// GetNode returns the underlying Node from the PlanNode.
func GetNode(planNode *PlanNode) Node {
	switch kind := planNode.Kind.(type) {
	case *PlanNode_AggregateRange:
		return kind.AggregateRange
	case *PlanNode_AggregateVector:
		return kind.AggregateVector
	case *PlanNode_Scan:
		return kind.Scan
	case *PlanNode_Filter:
		return kind.Filter
	case *PlanNode_Limit:
		return kind.Limit
	case *PlanNode_Merge:
		return kind.Merge
	case *PlanNode_Parse:
		return kind.Parse
	case *PlanNode_Projection:
		return kind.Projection
	case *PlanNode_SortMerge:
		return kind.SortMerge
	case *PlanNode_ColumnCompat:
		return kind.ColumnCompat
	case *PlanNode_TopK:
		return kind.TopK
	case *PlanNode_ScanSet:
		return kind.ScanSet
	case *PlanNode_Parallelize:
		return kind.Parallelize
	case *PlanNode_Join:
		return kind.Join
	default:
		panic(fmt.Sprintf("unknown node kind %T", kind))
	}
}

// Visitor defines an interface for visiting each type of Node.
type Visitor interface {
	VisitAggregateRange(*AggregateRange) error
	VisitAggregateVector(*AggregateVector) error
	VisitDataObjScan(*DataObjScan) error
	VisitFilter(*Filter) error
	VisitLimit(*Limit) error
	VisitMerge(*Merge) error
	VisitParse(*Parse) error
	VisitProjection(*Projection) error
	VisitSortMerge(*SortMerge) error
	VisitColumnCompat(*ColumnCompat) error
	VisitTopK(*TopK) error
	VisitScanSet(*ScanSet) error
	VisitParallelize(*Parallelize) error
	VisitJoin(*Join) error
}

//
// Implementations of the Node interface for each type.
//

func (n *AggregateRange) isNode()  {}
func (n *AggregateVector) isNode() {}
func (n *DataObjScan) isNode()     {}
func (n *Filter) isNode()          {}
func (n *Limit) isNode()           {}
func (n *Merge) isNode()           {}
func (n *Parse) isNode()           {}
func (n *Projection) isNode()      {}
func (n *SortMerge) isNode()       {}
func (n *ColumnCompat) isNode()    {}
func (n *TopK) isNode()            {}
func (n *ScanSet) isNode()         {}
func (n *Parallelize) isNode()     {}
func (n *Join) isNode()            {}

func (n *AggregateRange) ID() string  { return n.GetId().Value.String() }
func (n *AggregateVector) ID() string { return n.GetId().Value.String() }
func (n *DataObjScan) ID() string     { return n.GetId().Value.String() }
func (n *Filter) ID() string          { return n.GetId().Value.String() }
func (n *Limit) ID() string           { return n.GetId().Value.String() }
func (n *Merge) ID() string           { return n.GetId().Value.String() }
func (n *Parse) ID() string           { return n.GetId().Value.String() }
func (n *Projection) ID() string      { return n.GetId().Value.String() }
func (n *SortMerge) ID() string       { return n.GetId().Value.String() }
func (n *ColumnCompat) ID() string    { return n.GetId().Value.String() }
func (n *TopK) ID() string            { return n.GetId().Value.String() }
func (n *ScanSet) ID() string         { return n.GetId().Value.String() }
func (n *Parallelize) ID() string     { return n.GetId().Value.String() }
func (n *Join) ID() string            { return n.GetId().Value.String() }

func (n *AggregateRange) ulid() ulid.ULID  { return n.GetId().Value }
func (n *AggregateVector) ulid() ulid.ULID { return n.GetId().Value }
func (n *DataObjScan) ulid() ulid.ULID     { return n.GetId().Value }
func (n *Filter) ulid() ulid.ULID          { return n.GetId().Value }
func (n *Limit) ulid() ulid.ULID           { return n.GetId().Value }
func (n *Merge) ulid() ulid.ULID           { return n.GetId().Value }
func (n *Parse) ulid() ulid.ULID           { return n.GetId().Value }
func (n *Projection) ulid() ulid.ULID      { return n.GetId().Value }
func (n *SortMerge) ulid() ulid.ULID       { return n.GetId().Value }
func (n *ColumnCompat) ulid() ulid.ULID    { return n.GetId().Value }
func (n *TopK) ulid() ulid.ULID            { return n.GetId().Value }
func (n *ScanSet) ulid() ulid.ULID         { return n.GetId().Value }
func (n *Parallelize) ulid() ulid.ULID     { return n.GetId().Value }
func (n *Join) ulid() ulid.ULID            { return n.GetId().Value }

func (n *AggregateRange) Kind() NodeKind  { return NodeKindAggregateRange }
func (n *AggregateVector) Kind() NodeKind { return NodeKindAggregateVector }
func (n *DataObjScan) Kind() NodeKind     { return NodeKindDataObjScan }
func (n *Filter) Kind() NodeKind          { return NodeKindFilter }
func (n *Limit) Kind() NodeKind           { return NodeKindLimit }
func (n *Merge) Kind() NodeKind           { return NodeKindMerge }
func (n *Parse) Kind() NodeKind           { return NodeKindParse }
func (n *Projection) Kind() NodeKind      { return NodeKindProjection }
func (n *SortMerge) Kind() NodeKind       { return NodeKindSortMerge }
func (n *ColumnCompat) Kind() NodeKind    { return NodeKindColumnCompat }
func (n *TopK) Kind() NodeKind            { return NodeKindTopK }
func (n *ScanSet) Kind() NodeKind         { return NodeKindScanSet }
func (n *Parallelize) Kind() NodeKind     { return NodeKindParallelize }
func (n *Join) Kind() NodeKind            { return NodeKindJoin }

func (n *AggregateRange) Accept(v Visitor) error  { return v.VisitAggregateRange(n) }
func (n *AggregateVector) Accept(v Visitor) error { return v.VisitAggregateVector(n) }
func (n *DataObjScan) Accept(v Visitor) error     { return v.VisitDataObjScan(n) }
func (n *Filter) Accept(v Visitor) error          { return v.VisitFilter(n) }
func (n *Limit) Accept(v Visitor) error           { return v.VisitLimit(n) }
func (n *Merge) Accept(v Visitor) error           { return v.VisitMerge(n) }
func (n *Parse) Accept(v Visitor) error           { return v.VisitParse(n) }
func (n *Projection) Accept(v Visitor) error      { return v.VisitProjection(n) }
func (n *SortMerge) Accept(v Visitor) error       { return v.VisitSortMerge(n) }
func (n *ColumnCompat) Accept(v Visitor) error    { return v.VisitColumnCompat(n) }
func (n *TopK) Accept(v Visitor) error            { return v.VisitTopK(n) }
func (n *ScanSet) Accept(v Visitor) error         { return v.VisitScanSet(n) }
func (n *Parallelize) Accept(v Visitor) error     { return v.VisitParallelize(n) }
func (n *Join) Accept(v Visitor) error            { return v.VisitJoin(n) }

func (n *AggregateRange) ToPlanNode() *PlanNode  { return planNode(&PlanNode_AggregateRange{n}) }
func (n *AggregateVector) ToPlanNode() *PlanNode { return planNode(&PlanNode_AggregateVector{n}) }
func (n *DataObjScan) ToPlanNode() *PlanNode     { return planNode(&PlanNode_Scan{n}) }
func (n *Filter) ToPlanNode() *PlanNode          { return planNode(&PlanNode_Filter{n}) }
func (n *Limit) ToPlanNode() *PlanNode           { return planNode(&PlanNode_Limit{n}) }
func (n *Merge) ToPlanNode() *PlanNode           { return planNode(&PlanNode_Merge{n}) }
func (n *Parse) ToPlanNode() *PlanNode           { return planNode(&PlanNode_Parse{n}) }
func (n *Projection) ToPlanNode() *PlanNode      { return planNode(&PlanNode_Projection{n}) }
func (n *SortMerge) ToPlanNode() *PlanNode       { return planNode(&PlanNode_SortMerge{n}) }
func (n *ColumnCompat) ToPlanNode() *PlanNode    { return planNode(&PlanNode_ColumnCompat{n}) }
func (n *TopK) ToPlanNode() *PlanNode            { return planNode(&PlanNode_TopK{n}) }
func (n *ScanSet) ToPlanNode() *PlanNode         { return planNode(&PlanNode_ScanSet{n}) }
func (n *Parallelize) ToPlanNode() *PlanNode     { return planNode(&PlanNode_Parallelize{n}) }
func (n *Join) ToPlanNode() *PlanNode            { return planNode(&PlanNode_Join{n}) }

// Clone returns a deep copy of the node (minus its ID).
func (n *AggregateRange) Clone() Node {
	return &AggregateRange{
		PartitionBy:    cloneColExpressions(n.PartitionBy),
		Operation:      n.Operation,
		StartUnixNanos: n.StartUnixNanos,
		EndUnixNanos:   n.EndUnixNanos,
		StepNs:         n.StepNs,
		RangeNs:        n.RangeNs,
	}
}
func (n *AggregateVector) Clone() Node {
	return &AggregateVector{
		GroupBy:   cloneColExpressions(n.GroupBy),
		Operation: n.Operation,
	}
}
func (n *DataObjScan) Clone() Node {
	return &DataObjScan{
		Location:    n.Location,
		Section:     n.Section,
		StreamIds:   slices.Clone(n.StreamIds),
		Projections: cloneColExpressions(n.Projections),
		Predicates:  cloneExpressions(n.Predicates),
	}
}
func (n *Filter) Clone() Node {
	return &Filter{
		Predicates: cloneExpressions(n.Predicates),
	}
}
func (n *Limit) Clone() Node {
	return &Limit{
		Skip:  n.Skip,
		Fetch: n.Fetch,
	}
}
func (n *Merge) Clone() Node {
	return &Merge{}
}
func (n *Parse) Clone() Node {
	return &Parse{
		Operation:     n.Operation,
		RequestedKeys: slices.Clone(n.RequestedKeys),
	}
}
func (n *Projection) Clone() Node {
	return &Projection{
		Expressions: cloneExpressions(n.Expressions),
		All:         n.All,
		Expand:      n.Expand,
		Drop:        n.Drop,
	}
}
func (n *SortMerge) Clone() Node {
	tmpCol := n.Column.Clone()
	return &SortMerge{
		Column: tmpCol.GetColumnExpression(),
		Order:  n.Order,
	}
}
func (n *ColumnCompat) Clone() Node {
	return &ColumnCompat{
		Source:      n.Source,
		Destination: n.Destination,
		Collision:   n.Collision,
	}
}
func (n *TopK) Clone() Node {
	tmp := (*n.SortBy).Clone()
	return &TopK{
		SortBy:     tmp.GetColumnExpression(),
		Ascending:  n.Ascending,
		NullsFirst: n.NullsFirst,
		K:          n.K,
	}
}
func (n *ScanSet) Clone() Node {
	newTargets := make([]*ScanTarget, 0, len(n.Targets))
	for _, target := range n.Targets {
		newTargets = append(newTargets, target.Clone())
	}

	return &ScanSet{Targets: newTargets}
}
func (n *Parallelize) Clone() Node {
	return &Parallelize{ /* nothing to clone */ }
}

func (n *Join) Clone() Node {
	return &Join{ /* nothing to clone */ }
}

func (t *ScanTarget) Clone() *ScanTarget {
	res := &ScanTarget{Type: t.Type}
	if t.DataObject != nil {
		res.DataObject = t.DataObject.Clone().(*DataObjScan)
	}
	return res
}

// CloneWithNewID returns a deep copy of the node with a new ID.
func (n *AggregateRange) CloneWithNewID() Node {
	tmp := n.Clone().ToPlanNode().GetAggregateRange()
	tmp.Id = PlanNodeID{ulid.New()}
	return tmp
}
func (n *AggregateVector) CloneWithNewID() Node {
	tmp := n.Clone().ToPlanNode().GetAggregateVector()
	tmp.Id = PlanNodeID{ulid.New()}
	return tmp
}
func (n *DataObjScan) CloneWithNewID() Node {
	tmp := n.Clone().ToPlanNode().GetScan()
	tmp.Id = PlanNodeID{ulid.New()}
	return tmp
}
func (n *Filter) CloneWithNewID() Node {
	tmp := n.Clone().ToPlanNode().GetFilter()
	tmp.Id = PlanNodeID{ulid.New()}
	return tmp

}
func (n *Limit) CloneWithNewID() Node {
	tmp := n.Clone().ToPlanNode().GetLimit()
	tmp.Id = PlanNodeID{ulid.New()}
	return tmp
}
func (n *Merge) CloneWithNewID() Node {
	tmp := n.Clone().ToPlanNode().GetMerge()
	tmp.Id = PlanNodeID{ulid.New()}
	return tmp
}
func (n *Parse) CloneWithNewID() Node {
	tmp := n.Clone().ToPlanNode().GetParse()
	tmp.Id = PlanNodeID{ulid.New()}
	return tmp
}
func (n *Projection) CloneWithNewID() Node {
	tmp := n.Clone().ToPlanNode().GetProjection()
	tmp.Id = PlanNodeID{ulid.New()}
	return tmp
}
func (n *SortMerge) CloneWithNewID() Node {
	tmp := n.Clone().ToPlanNode().GetSortMerge()
	tmp.Id = PlanNodeID{ulid.New()}
	return tmp
}
func (n *ColumnCompat) CloneWithNewID() Node {
	tmp := n.Clone().ToPlanNode().GetColumnCompat()
	tmp.Id = PlanNodeID{ulid.New()}
	return tmp
}
func (n *TopK) CloneWithNewID() Node {
	tmp := n.Clone().ToPlanNode().GetTopK()
	tmp.Id = PlanNodeID{ulid.New()}
	return tmp
}
func (n *ScanSet) CloneWithNewID() Node {
	tmp := n.Clone().ToPlanNode().GetScanSet()
	tmp.Id = PlanNodeID{ulid.New()}
	return tmp
}
func (n *Parallelize) CloneWithNewID() Node {
	tmp := n.Clone().ToPlanNode().GetParallelize()
	tmp.Id = PlanNodeID{ulid.New()}
	return tmp
}
func (n *Join) CloneWithNewID() Node {
	tmp := n.Clone().ToPlanNode().GetJoin()
	tmp.Id = PlanNodeID{ulid.New()}
	return tmp
}

func planNode(kind isPlanNode_Kind) *PlanNode {
	return &PlanNode{Kind: kind}
}

// ShardableNode is a Node that can be split into multiple smaller partitions.
type ShardableNode interface {
	Node

	// Shards produces a sequence of nodes that represent a fragment of the
	// original node. Returned nodes do not need to be the same type as the
	// original node.
	//
	// Implementations must produce unique values of Node in each call to
	// Shards.
	Shards() iter.Seq[Node]
}

// Shards returns an iterator over the shards of the scan. Each emitted shard
// will be a clone. Projections and predicates on the ScanSet are cloned and
// applied to each shard.
//
// Shards panics if one of the targets is invalid.
func (n *ScanSet) Shards() iter.Seq[Node] {
	return func(yield func(Node) bool) {
		for _, target := range n.Targets {
			switch target.Type {
			case SCAN_TYPE_DATA_OBJECT:
				node := target.DataObject.Clone().(*DataObjScan)
				node.Projections = cloneColExpressions(n.Projections)
				node.Predicates = cloneExpressions(n.Predicates)

				if !yield(node) {
					return
				}

			default:
				panic(fmt.Sprintf("invalid scan type %s", target.Type))
			}
		}
	}
}

var SupportedRangeAggregationTypes = []AggregateRangeOp{
	AGGREGATE_RANGE_OP_COUNT, AGGREGATE_RANGE_OP_SUM, AGGREGATE_RANGE_OP_MAX, AGGREGATE_RANGE_OP_MIN,
}

var SupportedVectorAggregationTypes = []AggregateVectorOp{AGGREGATE_VECTOR_OP_SUM, AGGREGATE_VECTOR_OP_MAX, AGGREGATE_VECTOR_OP_MIN, AGGREGATE_VECTOR_OP_COUNT}

// ColumnTypePrecedence returns the precedence of the given [ColumnType].
func ColumnTypePrecedence(ct ColumnType) int {
	switch ct {
	case COLUMN_TYPE_GENERATED:
		return PrecedenceGenerated
	case COLUMN_TYPE_PARSED:
		return PrecedenceParsed
	case COLUMN_TYPE_METADATA:
		return PrecedenceMetadata
	case COLUMN_TYPE_LABEL:
		return PrecedenceLabel
	default:
		return PrecedenceBuiltin // Default to lowest precedence
	}
}

// Column type precedence for ambiguous column resolution (highest to lowest):
// Generated > Parsed > Metadata > Label > Builtin
const (
	PrecedenceGenerated = iota // 0 - highest precedence

	PrecedenceParsed   // 1
	PrecedenceMetadata // 2
	PrecedenceLabel    // 3
	PrecedenceBuiltin  // 4 - lowest precedence
)

var ctNames = [7]string{"invalid", "builtin", "label", "metadata", "parsed", "ambiguous", "generated"}

// ColumnTypeFromString returns the [ColumnType] from its string representation.
func ColumnTypeFromString(ct string) ColumnType {
	switch ct {
	case ctNames[1]:
		return COLUMN_TYPE_BUILTIN
	case ctNames[2]:
		return COLUMN_TYPE_LABEL
	case ctNames[3]:
		return COLUMN_TYPE_METADATA
	case ctNames[4]:
		return COLUMN_TYPE_PARSED
	case ctNames[5]:
		return COLUMN_TYPE_AMBIGUOUS
	case ctNames[6]:
		return COLUMN_TYPE_GENERATED
	default:
		panic(fmt.Sprintf("invalid column type: %s", ct))
	}
}

func (p *Plan) NodeById(id PlanNodeID) Node {
	for _, n := range p.Nodes {
		if GetNode(n).ulid() == id.Value {
			return GetNode(n)
		}
	}
	return nil
}

func (p *Plan) NodeByStringId(id string) Node {
	for _, n := range p.Nodes {
		if GetNode(n).ID() == id {
			return GetNode(n)
		}
	}
	return nil
}

func (p *Plan) Roots() []Node {
	if len(p.Nodes) == 0 {
		return nil
	}

	var nodes = p.Nodes
	roots := []Node{}
	for _, n := range nodes {
		roots = append(roots, GetNode(n))
	}
	for _, edge := range p.Edges {
		if i := slices.Index(roots, p.NodeById(edge.Child)); i >= 0 {
			roots = append(roots[:i], roots[i+1:]...)
		}
	}
	return roots
}

func (p *Plan) Root() (Node, error) {
	roots := p.Roots()
	if len(roots) == 0 {
		return nil, fmt.Errorf("plan has no root node")
	}
	if len(roots) == 1 {
		return roots[0], nil
	}
	return nil, fmt.Errorf("plan has multiple root nodes")
}

func (p *Plan) Leaves() []Node {
	if len(p.Nodes) == 0 {
		return nil
	}

	var nodes = p.Nodes
	leaves := []Node{}
	for _, n := range nodes {
		leaves = append(leaves, GetNode(n))
	}
	for _, edge := range p.Edges {
		if i := slices.Index(leaves, p.NodeById(edge.Parent)); i >= 0 {
			leaves = append(leaves[:i], leaves[i+1:]...)
		}
	}
	return leaves
}

func (p *Plan) Parents(n Node) []Node {
	parents := []Node{}
	for _, e := range p.Edges {
		if e.Child.Value == n.ulid() {
			parents = append(parents, p.NodeById(e.Parent))
		}
	}
	return parents
}

func (p *Plan) Children(n Node) []Node {
	children := []Node{}
	for _, e := range p.Edges {
		if e.Parent.Value == n.ulid() {
			children = append(children, p.NodeById(e.Child))
		}
	}
	return children
}

func (p *Plan) Add(n Node) *PlanNode {
	if n == nil {
		return nil
	}
	if p.NodeById(PlanNodeID{n.ulid()}) == nil { // only add if it's not already present
		p.Nodes = append(p.Nodes, n.ToPlanNode())
	}
	return n.ToPlanNode()
}

func (p *Plan) AddEdge(e dag.Edge[Node]) error {
	if (e.Parent == nil) || (e.Child == nil) {
		return fmt.Errorf("parent and child nodes must not be zero values")
	}
	if e.Parent.ID() == e.Child.ID() {
		return fmt.Errorf("cannot connect a node (%v) to itself", e.Parent.ID())
	}
	if p.NodeByStringId(e.Parent.ID()) == nil || p.NodeByStringId(e.Child.ID()) == nil {
		return fmt.Errorf("both nodes %v and %v must already exist in the plan", e.Parent.ID(), e.Child.ID())
	}
	for _, edge := range p.Edges {
		if (edge.Parent.Value == e.Parent.ulid()) && (edge.Child.Value == e.Child.ulid()) {
			return fmt.Errorf("edge between node %v and %v already exists", e.Parent.ID(), e.Child.ID())
		}
	}
	p.Edges = append(p.Edges, &PlanEdge{PlanNodeID{Value: e.Parent.ulid()}, PlanNodeID{Value: e.Child.ulid()}})
	return nil
}

func (p *Plan) Eliminate(n Node) {
	if p.NodeByStringId(n.ID()) == nil {
		return // no node to eliminate
	}
	// For each parent p in the node to eliminate, push up n's children to
	// become children of p, and remove n as a child of p.
	parents := p.Parents(n)

	// First remove n as a child of p
	for i := 0; i < len(p.Edges); i++ {
		edge := p.Edges[i]
		if edge.Child.Value.String() == n.ID() {
			p.Edges = append(p.Edges[:i], p.Edges[i+1:]...)
			i--
		}
	}

	// Now push up n's children to become children of p
	for _, child := range p.Children(n) {
		for _, parent := range parents {
			edgeExists := false
			for _, e := range p.Edges {
				if e.Parent.Value.String() == parent.ID() && e.Child.Value.String() == child.ID() {
					// edge already exists, skip
					edgeExists = true
				}
			}
			if !edgeExists {
				p.AddEdge(dag.Edge[Node]{Parent: parent, Child: child})
			}
		}
	}
	// Next remove n as a parent of n's children
	for i := 0; i < len(p.Edges); i++ {
		edge := p.Edges[i]
		if edge.Parent.Value.String() == n.ID() {
			p.Edges = append(p.Edges[:i], p.Edges[i+1:]...)
			i--
		}
	}

	// Finally, remove n
	var nodeIds []ulid.ULID
	for _, node := range p.Nodes {
		nodeIds = append(nodeIds, GetNode(node).ulid())
	}
	nodeIdx := slices.Index(nodeIds, n.ulid()) // guaranteed to be >=0 since we found n earlier
	p.Nodes = append(p.Nodes[:nodeIdx], p.Nodes[nodeIdx+1:]...)
}

// Inject injects a new node between a parent and its children:
//
// * The children of parent become children of node.
// * The child of parent becomes node.
//
// Inject panics if given a node that already exists in the plan.
//
// For convenience, Inject returns node without modification.
func (p *Plan) Inject(parent, node Node) Node {
	if p.NodeByStringId(node.ID()) != nil {
		panic("injectNode: target node already exists in plan")
	}
	p.Add(node)

	// Update parent's children so that their parent is node.
	for i := 0; i < len(p.Edges); i++ {
		if p.Edges[i].Parent.Value.String() == parent.ID() {
			p.AddEdge(dag.Edge[Node]{Parent: node, Child: p.NodeByStringId(p.Edges[i].Child.Value.String())})
			p.Edges = append(p.Edges[:i], p.Edges[i+1:]...)
			i--
		}
	}

	// Add an edge between parent and node.
	p.AddEdge(dag.Edge[Node]{Parent: parent, Child: node})
	return node
}

// WalkFunc is a function that gets invoked when walking a Graph. Walking will
// stop if WalkFunc returns a non-nil error.
type WalkFunc func(n Node) error

func (p *Plan) VisitorWalk(n Node, v Visitor, o WalkOrder) error {
	return p.Walk(n, func(n Node) error { return n.Accept(v) }, o)
}

// Walk performs a depth-first walk of outgoing edges for all nodes in start,
// invoking the provided fn for each node. Walk returns the error returned by
// fn.
//
// Nodes unreachable from start will not be passed to fn.
func (p *Plan) Walk(n Node, f WalkFunc, order WalkOrder) error {
	visited := map[Node]bool{}
	switch order {
	case PRE_ORDER_WALK:
		return p.preOrderWalk(n, f, visited)
	case POST_ORDER_WALK:
		return p.postOrderWalk(n, f, visited)
	default:
		return errors.New("unsupported walk order. must be one of PreOrderWalk and PostOrderWalk")
	}
}

func (p *Plan) preOrderWalk(n Node, f WalkFunc, visited map[Node]bool) error {
	if visited[n] {
		return nil
	}
	visited[n] = true

	if err := f(n); err != nil {
		return err
	}

	for _, child := range p.Children(n) {
		if err := p.preOrderWalk(child, f, visited); err != nil {
			return err
		}
	}
	return nil
}

func (p *Plan) postOrderWalk(n Node, f WalkFunc, visited map[Node]bool) error {
	if visited[n] {
		return nil
	}
	visited[n] = true

	for _, child := range p.Children(n) {
		if err := p.postOrderWalk(child, f, visited); err != nil {
			return err
		}
	}

	return f(n)
}

// FromGraph constructs a Plan from a given DAG.
func FromGraph(graph dag.Graph[Node]) *Plan {
	p := &Plan{}
	for _, root := range graph.Roots() {
		fromGraphHelper(p, graph, root)
	}
	return p
}

func fromGraphHelper(p *Plan, g dag.Graph[Node], n Node) {
	p.Add(n)
	for _, c := range g.Children(n) {
		p.Add(c)
		p.AddEdge(dag.Edge[Node]{Parent: n, Child: c})
		fromGraphHelper(p, g, c)
	}
}

// Graph returns the underlying graph of the plan.
func (p *Plan) Graph() *dag.Graph[Node] {
	g := &dag.Graph[Node]{}
	for _, n := range p.Nodes {
		g.Add(GetNode(n))
	}
	for _, e := range p.Edges {
		g.AddEdge(dag.Edge[Node]{Parent: p.NodeById(e.Parent), Child: p.NodeById(e.Child)})
	}
	return g
}
