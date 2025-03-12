package physical

type Traversable interface {
	Accept(Visitor) error
}

type Visitor interface {
	VisitDataObjScan(*DataObjScan) error
	VisitSortMerge(*SortMerge) error
	VisitProjection(*Projection) error
	VisitFilter(*Filter) error
	VisitLimit(*Limit) error
}
