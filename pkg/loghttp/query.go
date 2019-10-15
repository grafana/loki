package loghttp

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/prometheus/common/model"
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

// PushRequest models a log stream push
type PushRequest struct {
	Streams []*Stream `json:"streams"`
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

// UnmarshalJSON implements the json.Unmarshaler interface.
func (q *QueryResponseData) UnmarshalJSON(data []byte) error {
	unmarshal := struct {
		Type   ResultType      `json:"resultType"`
		Result json.RawMessage `json:"result"`
	}{}

	err := json.Unmarshal(data, &unmarshal)
	if err != nil {
		return err
	}

	var value ResultValue

	// unmarshal results
	switch unmarshal.Type {
	case ResultTypeStream:
		var s Streams
		err = json.Unmarshal(unmarshal.Result, &s)
		value = s
	case ResultTypeMatrix:
		var m Matrix
		err = json.Unmarshal(unmarshal.Result, &m)
		value = m
	case ResultTypeVector:
		var v Vector
		err = json.Unmarshal(unmarshal.Result, &v)
		value = v
	default:
		return fmt.Errorf("unknown type: %s", unmarshal.Type)
	}

	if err != nil {
		return err
	}

	q.ResultType = unmarshal.Type
	q.Result = value

	return nil
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
