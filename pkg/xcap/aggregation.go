package xcap

import "math"

// AggregatedObservation holds an aggregated value for a statistic within a region.
type AggregatedObservation struct {
	Statistic Statistic
	Value     any
	Count     int // number of observations aggregated
}

// Record aggregates a new observation into this aggregated observation.
// It updates the value according to the statistic's aggregation type.
func (a *AggregatedObservation) Record(obs Observation) {
	a.aggregate(obs.statistic().Aggregation(), obs.value())
	a.Count++
}

// Merge aggregates another AggregatedObservation into this one.
func (a *AggregatedObservation) Merge(other *AggregatedObservation) {
	if other == nil {
		return
	}
	a.aggregate(a.Statistic.Aggregation(), other.Value)
	a.Count += other.Count
}

// mergeV2 merges a flattened V2 value without first constructing an
// intermediate AggregatedObservation.
func (a *AggregatedObservation) mergeV2(valueBits uint64, count int) {
	switch a.Statistic.DataType() {
	case DataTypeInt64:
		a.aggregateInt64(a.Statistic.Aggregation(), int64(valueBits))
	case DataTypeFloat64:
		a.aggregateFloat64(a.Statistic.Aggregation(), math.Float64frombits(valueBits))
	case DataTypeBool:
		a.aggregateBool(a.Statistic.Aggregation(), valueBits != 0)
	}
	a.Count += count
}

// newAggregatedObservationV2 creates an aggregated observation from a
// flattened V2 value.
func newAggregatedObservationV2(stat Statistic, valueBits uint64, count int) *AggregatedObservation {
	var value any
	switch stat.DataType() {
	case DataTypeInt64:
		value = int64(valueBits)
	case DataTypeFloat64:
		value = math.Float64frombits(valueBits)
	case DataTypeBool:
		value = valueBits != 0
	}

	return &AggregatedObservation{
		Statistic: stat,
		Value:     value,
		Count:     count,
	}
}

func (a *AggregatedObservation) aggregate(aggType AggregationType, val any) {
	switch aggType {
	case AggregationTypeSum:
		switch v := val.(type) {
		case int64:
			a.Value = a.Value.(int64) + v
		case float64:
			a.Value = a.Value.(float64) + v
		}
	case AggregationTypeMin:
		switch v := val.(type) {
		case int64:
			if v < a.Value.(int64) {
				a.Value = v
			}
		case float64:
			if v < a.Value.(float64) {
				a.Value = v
			}
		}
	case AggregationTypeMax:
		switch v := val.(type) {
		case int64:
			if v > a.Value.(int64) {
				a.Value = v
			}
		case float64:
			if v > a.Value.(float64) {
				a.Value = v
			}
		case bool:
			// For flags, true > false
			if v {
				a.Value = v
			}
		}
	case AggregationTypeLast:
		a.Value = val
	case AggregationTypeFirst:
		// Keep the first value, don't update
		if a.Value == nil {
			a.Value = val
		}
	}
}

func (a *AggregatedObservation) aggregateInt64(aggType AggregationType, value int64) {
	switch aggType {
	case AggregationTypeSum:
		a.Value = a.Value.(int64) + value
	case AggregationTypeMin:
		if value < a.Value.(int64) {
			a.Value = value
		}
	case AggregationTypeMax:
		if value > a.Value.(int64) {
			a.Value = value
		}
	case AggregationTypeLast:
		a.Value = value
	}
}

func (a *AggregatedObservation) aggregateFloat64(aggType AggregationType, value float64) {
	switch aggType {
	case AggregationTypeSum:
		a.Value = a.Value.(float64) + value
	case AggregationTypeMin:
		if value < a.Value.(float64) {
			a.Value = value
		}
	case AggregationTypeMax:
		if value > a.Value.(float64) {
			a.Value = value
		}
	case AggregationTypeLast:
		a.Value = value
	}
}

func (a *AggregatedObservation) aggregateBool(aggType AggregationType, value bool) {
	switch aggType {
	case AggregationTypeMax:
		if value {
			a.Value = true
		}
	case AggregationTypeLast:
		a.Value = value
	}
}

func (a *AggregatedObservation) Int64() (int64, bool) {
	if a == nil {
		return 0, false
	} else if a.Statistic.DataType() != DataTypeInt64 {
		return 0, false
	}

	return a.Value.(int64), true
}

func (a *AggregatedObservation) Float64() (float64, bool) {
	if a == nil {
		return 0, false
	} else if a.Statistic.DataType() != DataTypeFloat64 {
		return 0, false
	}

	return a.Value.(float64), true
}

func (a *AggregatedObservation) Bool() bool {
	if a == nil {
		return false
	} else if a.Statistic.DataType() != DataTypeBool {
		return false
	}

	return a.Value.(bool)
}
