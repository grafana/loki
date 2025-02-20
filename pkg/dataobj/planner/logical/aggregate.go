package logical

type aggregateOp string

const (
	aggregateOpSum   aggregateOp = "sum"
	aggregateOpAvg   aggregateOp = "avg"
	aggregateOpMin   aggregateOp = "min"
	aggregateOpMax   aggregateOp = "max"
	aggregateOpCount aggregateOp = "count"
)
