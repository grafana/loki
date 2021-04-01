package telegraf

import (
	"time"
)

// Accumulator allows adding metrics to the processing flow.
type Accumulator interface {
	// AddFields adds a metric to the accumulator with the given measurement
	// name, fields, and tags (and timestamp). If a timestamp is not provided,
	// then the accumulator sets it to "now".
	AddFields(measurement string,
		fields map[string]interface{},
		tags map[string]string,
		t ...time.Time)

	// AddGauge is the same as AddFields, but will add the metric as a "Gauge" type
	AddGauge(measurement string,
		fields map[string]interface{},
		tags map[string]string,
		t ...time.Time)

	// AddCounter is the same as AddFields, but will add the metric as a "Counter" type
	AddCounter(measurement string,
		fields map[string]interface{},
		tags map[string]string,
		t ...time.Time)

	// AddSummary is the same as AddFields, but will add the metric as a "Summary" type
	AddSummary(measurement string,
		fields map[string]interface{},
		tags map[string]string,
		t ...time.Time)

	// AddHistogram is the same as AddFields, but will add the metric as a "Histogram" type
	AddHistogram(measurement string,
		fields map[string]interface{},
		tags map[string]string,
		t ...time.Time)

	// AddMetric adds an metric to the accumulator.
	AddMetric(Metric)

	// SetPrecision sets the timestamp rounding precision.  All metrics addeds
	// added to the accumulator will have their timestamp rounded to the
	// nearest multiple of precision.
	SetPrecision(precision time.Duration)

	// Report an error.
	AddError(err error)

	// Upgrade to a TrackingAccumulator with space for maxTracked
	// metrics/batches.
	WithTracking(maxTracked int) TrackingAccumulator
}

// TrackingID uniquely identifies a tracked metric group
type TrackingID uint64

// DeliveryInfo provides the results of a delivered metric group.
type DeliveryInfo interface {
	// ID is the TrackingID
	ID() TrackingID

	// Delivered returns true if the metric was processed successfully.
	Delivered() bool
}

// TrackingAccumulator is an Accumulator that provides a signal when the
// metric has been fully processed.  Sending more metrics than the accumulator
// has been allocated for without reading status from the Accepted or Rejected
// channels is an error.
type TrackingAccumulator interface {
	Accumulator

	// Add the Metric and arrange for tracking feedback after processing..
	AddTrackingMetric(m Metric) TrackingID

	// Add a group of Metrics and arrange for a signal when the group has been
	// processed.
	AddTrackingMetricGroup(group []Metric) TrackingID

	// Delivered returns a channel that will contain the tracking results.
	Delivered() <-chan DeliveryInfo
}
