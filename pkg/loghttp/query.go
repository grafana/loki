package loghttp

import (
	stdjson "encoding/json"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/c2h5oh/datasize"
	"github.com/gorilla/mux"
	"github.com/prometheus/common/model"

	"github.com/grafana/dskit/httpgrpc"

	"github.com/grafana/loki/v3/pkg/logproto"
	logqlmodel "github.com/grafana/loki/v3/pkg/logql/model"
	"github.com/grafana/loki/v3/pkg/logql/model/stats"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/util"
	"github.com/grafana/loki/v3/pkg/util/json"
)

var (
	errEndBeforeStart     = errors.New("end timestamp must not be before or equal to start time")
	errZeroOrNegativeStep = errors.New("zero or negative query resolution step widths are not accepted. Try a positive integer")
	errStepTooSmall       = errors.New("exceeded maximum resolution of 11,000 points per time series. Try increasing the value of the step parameter")
	errNegativeInterval   = errors.New("interval must be >= 0")
)

// QueryStatus holds the status of a query
type QueryStatus string

// QueryStatus values
const (
	QueryStatusSuccess = "success"
	QueryStatusFail    = "fail"
)

// Volume HTTP defaults and validation duplicated here so pkg/loghttp stays free of
// pkg/storage/stores/index/seriesvolume (AGPL). Keep in sync with that package's
// DefaultLimit, DefaultAggregateBy, and ValidateAggregateBy.
const (
	defaultVolumeLimit       = 100
	defaultVolumeAggregateBy = "series"

	volumeAggregateByLabels = "labels"
	volumeAggregateBySeries = "series"
)

// QueryResponse represents the http json response to a Loki range and instant query
type QueryResponse struct {
	Status   string            `json:"status"`
	Warnings []string          `json:"warnings,omitempty"`
	Data     QueryResponseData `json:"data"`
}

func (q *QueryResponse) UnmarshalJSON(data []byte) error {
	d := json.GetDecoder(data)
	defer d.Release()
	return q.decode(d)
}

func (q *QueryResponse) decode(d *json.Decoder) error {
	return d.EachField(func(key []byte) error {
		switch string(key) {
		case "status":
			status, err := d.ReadScalar()
			if err != nil {
				return err
			}
			q.Status = status
		case "warnings":
			if d.PeekKind() == jsontext.KindNull {
				q.Warnings = nil
				return d.SkipValue()
			}
			var warnings []string
			if err := d.EachElement(func() error {
				if d.PeekKind() != jsontext.KindString {
					return d.SkipValue()
				}
				w, err := d.ReadScalar()
				warnings = append(warnings, w)
				return err
			}); err != nil {
				return err
			}
			q.Warnings = warnings
		case "data":
			var responseData QueryResponseData
			if err := responseData.decode(d); err != nil {
				return err
			}
			q.Data = responseData
		default:
			return d.SkipValue()
		}
		return nil
	})
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
	d := json.GetDecoder(data)
	defer d.Release()
	return ss.decode(d)
}

func (ss *Streams) decode(d *json.Decoder) error {
	return d.EachElement(func() error {
		var stream Stream
		if err := stream.decode(d); err != nil {
			return err
		}
		*ss = append(*ss, stream)
		return nil
	})
}

func (s Streams) ToProto() []logproto.Stream {
	if len(s) == 0 {
		return nil
	}
	result := make([]logproto.Stream, 0, len(s))
	for _, s := range s {
		result = append(result, logproto.Stream{
			Labels:  s.Labels.String(),
			Entries: protoEntries(s.Entries),
		})
	}
	return result
}

func protoEntries(in []Entry) []logproto.Entry {
	out := make([]logproto.Entry, 0, len(in))
	for _, ent := range in {
		out = append(out, logproto.Entry{
			Timestamp:          ent.Timestamp,
			Line:               ent.Line,
			StructuredMetadata: logproto.FromLabelsToLabelAdapters(ent.StructuredMetadata),
			Parsed:             logproto.FromLabelsToLabelAdapters(ent.Parsed),
		})
	}
	return out
}

// Stream represents a log stream.  It includes a set of log entries and their labels.
type Stream struct {
	Labels  LabelSet `json:"stream"`
	Entries []Entry  `json:"values"`
}

func (s *Stream) UnmarshalJSON(data []byte) error {
	d := json.GetDecoder(data)
	defer d.Release()
	return s.decode(d)
}

func (s *Stream) decode(d *json.Decoder) error {
	if s.Labels == nil {
		s.Labels = LabelSet{}
	}
	if len(s.Entries) > 0 {
		s.Entries = s.Entries[:0]
	}
	return d.EachField(func(key []byte) error {
		switch string(key) {
		case "stream":
			v, err := d.ReadValue()
			if err != nil {
				return err
			}
			return s.Labels.UnmarshalJSON(v)
		case "values":
			if d.PeekKind() == jsontext.KindNull {
				return d.SkipValue()
			}
			return d.EachElement(func() error {
				if d.PeekKind() == jsontext.KindNull {
					return d.SkipValue()
				}
				var entry Entry
				if err := entry.decode(d); err != nil {
					return err
				}
				s.Entries = append(s.Entries, entry)
				return nil
			})
		default:
			return d.SkipValue()
		}
	})
}

// UnmarshalJSON implements the json.Unmarshaler interface.
func (q *QueryResponseData) UnmarshalJSON(data []byte) error {
	d := json.GetDecoder(data)
	defer d.Release()
	return q.decode(d)
}

func (q *QueryResponseData) decode(d *json.Decoder) error {
	var (
		haveType  bool
		deferred  jsontext.Value // "result", if it came before "resultType"
		hasResult bool
	)
	err := d.EachField(func(key []byte) error {
		switch string(key) {
		case "resultType":
			resultType, err := d.ReadScalar()
			if err != nil {
				return err
			}
			q.ResultType = ResultType(resultType)
			haveType = true
		case "result":
			if haveType {
				return q.decodeResult(d)
			}
			v, err := d.ReadValue()
			if err != nil {
				return err
			}
			deferred, hasResult = v.Clone(), true
		case "stats":
			return unmarshalValue(d, &q.Statistics)
		default:
			return d.SkipValue()
		}
		return nil
	})
	if err != nil {
		return err
	}
	if !haveType {
		return errors.New("resultType not found")
	}
	if hasResult {
		rd := json.GetDecoder(deferred)
		defer rd.Release()
		return q.decodeResult(rd)
	}
	return nil
}

// unmarshalValue reads the next value and decodes it into v with encoding/json.
func unmarshalValue(d *json.Decoder, v any) error {
	raw, err := d.ReadValue()
	if err != nil {
		return err
	}
	return stdjson.Unmarshal(raw, v)
}

// decodeResult reads the "result" value according to q.ResultType.
func (q *QueryResponseData) decodeResult(d *json.Decoder) error {
	switch q.ResultType {
	case ResultTypeStream:
		ss := Streams{}
		if err := ss.decode(d); err != nil {
			return err
		}
		q.Result = ss
	case ResultTypeMatrix:
		var m Matrix
		if err := unmarshalValue(d, &m); err != nil {
			return err
		}
		q.Result = m
	case ResultTypeVector:
		var v Vector
		if err := unmarshalValue(d, &v); err != nil {
			return err
		}
		q.Result = v
	case ResultTypeScalar:
		var v Scalar
		if err := unmarshalValue(d, &v); err != nil {
			return err
		}
		q.Result = v
	default:
		return fmt.Errorf("unknown type: %s", q.ResultType)
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
		Limit:        defaultVolumeLimit,
		Step:         step,
		TargetLabels: nil,
		AggregateBy:  defaultVolumeAggregateBy,
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
	l, err := parseInt(r.Form.Get("limit"), defaultVolumeLimit)
	if err != nil {
		return err
	}

	if l == 0 {
		r.Form.Set("limit", fmt.Sprint(defaultVolumeLimit))
		return nil
	}

	if l <= 0 {
		return errors.New("limit must be a positive value")
	}

	return nil
}

func validateVolumeAggregateBy(aggregateBy string) bool {
	switch aggregateBy {
	case volumeAggregateByLabels, volumeAggregateBySeries:
		return true
	default:
		return false
	}
}

func volumeAggregateBy(r *http.Request) (string, error) {
	l := r.Form.Get("aggregateBy")
	if l == "" {
		return defaultVolumeAggregateBy, nil
	}

	if validateVolumeAggregateBy(l) {
		return l, nil
	}

	return "", errors.New("invalid aggregation option")
}
