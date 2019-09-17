package v1

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/prometheus/prometheus/promql"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
)

// QueryStatus holds the status of a query
type QueryStatus string

// QueryStatus values
const (
	QueryStatusSuccess = "success"
)

//QueryResponse represents the http json response to a label query
type QueryResponse struct {
	Status string            `json:"status"`
	Data   QueryResponseData `json:"data"`
}

// ResultType holds the type of the result
type ResultType string

// ResultType values
const (
	ResultTypeStream = "streams"
	ResultTypeVector = "vector"
	ResultTypeMatrix = "matrix"
)

// ResultValue interface mimics the promql.Value interface
type ResultValue interface {
	Type() ResultType
}

//QueryResponseData represents the http json response to a label query
type QueryResponseData struct {
	ResultType ResultType  `json:"resultType"`
	Result     ResultValue `json:"result"`
}

// Type implements the promql.Value interface
func (Streams) Type() ResultType { return ResultTypeStream }

// Type implements the promql.Value interface
func (Vector) Type() ResultType { return ResultTypeVector }

// Type implements the promql.Value interface
func (Matrix) Type() ResultType { return ResultTypeMatrix }

// Streams is a slice of Stream
type Streams []Stream

//Stream represents a log stream.  It includes a set of log entries and their labels.
type Stream struct {
	Labels  LabelSet `json:"stream"`
	Entries []Entry  `json:"values"`
}

//Entry represents a log entry.  It includes a log message and the time it occurred at.
type Entry struct {
	Timestamp time.Time
	Line      string
}

// MarshalJSON implements the json.Marshaler interface.
func (e *Entry) MarshalJSON() ([]byte, error) {
	l, err := json.Marshal(e.Line)
	if err != nil {
		return nil, err
	}
	return []byte(fmt.Sprintf("[\"%d\",%s]", e.Timestamp.UnixNano(), l)), nil
}

// UnmarshalJSON implements the json.Unmarshaler interface.
func (e *Entry) UnmarshalJSON(data []byte) error {
	var unmarshal []string

	err := json.Unmarshal(data, &unmarshal)
	if err != nil {
		return err
	}

	t, err := strconv.ParseInt(unmarshal[0], 10, 64)
	if err != nil {
		return err
	}

	e.Timestamp = time.Unix(0, t)
	e.Line = unmarshal[1]

	return nil
}

// Vector is a slice of Samples
type Vector []model.Sample

// Matrix is a slice of SampleStreams
type Matrix []model.SampleStream

// NewStreams constructs a Streams from a logql.Streams
func NewStreams(s logql.Streams) (Streams, error) {
	var err error
	ret := make([]Stream, len(s))

	for i, stream := range s {
		ret[i], err = NewStream(stream)

		if err != nil {
			return nil, err
		}
	}

	return ret, nil
}

// NewStream constructs a Stream from a logproto.Stream
func NewStream(s *logproto.Stream) (Stream, error) {
	labels, err := NewLabelSet(s.Labels)
	if err != nil {
		return Stream{}, err
	}

	ret := Stream{
		Labels:  labels,
		Entries: make([]Entry, len(s.Entries)),
	}

	for i, e := range s.Entries {
		ret.Entries[i] = NewEntry(e)
	}

	return ret, nil
}

// NewEntry constructs an Entry from a logproto.Entry
func NewEntry(e logproto.Entry) Entry {
	return Entry{
		Timestamp: e.Timestamp,
		Line:      e.Line,
	}
}

// NewVector constructs a Vector from a promql.Vector
func NewVector(v promql.Vector) Vector {
	ret := make([]model.Sample, len(v))

	for i, s := range v {
		ret[i] = NewSample(s)
	}

	return ret
}

// NewSample constructs a model.Sample from a promql.Sample
func NewSample(s promql.Sample) model.Sample {

	ret := model.Sample{
		Value:     model.SampleValue(s.V),
		Timestamp: model.Time(s.T),
		Metric:    NewMetric(s.Metric),
	}

	return ret
}

// NewMatrix constructs a Matrix from a promql.Matrix
func NewMatrix(m promql.Matrix) Matrix {
	ret := make([]model.SampleStream, len(m))

	for i, s := range m {
		ret[i] = NewSampleStream(s)
	}

	return ret
}

// NewSampleStream constructs a model.SampleStream from a promql.Series
func NewSampleStream(s promql.Series) model.SampleStream {
	ret := model.SampleStream{
		Metric: NewMetric(s.Metric),
		Values: make([]model.SamplePair, len(s.Points)),
	}

	for i, p := range s.Points {
		ret.Values[i].Timestamp = model.Time(p.T)
		ret.Values[i].Value = model.SampleValue(p.V)
	}

	return ret
}

// NewMetric constructs a labels.Labels from a model.Metric
func NewMetric(l labels.Labels) model.Metric {
	ret := make(map[model.LabelName]model.LabelValue)

	for _, label := range l {
		ret[model.LabelName(label.Name)] = model.LabelValue(label.Value)
	}

	return ret
}
