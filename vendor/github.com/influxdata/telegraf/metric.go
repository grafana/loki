package telegraf

import (
	"time"
)

// ValueType is an enumeration of metric types that represent a simple value.
type ValueType int

// Possible values for the ValueType enum.
const (
	_ ValueType = iota
	Counter
	Gauge
	Untyped
	Summary
	Histogram
)

// Tag represents a single tag key and value.
type Tag struct {
	Key   string
	Value string
}

// Field represents a single field key and value.
type Field struct {
	Key   string
	Value interface{}
}

// Metric is the type of data that is processed by Telegraf.  Input plugins,
// and to a lesser degree, Processor and Aggregator plugins create new Metrics
// and Output plugins write them.
//
//nolint:interfacebloat // conditionally allow to contain more methods
type Metric interface {
	// Name is the primary identifier for the Metric and corresponds to the
	// measurement in the InfluxDB data model.
	Name() string

	// Tags returns the tags as a map.  This method is deprecated, use TagList instead.
	Tags() map[string]string

	// TagList returns the tags as a slice ordered by the tag key in lexical
	// bytewise ascending order.  The returned value should not be modified,
	// use the AddTag or RemoveTag methods instead.
	TagList() []*Tag

	// Fields returns the fields as a map.  This method is deprecated, use FieldList instead.
	Fields() map[string]interface{}

	// FieldList returns the fields as a slice in an undefined order.  The
	// returned value should not be modified, use the AddField or RemoveField
	// methods instead.
	FieldList() []*Field

	// Time returns the timestamp of the metric.
	Time() time.Time

	// Type returns a general type for the entire metric that describes how you
	// might interpret, aggregate the values. Used by prometheus and statsd.
	Type() ValueType

	// SetName sets the metric name.
	SetName(name string)

	// AddPrefix adds a string to the front of the metric name.  It is
	// equivalent to m.SetName(prefix + m.Name()).
	//
	// This method is deprecated, use SetName instead.
	AddPrefix(prefix string)

	// AddSuffix appends a string to the back of the metric name.  It is
	// equivalent to m.SetName(m.Name() + suffix).
	//
	// This method is deprecated, use SetName instead.
	AddSuffix(suffix string)

	// GetTag returns the value of a tag and a boolean to indicate if it was set.
	GetTag(key string) (string, bool)

	// HasTag returns true if the tag is set on the Metric.
	HasTag(key string) bool

	// AddTag sets the tag on the Metric.  If the Metric already has the tag
	// set then the current value is replaced.
	AddTag(key, value string)

	// RemoveTag removes the tag if it is set.
	RemoveTag(key string)

	// GetField returns the value of a field and a boolean to indicate if it was set.
	GetField(key string) (interface{}, bool)

	// HasField returns true if the field is set on the Metric.
	HasField(key string) bool

	// AddField sets the field on the Metric.  If the Metric already has the field
	// set then the current value is replaced.
	AddField(key string, value interface{})

	// RemoveField removes the tag if it is set.
	RemoveField(key string)

	// SetTime sets the timestamp of the Metric.
	SetTime(t time.Time)

	// SetType sets the value-type of the Metric.
	SetType(t ValueType)

	// HashID returns an unique identifier for the series.
	HashID() uint64

	// Copy returns a deep copy of the Metric.
	Copy() Metric

	// Accept marks the metric as processed successfully and written to an
	// output.
	Accept()

	// Reject marks the metric as processed unsuccessfully.
	Reject()

	// Drop marks the metric as processed successfully without being written
	// to any output.
	Drop()
}

// TemplateMetric is an interface to use in templates (e.g text/template)
// to generate complex strings from metric properties
// e.g. '{{.Name}}-{{.Tag "foo"}}-{{.Field "bar"}}'
type TemplateMetric interface {
	Name() string
	Field(key string) interface{}
	Fields() map[string]interface{}
	Tag(key string) string
	Tags() map[string]string
	Time() time.Time
	String() string
}

type UnwrappableMetric interface {
	// Unwrap allows to access the underlying raw metric if an implementation
	// wraps it in the first place.
	Unwrap() Metric
}

type TrackingMetric interface {
	// TrackingID returns the ID used for tracking the metric
	TrackingID() TrackingID
	TrackingData() TrackingData
	UnwrappableMetric
}
