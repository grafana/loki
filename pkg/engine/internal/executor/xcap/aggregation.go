package xcap

// observationValue holds an aggregated value for a statistic within a region.
type observationValue struct {
	statistic Statistic
	value     interface{}
	count     int // number of observations aggregated
}

// aggregateObservation aggregates a new observation with an existing aggregated value.
// It returns the updated observationValue with the new value aggregated according to
// the statistic's aggregation type.
func aggregateObservation(existing observationValue, newObs Observation) observationValue {
	stat := newObs.statistic()
	newVal := newObs.value()

	switch stat.Aggregation() {
	case AggregationTypeSum:
		switch v := newVal.(type) {
		case int64:
			existing.value = existing.value.(int64) + v
		case float64:
			existing.value = existing.value.(float64) + v
		}

	case AggregationTypeMin:
		switch v := newVal.(type) {
		case int64:
			if v < existing.value.(int64) {
				existing.value = v
			}
		case float64:
			if v < existing.value.(float64) {
				existing.value = v
			}
		}

	case AggregationTypeMax:
		switch v := newVal.(type) {
		case int64:
			if v > existing.value.(int64) {
				existing.value = v
			}
		case float64:
			if v > existing.value.(float64) {
				existing.value = v
			}
		case bool:
			// For flags, true > false
			if v {
				existing.value = v
			}
		}

	case AggregationTypeLast:
		// Last value overwrites
		existing.value = newVal

	case AggregationTypeFirst:
		// First value is kept, do nothing
	}

	existing.count++
	return existing
}

