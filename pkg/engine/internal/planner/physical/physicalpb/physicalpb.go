// Package physicalpb contains the protobuf definitions for physical plan nodes.
package physicalpb

import fmt "fmt"

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
}

func (k NodeKind) String() string {
	if k >= 0 && int(k) < len(nodeKinds) {
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

	// Kind returns the kind for this node.
	Kind() NodeKind

	// Accept allows the node to be visited by a [Visitor], calling back to the
	// appropriate Node-specific Visit method on the Visitor interface.
	Accept(Visitor) error

	// ToPlanNode converts the node to a PlanNode.
	ToPlanNode() *PlanNode
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

func (n *AggregateRange) ID() string  { return n.GetId().Value.String() }
func (n *AggregateVector) ID() string { return n.GetId().Value.String() }
func (n *DataObjScan) ID() string     { return n.GetId().Value.String() }
func (n *Filter) ID() string          { return n.GetId().Value.String() }
func (n *Limit) ID() string           { return n.GetId().Value.String() }
func (n *Merge) ID() string           { return n.GetId().Value.String() }
func (n *Parse) ID() string           { return n.GetId().Value.String() }
func (n *Projection) ID() string      { return n.GetId().Value.String() }
func (n *SortMerge) ID() string       { return n.GetId().Value.String() }

func (n *AggregateRange) Kind() NodeKind  { return NodeKindAggregateRange }
func (n *AggregateVector) Kind() NodeKind { return NodeKindAggregateVector }
func (n *DataObjScan) Kind() NodeKind     { return NodeKindDataObjScan }
func (n *Filter) Kind() NodeKind          { return NodeKindFilter }
func (n *Limit) Kind() NodeKind           { return NodeKindLimit }
func (n *Merge) Kind() NodeKind           { return NodeKindMerge }
func (n *Parse) Kind() NodeKind           { return NodeKindParse }
func (n *Projection) Kind() NodeKind      { return NodeKindProjection }
func (n *SortMerge) Kind() NodeKind       { return NodeKindSortMerge }

func (n *AggregateRange) Accept(v Visitor) error  { return v.VisitAggregateRange(n) }
func (n *AggregateVector) Accept(v Visitor) error { return v.VisitAggregateVector(n) }
func (n *DataObjScan) Accept(v Visitor) error     { return v.VisitDataObjScan(n) }
func (n *Filter) Accept(v Visitor) error          { return v.VisitFilter(n) }
func (n *Limit) Accept(v Visitor) error           { return v.VisitLimit(n) }
func (n *Merge) Accept(v Visitor) error           { return v.VisitMerge(n) }
func (n *Parse) Accept(v Visitor) error           { return v.VisitParse(n) }
func (n *Projection) Accept(v Visitor) error      { return v.VisitProjection(n) }
func (n *SortMerge) Accept(v Visitor) error       { return v.VisitSortMerge(n) }

func (n *AggregateRange) ToPlanNode() *PlanNode  { return planNode(&PlanNode_AggregateRange{n}) }
func (n *AggregateVector) ToPlanNode() *PlanNode { return planNode(&PlanNode_AggregateVector{n}) }
func (n *DataObjScan) ToPlanNode() *PlanNode     { return planNode(&PlanNode_Scan{n}) }
func (n *Filter) ToPlanNode() *PlanNode          { return planNode(&PlanNode_Filter{n}) }
func (n *Limit) ToPlanNode() *PlanNode           { return planNode(&PlanNode_Limit{n}) }
func (n *Merge) ToPlanNode() *PlanNode           { return planNode(&PlanNode_Merge{n}) }
func (n *Parse) ToPlanNode() *PlanNode           { return planNode(&PlanNode_Parse{n}) }
func (n *Projection) ToPlanNode() *PlanNode      { return planNode(&PlanNode_Projection{n}) }
func (n *SortMerge) ToPlanNode() *PlanNode       { return planNode(&PlanNode_SortMerge{n}) }

func planNode(kind isPlanNode_Kind) *PlanNode {
	return &PlanNode{Kind: kind}
}
