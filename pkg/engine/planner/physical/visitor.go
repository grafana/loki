package physical

// Visitor defines the interface for objects that can visit each type of
// physical plan node. It implements the Visitor pattern, providing
// type-specific visit methods for each concrete node type in the physical
// plan.
type Visitor interface {
	VisitDataObjScan(*DataObjScan) error
	VisitSortMerge(*SortMerge) error
	VisitProjection(*Projection) error
	VisitRangeAggregation(*RangeAggregation) error
	VisitFilter(*Filter) error
	VisitLimit(*Limit) error
	VisitVectorAggregation(*VectorAggregation) error
}
