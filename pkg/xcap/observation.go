package xcap

// Observation holds a value for a particular statistic. Observations
// are created from statistics and then recorded into a Region using
// [Region.Record].
type Observation interface {
	// statistic returns the statistic this observation is for.
	statistic() Statistic
	// value returns the raw value of this observation.
	value() any
}

// observation is the internal implementation of Observation.
type observation struct {
	stat Statistic
	val  any
}

func (o *observation) statistic() Statistic {
	return o.stat
}

func (o *observation) value() any {
	return o.val
}
