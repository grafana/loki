package xcap

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

func (a *AggregatedObservation) Int64() (int64, bool) {
	if a.Statistic.DataType() != DataTypeInt64 {
		return 0, false
	}

	return a.Value.(int64), true
}

func (a *AggregatedObservation) Float64() (float64, bool) {
	if a.Statistic.DataType() != DataTypeFloat64 {
		return 0, false
	}

	return a.Value.(float64), true
}

func (a *AggregatedObservation) Bool() bool {
	if a.Statistic.DataType() != DataTypeBool {
		return false
	}

	return a.Value.(bool)
}
