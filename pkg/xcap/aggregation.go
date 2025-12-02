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
	stat := obs.statistic()
	val := obs.value()

	switch stat.Aggregation() {
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
		// Last value overwrites
		a.Value = val

	case AggregationTypeFirst:
		if a.Value == nil {
			a.Value = val
		}
	}

	a.Count++
}

// Merge aggregates another AggregatedObservation into this one.
// Both observations must be for the same statistic.
func (a *AggregatedObservation) Merge(other *AggregatedObservation) {
	if other == nil {
		return
	}

	switch a.Statistic.Aggregation() {
	case AggregationTypeSum:
		switch v := other.Value.(type) {
		case int64:
			a.Value = a.Value.(int64) + v
		case float64:
			a.Value = a.Value.(float64) + v
		}
	case AggregationTypeMin:
		switch v := other.Value.(type) {
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
		switch v := other.Value.(type) {
		case int64:
			if v > a.Value.(int64) {
				a.Value = v
			}
		case float64:
			if v > a.Value.(float64) {
				a.Value = v
			}
		case bool:
			if v {
				a.Value = v
			}
		}
	case AggregationTypeLast:
		a.Value = other.Value
	case AggregationTypeFirst:
		// Keep the first value, don't update
	}
	a.Count += other.Count
}
