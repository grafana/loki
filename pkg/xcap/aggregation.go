package xcap

// AggregatedObservation holds an aggregated value for a statistic within a region.
type AggregatedObservation struct {
	Statistic Statistic
	value     value
	Count     int // number of observations aggregated
}

// Record aggregates a new observation into this aggregated observation.
// It updates the value according to the statistic's aggregation type.
func (a *AggregatedObservation) Record(obs Observation) {
	a.aggregate(obs.stat.Aggregation(), obs.val)
	a.Count++
}

// Merge aggregates another AggregatedObservation into this one.
func (a *AggregatedObservation) Merge(other *AggregatedObservation) {
	if other == nil {
		return
	}
	a.aggregate(a.Statistic.Aggregation(), other.value)
	a.Count += other.Count
}

// aggregate folds val into the accumulator using the statistic's data type and
// aggregation. An empty accumulator (Count == 0) is seeded with the incoming
// value regardless of aggregation type, so the first value always wins.
func (a *AggregatedObservation) aggregate(aggType AggregationType, val value) {
	if a.Count == 0 {
		a.value = val
		return
	}

	switch a.Statistic.DataType() {
	case DataTypeInt64:
		a.aggregateInt64(aggType, val)
	case DataTypeFloat64:
		a.aggregateFloat64(aggType, val)
	case DataTypeBool:
		a.aggregateBool(aggType, val)
	}
}

func (a *AggregatedObservation) aggregateInt64(aggType AggregationType, val value) {
	switch aggType {
	case AggregationTypeSum:
		a.value = int64Value(a.value.int64() + val.int64())
	case AggregationTypeMin:
		if val.int64() < a.value.int64() {
			a.value = val
		}
	case AggregationTypeMax:
		if val.int64() > a.value.int64() {
			a.value = val
		}
	case AggregationTypeLast:
		a.value = val
	case AggregationTypeFirst:
		// Keep the first value.
	}
}

func (a *AggregatedObservation) aggregateFloat64(aggType AggregationType, val value) {
	switch aggType {
	case AggregationTypeSum:
		a.value = float64Value(a.value.float64() + val.float64())
	case AggregationTypeMin:
		if val.float64() < a.value.float64() {
			a.value = val
		}
	case AggregationTypeMax:
		if val.float64() > a.value.float64() {
			a.value = val
		}
	case AggregationTypeLast:
		a.value = val
	case AggregationTypeFirst:
		// Keep the first value.
	}
}

func (a *AggregatedObservation) aggregateBool(aggType AggregationType, val value) {
	switch aggType {
	case AggregationTypeMax:
		// For flags, true > false.
		if val.bool() {
			a.value = val
		}
	case AggregationTypeLast:
		a.value = val
	case AggregationTypeFirst:
		// Keep the first value.
	}
}

// Value returns the aggregated value as a typed interface based on the
// statistic's data type. It returns nil for a nil receiver or an unknown data
// type.
func (a *AggregatedObservation) Value() any {
	if a == nil {
		return nil
	}

	switch a.Statistic.DataType() {
	case DataTypeInt64:
		return a.value.int64()
	case DataTypeFloat64:
		return a.value.float64()
	case DataTypeBool:
		return a.value.bool()
	default:
		return nil
	}
}

func (a *AggregatedObservation) Int64() (int64, bool) {
	if a == nil {
		return 0, false
	} else if a.Statistic.DataType() != DataTypeInt64 {
		return 0, false
	}

	return a.value.int64(), true
}

func (a *AggregatedObservation) Float64() (float64, bool) {
	if a == nil {
		return 0, false
	} else if a.Statistic.DataType() != DataTypeFloat64 {
		return 0, false
	}

	return a.value.float64(), true
}

func (a *AggregatedObservation) Bool() bool {
	if a == nil {
		return false
	} else if a.Statistic.DataType() != DataTypeBool {
		return false
	}

	return a.value.bool()
}
