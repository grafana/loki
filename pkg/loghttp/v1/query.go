package v1

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/prometheus/common/model"
)

//QueryStatus
type QueryStatus string

//QueryStatus values
const (
	QueryStatusSuccess = "success"
)

//QueryResponse represents the http json response to a label query
type QueryResponse struct {
	Status string            `json:"status"`
	Data   QueryResponseData `json:"data"`
}

//ResultType
type ResultType string

//ResultType values
const (
	ResultTypeStream = "streams"
	ResultTypeVector = "vector"
	ResultTypeMatrix = "matrix"
)

type ResultValue interface {
	Type() ResultType
}

//QueryResponseData represents the http json response to a label query
type QueryResponseData struct {
	ResultType ResultType  `json:"resultType"`
	Result     ResultValue `json:"result"`
}

func (Streams) Type() ResultType { return ResultTypeStream }
func (Vector) Type() ResultType  { return ResultTypeVector }
func (Matrix) Type() ResultType  { return ResultTypeMatrix }

type Streams []Stream

//Stream represents a log stream.  It includes a set of log entries and their labels.
type Stream struct {
	Labels  LabelSet `json:"labels"`
	Entries []Entry  `json:"entries"`
}

//Entry represents a log entry.  It includes a log message and the time it occurred at.
type Entry struct {
	Timestamp time.Time
	Line      string
}

//MarshalJSON converts an Entry object to be prom compatible for http queries
func (e *Entry) MarshalJSON() ([]byte, error) {
	l, err := json.Marshal(e.Line)
	if err != nil {
		return nil, err
	}
	return []byte(fmt.Sprintf("[\"%d\",%s]", e.Timestamp.UnixNano(), l)), nil
}

//UnmarshalJSON
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

//Vector
type Vector []*model.Sample

//Matrix
type Matrix []*model.SampleStream

func NewStream(s *logproto.Stream) (*Stream, error) {
	labels, err := NewLabelSet(s.Labels)
	if err != nil {
		return nil, err
	}

	new := &Stream{
		Labels:  labels,
		Entries: make([]Entry, len(s.Entries)),
	}

	for i, e := range s.Entries {
		new.Entries[i] = NewEntry(e)
	}

	return new, nil
}

func NewEntry(e logproto.Entry) Entry {
	return Entry{
		Timestamp: e.Timestamp,
		Line:      e.Line,
	}
}
