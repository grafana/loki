package loghttp

import (
	"errors"
	"fmt"
	"net/http"
	"time"
	"unsafe"

	"github.com/buger/jsonparser"
	json "github.com/json-iterator/go"
	"github.com/prometheus/common/model"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logqlmodel/stats"
)

var (
	errEndBeforeStart   = errors.New("end timestamp must not be before or equal to start time")
	errNegativeStep     = errors.New("zero or negative query resolution step widths are not accepted. Try a positive integer")
	errStepTooSmall     = errors.New("exceeded maximum resolution of 11,000 points per timeseries. Try decreasing the query resolution (?step=XX)")
	errNegativeInterval = errors.New("interval must be >= 0")
)

// QueryStatus holds the status of a query
type QueryStatus string

// QueryStatus values
const (
	QueryStatusSuccess = "success"
	QueryStatusFail    = "fail"
)

// QueryResponse represents the http json response to a Loki range and instant query
type QueryResponse struct {
	Status string            `json:"status"`
	Data   QueryResponseData `json:"data"`
}

func (q *QueryResponse) UnmarshalJSON(data []byte) error {
	var err error
	_ = jsonparser.EachKey(data, func(i int, data []byte, vt jsonparser.ValueType, e error) {
		if e == jsonparser.KeyPathNotFoundError {
			return
		}
		if e != nil {
			err = e
		}
		switch i {
		case 0:
			var responseData QueryResponseData
			if jsonErr := responseData.UnmarshalJSON(data); jsonErr != nil {
				err = jsonErr
				return
			}
			q.Data = responseData
		case 1:
			q.Status = string(data)
			return
		}
	}, [][]string{{"data"}, {"status"}}...)
	return err
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
	ResultTypeScalar = "scalar"
	ResultTypeVector = "vector"
	ResultTypeMatrix = "matrix"
)

// ResultValue interface mimics the promql.Value interface
type ResultValue interface {
	Type() ResultType
}

// QueryResponseData represents the http json response to a label query
type QueryResponseData struct {
	ResultType ResultType   `json:"resultType"`
	Result     ResultValue  `json:"result"`
	Statistics stats.Result `json:"stats"`
}

// Type implements the promql.Value interface
func (Streams) Type() ResultType { return ResultTypeStream }

// Type implements the promql.Value interface
func (Scalar) Type() ResultType { return ResultTypeScalar }

// Type implements the promql.Value interface
func (Vector) Type() ResultType { return ResultTypeVector }

// Type implements the promql.Value interface
func (Matrix) Type() ResultType { return ResultTypeMatrix }

// Streams is a slice of Stream
type Streams []Stream

func (s Streams) ToProto() []logproto.Stream {
	if len(s) == 0 {
		return nil
	}
	result := make([]logproto.Stream, 0, len(s))
	for _, s := range s {
		entries := *(*[]logproto.Entry)(unsafe.Pointer(&s.Entries))
		result = append(result, logproto.Stream{Labels: s.Labels.String(), Entries: entries})
	}
	return result
}

// Stream represents a log stream.  It includes a set of log entries and their labels.
type Stream struct {
	Labels  LabelSet `json:"stream"`
	Entries []Entry  `json:"values"`
}

// UnmarshalJSON implements the json.Unmarshaler interface.
func (q *QueryResponseData) UnmarshalJSON(data []byte) error {
	resultType, err := jsonparser.GetString(data, "resultType")
	if err != nil {
		return err
	}
	q.ResultType = ResultType(resultType)

	err = jsonparser.ObjectEach(data, func(key, value []byte, dataType jsonparser.ValueType, offset int) error {
		switch string(key) {
		case "result": // result
			switch q.ResultType {
			case ResultTypeStream:
				var ss Streams
				if _, innerErr := jsonparser.ArrayEach(value, func(value []byte, dataType jsonparser.ValueType, offset int, jsonErr error) {
					if jsonErr != nil {
						err = jsonErr
						return
					}
					stream := Stream{
						Labels:  LabelSet{},
						Entries: []Entry{},
					}
					if jsonErr := jsonparser.ObjectEach(value, func(key, value []byte, dataType jsonparser.ValueType, offset int) error {
						switch string(key) {
						case "stream":
							jsonErr := jsonparser.ObjectEach(value, func(key, val []byte, dataType jsonparser.ValueType, offset int) error {
								v, err := jsonparser.ParseString(val)
								if err != nil {
									return err
								}
								k, err := jsonparser.ParseString(key)
								if err != nil {
									return err
								}
								stream.Labels[k] = v
								return nil
							})
							if jsonErr != nil {
								return jsonErr
							}
						case "values": // values
							_, jsonErr := jsonparser.ArrayEach(value, func(value []byte, dataType jsonparser.ValueType, offset int, jsonErr error) {
								if jsonErr != nil {
									err = jsonErr
									return
								}
								var entry Entry
								var i int
								_, jsonErr = jsonparser.ArrayEach(value, func(value []byte, dataType jsonparser.ValueType, offset int, jsonErr error) {
									if jsonErr != nil {
										err = jsonErr
										return
									}
									switch i {
									case 0: // timestamp
										ts, perr := jsonparser.ParseInt(value)
										if perr != nil {
											err = jsonErr
											return
										}
										entry.Timestamp = time.Unix(0, ts)
									case 1: // value
										v, pErr := jsonparser.ParseString(value)
										if pErr != nil {
											err = jsonErr
											return
										}
										entry.Line = v
									}
									i++
								})
								if jsonErr != nil {
									err = jsonErr
									return
								}
								stream.Entries = append(stream.Entries, entry)
							})
							if jsonErr != nil {
								return jsonErr
							}
						}
						return nil
					}); jsonErr != nil {
						err = jsonErr
						return
					}
					ss = append(ss, stream)
				}); innerErr != nil {
					return innerErr
				}
				q.Result = ss
			case ResultTypeMatrix:
				var m Matrix
				err = json.Unmarshal(value, &m)
				q.Result = m
			case ResultTypeVector:
				var v Vector
				err = json.Unmarshal(value, &v)
				q.Result = v
			case ResultTypeScalar:
				var v Scalar
				err = json.Unmarshal(value, &v)
				q.Result = v
			default:
				return fmt.Errorf("unknown type: %s", q.ResultType)
			}
		case "stats": // stats
			if innerErr := json.Unmarshal(value, &q.Statistics); innerErr != nil {
				return innerErr
			}

		}
		return nil
	})
	if err != nil {
		return err
	}
	return nil
}

// Scalar is a single timestamp/float with no labels
type Scalar model.Scalar

func (s Scalar) MarshalJSON() ([]byte, error) {
	return model.Scalar(s).MarshalJSON()
}

func (s *Scalar) UnmarshalJSON(b []byte) error {
	var v model.Scalar
	if err := v.UnmarshalJSON(b); err != nil {
		return err
	}
	*s = Scalar(v)
	return nil
}

// Vector is a slice of Samples
type Vector []model.Sample

// Matrix is a slice of SampleStreams
type Matrix []model.SampleStream

// InstantQuery defines a log instant query.
type InstantQuery struct {
	Query     string
	Ts        time.Time
	Limit     uint32
	Direction logproto.Direction
}

// ParseInstantQuery parses an InstantQuery request from an http request.
func ParseInstantQuery(r *http.Request) (*InstantQuery, error) {
	var err error
	request := &InstantQuery{
		Query: query(r),
	}
	request.Limit, err = limit(r)
	if err != nil {
		return nil, err
	}

	request.Ts, err = ts(r)
	if err != nil {
		return nil, err
	}

	request.Direction, err = direction(r)
	if err != nil {
		return nil, err
	}

	return request, nil
}

// RangeQuery defines a log range query.
type RangeQuery struct {
	Start     time.Time
	End       time.Time
	Step      time.Duration
	Interval  time.Duration
	Query     string
	Direction logproto.Direction
	Limit     uint32
	Shards    []string
}

// ParseRangeQuery parses a RangeQuery request from an http request.
func ParseRangeQuery(r *http.Request) (*RangeQuery, error) {
	var result RangeQuery
	var err error

	result.Query = query(r)
	result.Start, result.End, err = bounds(r)
	if err != nil {
		return nil, err
	}

	if result.End.Before(result.Start) {
		return nil, errEndBeforeStart
	}

	result.Limit, err = limit(r)
	if err != nil {
		return nil, err
	}

	result.Direction, err = direction(r)
	if err != nil {
		return nil, err
	}

	result.Step, err = step(r, result.Start, result.End)
	if err != nil {
		return nil, err
	}

	if result.Step <= 0 {
		return nil, errNegativeStep
	}

	result.Shards = shards(r)

	// For safety, limit the number of returned points per timeseries.
	// This is sufficient for 60s resolution for a week or 1h resolution for a year.
	if (result.End.Sub(result.Start) / result.Step) > 11000 {
		return nil, errStepTooSmall
	}

	result.Interval, err = interval(r)
	if err != nil {
		return nil, err
	}

	if result.Interval < 0 {
		return nil, errNegativeInterval
	}

	return &result, nil
}
