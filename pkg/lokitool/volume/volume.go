package volume

import "time"

// Query contains the parameters of a volume query.
type Query struct {
	QueryString       string
	Start             time.Time
	End               time.Time
	Limit             int
	TargetLabels      []string
	AggregateByLabels bool
}
