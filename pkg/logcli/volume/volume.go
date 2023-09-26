package volume

import "time"

type Query struct {
	QueryString       string
	Start             time.Time
	End               time.Time
	Step              time.Duration
	Quiet             bool
	Limit             int
	TargetLabels      []string
	AggregateByLabels bool
}
