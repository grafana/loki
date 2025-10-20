package physical

// Visitor defines the interface for objects that can visit each type of
// physical plan node. It implements the Visitor pattern, providing
// type-specific visit methods for each concrete node type in the physical
// plan.
type Visitor interface {
	VisitDataObjScan(*DataObjScan) error
	VisitProjection(*Projection) error
	VisitRangeAggregation(*RangeAggregation) error
	VisitFilter(*Filter) error
	VisitLimit(*Limit) error
	VisitVectorAggregation(*VectorAggregation) error
	VisitParse(*ParseNode) error
	VisitCompat(*ColumnCompat) error
	VisitTopK(*TopK) error
	VisitParallelize(*Parallelize) error
	VisitScanSet(*ScanSet) error
}
