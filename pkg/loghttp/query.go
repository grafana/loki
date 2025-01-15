package loghttp

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unsafe"

	"github.com/c2h5oh/datasize"
	"github.com/gorilla/mux"
	"github.com/grafana/jsonparser"
	json "github.com/json-iterator/go"
	"github.com/prometheus/common/model"

	"github.com/grafana/dskit/httpgrpc"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/logqlmodel"
	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
	"github.com/grafana/loki/v3/pkg/storage/stores/index/seriesvolume"
	"github.com/grafana/loki/v3/pkg/util"
)

var (
	errEndBeforeStart     = errors.New("end timestamp must not be before or equal to start time")
	errZeroOrNegativeStep = errors.New("zero or negative query resolution step widths are not accepted. Try a positive integer")
	errNegativeStep       = errors.New("negative query resolution step widths are not accepted. Try a positive integer")
	errStepTooSmall       = errors.New("exceeded maximum resolution of 11,000 points per time series. Try increasing the value of the step parameter")
	errNegativeInterval   = errors.New("interval must be >= 0")
)

// QueryStatus holds the status of a query
type QueryStatus string

// QueryStatus values
const (
	QueryStatusSuccess = "success"
	QueryStatusFail    = "fail"
	// How much stack space to allocate for unescaping JSON strings; if a string longer
	// than this needs to be escaped, it will result in a heap allocation
	unescapeStackBufSize = 64
)

// QueryResponse represents the http json response to a Loki range and instant query
type QueryResponse struct {
	Status   string            `json:"status"`
	Warnings []string          `json:"warnings,omitempty"`
	Data     QueryResponseData `json:"data"`
}

func (q *QueryResponse) UnmarshalJSON(data []byte) error {
	return jsonparser.ObjectEach(data, func(key, value []byte, dataType jsonparser.ValueType, offset int) error {
		switch string(key) {
		case "status":
			q.Status = string(value)
		case "warnings":
			var warnings []string
			if _, err := jsonparser.ArrayEach(value, func(value []byte, dataType jsonparser.ValueType, offset int, err error) {
				if dataType == jsonparser.String {
					warnings = append(warnings, unescapeJSONString(value))
				}
			}); err != nil {
				return err
			}

			q.Warnings = warnings
		case "data":
			var responseData QueryResponseData
			if err := responseData.UnmarshalJSON(value); err != nil {
				return err
			}
			q.Data = responseData
		}
		return nil
	})
}

func unescapeJSONString(b []byte) string {
	var stackbuf [unescapeStackBufSize]byte // stack-allocated array for allocation-free unescaping of small strings
	bU, err := jsonparser.Unescape(b, stackbuf[:])
	if err != nil {
		return ""
	}

	return string(bU)
}

// PushRequest models a log stream push but is unmarshalled to proto push format.
type PushRequest struct {
	Streams []LogProtoStream `json:"streams"`
}

// LogProtoStream helps with unmarshalling of each log stream for push request.
// This might look un-necessary but without it the CPU usage in benchmarks was increasing by ~25% :shrug:
type LogProtoStream logproto.Stream

func (s *LogProtoStream) UnmarshalJSON(data []byte) error {
	err := jsonparser.ObjectEach(data, func(key, val []byte, ty jsonparser.ValueType, _ int) error {
		switch string(key) {
		case "stream":
			var labels LabelSet
			if err := labels.UnmarshalJSON(val); err != nil {
				return err
			}
			s.Labels = labels.String()
		case "values":
			if ty == jsonparser.Null {
				return nil
			}
			entries, err := unmarshalHTTPToLogProtoEntries(val)
			if err != nil {
				return err
			}
			s.Entries = entries
		}
		return nil
	})
	return err
}

func unmarshalHTTPToLogProtoEntries(data []byte) ([]logproto.Entry, error) {
	var (
		entries    []logproto.Entry
		parseError error
	)
	if _, err := jsonparser.ArrayEach(data, func(value []byte, ty jsonparser.ValueType, _ int, err error) {
		if err != nil || parseError != nil {
			return
		}
		if ty == jsonparser.Null {
			return
		}
		e, err := unmarshalHTTPToLogProtoEntry(value)
		if err != nil {
			parseError = err
			return
		}
		entries = append(entries, e)
	}); err != nil {
		parseError = err
	}

	if parseError != nil {
		return nil, parseError
	}

	return entries, nil
}

func unmarshalHTTPToLogProtoEntry(data []byte) (logproto.Entry, error) {
	var (
		i          int
		parseError error
		e          logproto.Entry
	)
	_, err := jsonparser.ArrayEach(data, func(value []byte, t jsonparser.ValueType, _ int, _ error) {
		// assert that both items in array are of type string
		if (i == 0 || i == 1) && t != jsonparser.String {
			parseError = jsonparser.MalformedStringError
			return
		} else if i == 2 && t != jsonparser.Object {
			parseError = jsonparser.MalformedObjectError
			return
		}
		switch i {
		case 0: // timestamp
			ts, err := jsonparser.ParseInt(value)
			if err != nil {
				parseError = err
				return
			}
			e.Timestamp = time.Unix(0, ts)
		case 1: // value
			v, err := jsonparser.ParseString(value)
			if err != nil {
				parseError = err
				return
			}
			e.Line = v
		case 2: // structuredMetadata
			var structuredMetadata []logproto.LabelAdapter
			err := jsonparser.ObjectEach(value, func(key, val []byte, dataType jsonparser.ValueType, _ int) error {
				if dataType != jsonparser.String {
					return jsonparser.MalformedStringError
				}
				structuredMetadata = append(structuredMetadata, logproto.LabelAdapter{
					Name:  string(key),
					Value: string(val),
				})
				return nil
			})
			if err != nil {
				parseError = err
				return
			}
			e.StructuredMetadata = structuredMetadata
		}
		i++
	})
	if parseError != nil {
		return e, parseError
	}
	return e, err
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

func (ss *Streams) UnmarshalJSON(data []byte) error {
	var parseError error
	_, err := jsonparser.ArrayEach(data, func(value []byte, dataType jsonparser.ValueType, offset int, err error) {
		var stream Stream
		if err := stream.UnmarshalJSON(value); err != nil {
			parseError = err
			return
		}
		*ss = append(*ss, stream)
	})
	if parseError != nil {
		return parseError
	}
	return err
}

func (s Streams) ToProto() []logproto.Stream {
	if len(s) == 0 {
		return nil
	}
	result := make([]logproto.Stream, 0, len(s))
	for _, s := range s {
		entries := *(*[]logproto.Entry)(unsafe.Pointer(&s.Entries)) // #nosec G103 -- we know the string is not mutated
		result = append(result, logproto.Stream{
			Labels:  s.Labels.String(),
			Entries: entries,
		})
	}
	return result
}

// Stream represents a log stream.  It includes a set of log entries and their labels.
type Stream struct {
	Labels  LabelSet `json:"stream"`
	Entries []Entry  `json:"values"`
}

func (s *Stream) UnmarshalJSON(data []byte) error {
	if s.Labels == nil {
		s.Labels = LabelSet{}
	}
	if len(s.Entries) > 0 {
		s.Entries = s.Entries[:0]
	}
	return jsonparser.ObjectEach(data, func(key, value []byte, ty jsonparser.ValueType, _ int) error {
		switch string(key) {
		case "stream":
			if err := s.Labels.UnmarshalJSON(value); err != nil {
				return err
			}
		case "values":
			if ty == jsonparser.Null {
				return nil
			}
			var parseError error
			_, err := jsonparser.ArrayEach(value, func(value []byte, ty jsonparser.ValueType, _ int, _ error) {
				if ty == jsonparser.Null {
					return
				}
				var entry Entry
				if err := entry.UnmarshalJSON(value); err != nil {
					parseError = err
					return
				}
				s.Entries = append(s.Entries, entry)
			})
			if parseError != nil {
				return parseError
			}
			return err
		}
		return nil
	})
}

// UnmarshalJSON implements the json.Unmarshaler interface.
func (q *QueryResponseData) UnmarshalJSON(data []byte) error {
	resultType, err := jsonparser.GetString(data, "resultType")
	if err != nil {
		return err
	}
	q.ResultType = ResultType(resultType)

	return jsonparser.ObjectEach(data, func(key, value []byte, dataType jsonparser.ValueType, _ int) error {
		switch string(key) {
		case "result":
			switch q.ResultType {
			case ResultTypeStream:
				ss := Streams{}
				if err := ss.UnmarshalJSON(value); err != nil {
					return err
				}
				q.Result = ss
			case ResultTypeMatrix:
				var m Matrix
				if err = json.Unmarshal(value, &m); err != nil {
					return err
				}
				q.Result = m
			case ResultTypeVector:
				var v Vector
				if err = json.Unmarshal(value, &v); err != nil {
					return err
				}
				q.Result = v
			case ResultTypeScalar:
				var v Scalar
				if err = json.Unmarshal(value, &v); err != nil {
					return err
				}
				q.Result = v
			default:
				return fmt.Errorf("unknown type: %s", q.ResultType)
			}
		case "stats":
			if err := json.Unmarshal(value, &q.Statistics); err != nil {
				return err
			}
		}
		return nil
	})
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
	Shards    []string
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
	request.Shards = shards(r)

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

func NewRangeQueryWithDefaults() *RangeQuery {
	start, end, _ := determineBounds(time.Now(), "", "", "")
	result := &RangeQuery{
		Start:     start,
		End:       end,
		Limit:     defaultQueryLimit,
		Direction: defaultDirection,
		Interval:  0,
	}
	result.UpdateStep()
	return result
}

// UpdateStep will adjust the step given new start and end.
func (q *RangeQuery) UpdateStep() {
	q.Step = time.Duration(defaultQueryRangeStep(q.Start, q.End)) * time.Second
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
		return nil, errZeroOrNegativeStep
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

	if GetVersion(r.URL.Path) == VersionLegacy {
		result.Query, err = parseRegexQuery(r)
		if err != nil {
			return nil, httpgrpc.Errorf(http.StatusBadRequest, "%s", err.Error())
		}

		expr, err := syntax.ParseExpr(result.Query)
		if err != nil {
			return nil, err
		}

		// short circuit metric queries
		if _, ok := expr.(syntax.SampleExpr); ok {
			return nil, httpgrpc.Errorf(http.StatusBadRequest, "legacy endpoints only support %s result type", logqlmodel.ValueTypeStreams)
		}
	}

	return &result, nil
}

func ParseIndexStatsQuery(r *http.Request) (*RangeQuery, error) {
	// TODO(owen-d): use a specific type/validation instead
	// of using range query parameters (superset)
	return ParseRangeQuery(r)
}

func ParseIndexShardsQuery(r *http.Request) (*RangeQuery, datasize.ByteSize, error) {
	// TODO(owen-d): use a specific type/validation instead
	// of using range query parameters (superset)
	parsed, err := ParseRangeQuery(r)
	if err != nil {
		return nil, 0, err
	}
	targetBytes, err := parseBytes(r, "targetBytesPerShard", true)
	if targetBytes <= 0 {
		return nil, 0, errors.New("targetBytesPerShard must be a positive value")
	}
	return parsed, targetBytes, err
}

func NewVolumeRangeQueryWithDefaults(matchers string) *logproto.VolumeRequest {
	start, end, _ := determineBounds(time.Now(), "", "", "")
	step := (time.Duration(defaultQueryRangeStep(start, end)) * time.Second).Milliseconds()
	from, through := util.RoundToMilliseconds(start, end)
	return &logproto.VolumeRequest{
		From:         from,
		Through:      through,
		Matchers:     matchers,
		Limit:        seriesvolume.DefaultLimit,
		Step:         step,
		TargetLabels: nil,
		AggregateBy:  seriesvolume.DefaultAggregateBy,
	}
}

func NewVolumeInstantQueryWithDefaults(matchers string) *logproto.VolumeRequest {
	r := NewVolumeRangeQueryWithDefaults(matchers)
	r.Step = 0
	return r
}

type VolumeInstantQuery struct {
	Start        time.Time
	End          time.Time
	Query        string
	Limit        uint32
	TargetLabels []string
	AggregateBy  string
}

func ParseVolumeInstantQuery(r *http.Request) (*VolumeInstantQuery, error) {
	err := volumeLimit(r)
	if err != nil {
		return nil, err
	}

	result, err := ParseInstantQuery(r)
	if err != nil {
		return nil, err
	}

	aggregateBy, err := volumeAggregateBy(r)
	if err != nil {
		return nil, err
	}

	svInstantQuery := VolumeInstantQuery{
		Query:        result.Query,
		Limit:        result.Limit,
		TargetLabels: targetLabels(r),
		AggregateBy:  aggregateBy,
	}

	svInstantQuery.Start, svInstantQuery.End, err = bounds(r)
	if err != nil {
		return nil, err
	}

	if svInstantQuery.End.Before(svInstantQuery.Start) {
		return nil, errEndBeforeStart
	}

	return &svInstantQuery, nil
}

type VolumeRangeQuery struct {
	Start        time.Time
	End          time.Time
	Step         time.Duration
	Query        string
	Limit        uint32
	TargetLabels []string
	AggregateBy  string
}

func ParseVolumeRangeQuery(r *http.Request) (*VolumeRangeQuery, error) {
	err := volumeLimit(r)
	if err != nil {
		return nil, err
	}

	result, err := ParseRangeQuery(r)
	if err != nil {
		return nil, err
	}

	aggregateBy, err := volumeAggregateBy(r)
	if err != nil {
		return nil, err
	}

	return &VolumeRangeQuery{
		Start:        result.Start,
		End:          result.End,
		Step:         result.Step,
		Query:        result.Query,
		Limit:        result.Limit,
		TargetLabels: targetLabels(r),
		AggregateBy:  aggregateBy,
	}, nil
}

func ParseDetectedFieldsQuery(r *http.Request) (*logproto.DetectedFieldsRequest, error) {
	var err error
	result := &logproto.DetectedFieldsRequest{}

	result.Query = query(r)
	result.Values, result.Name = values(r)
	result.Start, result.End, err = bounds(r)
	if err != nil {
		return nil, err
	}

	if result.End.Before(result.Start) {
		return nil, errEndBeforeStart
	}

	result.LineLimit, err = lineLimit(r)
	if err != nil {
		return nil, err
	}

	result.Limit, err = detectedFieldsLimit(r)
	if err != nil {
		return nil, err
	}

	step, err := step(r, result.Start, result.End)
	result.Step = step.Milliseconds()
	if err != nil {
		return nil, err
	}

	if result.Step <= 0 {
		return nil, errZeroOrNegativeStep
	}

	// For safety, limit the number of returned points per timeseries.
	// This is sufficient for 60s resolution for a week or 1h resolution for a year.
	if (result.End.Sub(result.Start) / step) > 11000 {
		return nil, errStepTooSmall
	}

	return result, nil
}

func values(r *http.Request) (bool, string) {
	name, ok := mux.Vars(r)["name"]
	return ok, name
}

func targetLabels(r *http.Request) []string {
	lbls := strings.Split(r.Form.Get("targetLabels"), ",")
	if (len(lbls) == 1 && lbls[0] == "") || len(lbls) == 0 {
		return nil
	}

	return lbls
}

func volumeLimit(r *http.Request) error {
	l, err := parseInt(r.Form.Get("limit"), seriesvolume.DefaultLimit)
	if err != nil {
		return err
	}

	if l == 0 {
		r.Form.Set("limit", fmt.Sprint(seriesvolume.DefaultLimit))
		return nil
	}

	if l <= 0 {
		return errors.New("limit must be a positive value")
	}

	return nil
}

func volumeAggregateBy(r *http.Request) (string, error) {
	l := r.Form.Get("aggregateBy")
	if l == "" {
		return seriesvolume.DefaultAggregateBy, nil
	}

	if seriesvolume.ValidateAggregateBy(l) {
		return l, nil
	}

	return "", errors.New("invalid aggregation option")
}
