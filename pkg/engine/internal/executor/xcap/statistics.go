package xcap

// AggregationType specifies how to combine multiple observations of the
// same statistic.
type AggregationType int

const (
	// AggregationTypeSum sums all observations together into a
	// final value.
	AggregationTypeSum AggregationType = iota
	// AggregationTypeMin uses the smallest value of all observations.
	AggregationTypeMin
	// AggregationTypeMax uses the largest value of all observations.
	AggregationTypeMax
	// AggregationTypeLast uses the last recorded observation value.
	AggregationTypeLast
	// AggregationTypeFirst uses the first recorded observation value.
	AggregationTypeFirst
)

// Statistic is the interface that all statistic types implement.
type Statistic interface {
	// Name returns the name of the statistic.
	Name() string
	// Aggregation returns the aggregation type for this statistic.
	Aggregation() AggregationType
}

// StatisticInt64 tracks a numerical statistic as a signed integer.
type StatisticInt64 struct {
	name        string
	aggregation AggregationType
}

// NewStatisticInt64 creates a new numeric statistic holding signed
// integers.
func NewStatisticInt64(name string, agg AggregationType) *StatisticInt64 {
	return &StatisticInt64{
		name:        name,
		aggregation: agg,
	}
}

// Name returns the name of the statistic.
func (s *StatisticInt64) Name() string {
	return s.name
}

// Aggregation returns the aggregation type for this statistic.
func (s *StatisticInt64) Aggregation() AggregationType {
	return s.aggregation
}

// Observe creates a new observation for the statistic with the given value.
// Observations have no effect until recorded into a Region with [Region.Record].
func (s *StatisticInt64) Observe(value int64) Observation {
	return &observation{
		stat: s,
		val:  value,
	}
}

// StatisticFloat64 tracks a numerical statistic as a double
// precision floating point value.
type StatisticFloat64 struct {
	name        string
	aggregation AggregationType
}

// NewStatisticFloat64 creates a new numeric statistic holding double
// precision floating point values.
func NewStatisticFloat64(name string, agg AggregationType) *StatisticFloat64 {
	return &StatisticFloat64{
		name:        name,
		aggregation: agg,
	}
}

// Name returns the name of the statistic.
func (s *StatisticFloat64) Name() string {
	return s.name
}

// Aggregation returns the aggregation type for this statistic.
func (s *StatisticFloat64) Aggregation() AggregationType {
	return s.aggregation
}

// Observe creates a new observation for the statistic with the given value.
// Observations have no effect until recorded into a Region with [Region.Record].
func (s *StatisticFloat64) Observe(value float64) Observation {
	return &observation{
		stat: s,
		val:  value,
	}
}

// StatisticFlag tracks a boolean condition. When aggregating across
// Regions, a StatisticFlag will be true if any Region recorded an
// observation with a value of true.
type StatisticFlag struct {
	name string
}

// NewStatisticFlag creates a new statistic holding a boolean condition.
func NewStatisticFlag(name string) *StatisticFlag {
	return &StatisticFlag{
		name: name,
	}
}

// Name returns the name of the statistic.
func (s *StatisticFlag) Name() string {
	return s.name
}

// Aggregation returns the aggregation type for this statistic.
// Flags always use MAX aggregation (true > false).
func (s *StatisticFlag) Aggregation() AggregationType {
	return AggregationTypeMax
}

// Observe creates a new observation for the statistic with the given value.
// Observations have no effect until recorded into a Region with [Region.Record].
func (s *StatisticFlag) Observe(value bool) Observation {
	return &observation{
		stat: s,
		val:  value,
	}
}
