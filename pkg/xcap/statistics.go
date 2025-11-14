package xcap

// DataType specifies the data type of a statistic's values.
type DataType int

const (
	// DataTypeInvalid represents an invalid/unspecified data type.
	DataTypeInvalid DataType = iota
	// DataTypeInt64 represents a signed 64-bit integer statistic.
	DataTypeInt64
	// DataTypeFloat64 represents a double precision floating point statistic.
	DataTypeFloat64
	// DataTypeBool represents a boolean statistic (flag).
	DataTypeBool
)

// AggregationType specifies how to combine multiple observations of the
// same statistic.
type AggregationType int

const (
	// AggregationTypeInvalid represents an invalid/unspecified aggregation type.
	AggregationTypeInvalid AggregationType = iota
	// AggregationTypeSum sums all observations together into a
	// final value.
	AggregationTypeSum
	// AggregationTypeMin uses the smallest value of all observations.
	AggregationTypeMin
	// AggregationTypeMax uses the largest value of all observations.
	AggregationTypeMax
	// AggregationTypeLast uses the last recorded observation value.
	AggregationTypeLast
	// AggregationTypeFirst uses the first recorded observation value.
	AggregationTypeFirst
)

// StatisticKey is a comparable struct that uniquely identifies a statistic
// by its definition (name, data type, and aggregation type).
type StatisticKey struct {
	Name        string
	DataType    DataType
	Aggregation AggregationType
}

// Statistic is the interface that all statistic types implement.
type Statistic interface {
	Name() string
	DataType() DataType
	Aggregation() AggregationType
	Key() StatisticKey
}

// statistic holds the common definition of a statistic.
type statistic struct {
	name        string
	dataType    DataType
	aggregation AggregationType
}

// Name returns the name of the statistic.
func (s *statistic) Name() string {
	return s.name
}

// DataType returns the data type of the statistic.
func (s *statistic) DataType() DataType {
	return s.dataType
}

// Aggregation returns the aggregation type for this statistic.
func (s *statistic) Aggregation() AggregationType {
	return s.aggregation
}

// Key returns a StatisticKey that uniquely identifies this statistic.
// Statistics with the same definition will have the same key.
func (s *statistic) Key() StatisticKey {
	return StatisticKey{
		Name:        s.name,
		DataType:    s.dataType,
		Aggregation: s.aggregation,
	}
}

// StatisticInt64 is a statistic for int64 values.
type StatisticInt64 struct {
	statistic
}

// Observe creates an observation with an int64 value.
func (s *StatisticInt64) Observe(value int64) Observation {
	return &observation{
		stat: s,
		val:  value,
	}
}

// StatisticFloat64 is a statistic for float64 values.
type StatisticFloat64 struct {
	statistic
}

// Observe creates an observation with a float64 value.
func (s *StatisticFloat64) Observe(value float64) Observation {
	return &observation{
		stat: s,
		val:  value,
	}
}

// StatisticFlag is a statistic for bool values.
type StatisticFlag struct {
	statistic
}

// Observe creates an observation with a bool value.
func (s *StatisticFlag) Observe(value bool) Observation {
	return &observation{
		stat: s,
		val:  value,
	}
}

// NewStatisticInt64 creates a new int64 statistic with the given name and aggregation.
func NewStatisticInt64(name string, aggregation AggregationType) *StatisticInt64 {
	return &StatisticInt64{
		statistic: statistic{
			name:        name,
			dataType:    DataTypeInt64,
			aggregation: aggregation,
		},
	}
}

// NewStatisticFloat64 creates a new float64 statistic with the given name and aggregation.
func NewStatisticFloat64(name string, aggregation AggregationType) *StatisticFloat64 {
	return &StatisticFloat64{
		statistic: statistic{
			name:        name,
			dataType:    DataTypeFloat64,
			aggregation: aggregation,
		},
	}
}

// NewStatisticFlag creates a new bool statistic (flag) with the given name.
// Flags always use AggregationTypeMax (true > false).
func NewStatisticFlag(name string) *StatisticFlag {
	return &StatisticFlag{
		statistic: statistic{
			name:        name,
			dataType:    DataTypeBool,
			aggregation: AggregationTypeMax,
		},
	}
}
