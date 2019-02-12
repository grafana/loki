package client

import (
	"bytes"
	stdjson "encoding/json"
	"fmt"
	"sort"
	"strconv"
	"time"
	"unsafe"

	jsoniter "github.com/json-iterator/go"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
)

var json = jsoniter.ConfigCompatibleWithStandardLibrary

// FromWriteRequest converts a WriteRequest proto into an array of samples.
func FromWriteRequest(req *WriteRequest) []model.Sample {
	// Just guess that there is one sample per timeseries
	samples := make([]model.Sample, 0, len(req.Timeseries))
	for _, ts := range req.Timeseries {
		for _, s := range ts.Samples {
			samples = append(samples, model.Sample{
				Metric:    FromLabelPairs(ts.Labels),
				Value:     model.SampleValue(s.Value),
				Timestamp: model.Time(s.TimestampMs),
			})
		}
	}
	return samples
}

// ToWriteRequest converts an array of samples into a WriteRequest proto.
func ToWriteRequest(samples []model.Sample, source WriteRequest_SourceEnum) *WriteRequest {
	req := &WriteRequest{
		Timeseries: make([]PreallocTimeseries, 0, len(samples)),
		Source:     source,
	}

	for _, s := range samples {
		ts := PreallocTimeseries{
			TimeSeries: TimeSeries{
				Labels: ToLabelPairs(s.Metric),
				Samples: []Sample{
					{
						Value:       float64(s.Value),
						TimestampMs: int64(s.Timestamp),
					},
				},
			},
		}
		req.Timeseries = append(req.Timeseries, ts)
	}

	return req
}

// ToQueryRequest builds a QueryRequest proto.
func ToQueryRequest(from, to model.Time, matchers []*labels.Matcher) (*QueryRequest, error) {
	ms, err := toLabelMatchers(matchers)
	if err != nil {
		return nil, err
	}

	return &QueryRequest{
		StartTimestampMs: int64(from),
		EndTimestampMs:   int64(to),
		Matchers:         ms,
	}, nil
}

// FromQueryRequest unpacks a QueryRequest proto.
func FromQueryRequest(req *QueryRequest) (model.Time, model.Time, []*labels.Matcher, error) {
	matchers, err := fromLabelMatchers(req.Matchers)
	if err != nil {
		return 0, 0, nil, err
	}
	from := model.Time(req.StartTimestampMs)
	to := model.Time(req.EndTimestampMs)
	return from, to, matchers, nil
}

// ToQueryResponse builds a QueryResponse proto.
func ToQueryResponse(matrix model.Matrix) *QueryResponse {
	resp := &QueryResponse{}
	for _, ss := range matrix {
		ts := TimeSeries{
			Labels:  ToLabelPairs(ss.Metric),
			Samples: make([]Sample, 0, len(ss.Values)),
		}
		for _, s := range ss.Values {
			ts.Samples = append(ts.Samples, Sample{
				Value:       float64(s.Value),
				TimestampMs: int64(s.Timestamp),
			})
		}
		resp.Timeseries = append(resp.Timeseries, ts)
	}
	return resp
}

// FromQueryResponse unpacks a QueryResponse proto.
func FromQueryResponse(resp *QueryResponse) model.Matrix {
	m := make(model.Matrix, 0, len(resp.Timeseries))
	for _, ts := range resp.Timeseries {
		var ss model.SampleStream
		ss.Metric = FromLabelPairs(ts.Labels)
		ss.Values = make([]model.SamplePair, 0, len(ts.Samples))
		for _, s := range ts.Samples {
			ss.Values = append(ss.Values, model.SamplePair{
				Value:     model.SampleValue(s.Value),
				Timestamp: model.Time(s.TimestampMs),
			})
		}
		m = append(m, &ss)
	}

	return m
}

// ToMetricsForLabelMatchersRequest builds a MetricsForLabelMatchersRequest proto
func ToMetricsForLabelMatchersRequest(from, to model.Time, matchers []*labels.Matcher) (*MetricsForLabelMatchersRequest, error) {
	ms, err := toLabelMatchers(matchers)
	if err != nil {
		return nil, err
	}

	return &MetricsForLabelMatchersRequest{
		StartTimestampMs: int64(from),
		EndTimestampMs:   int64(to),
		MatchersSet:      []*LabelMatchers{{Matchers: ms}},
	}, nil
}

// FromMetricsForLabelMatchersRequest unpacks a MetricsForLabelMatchersRequest proto
func FromMetricsForLabelMatchersRequest(req *MetricsForLabelMatchersRequest) (model.Time, model.Time, [][]*labels.Matcher, error) {
	matchersSet := make([][]*labels.Matcher, 0, len(req.MatchersSet))
	for _, matchers := range req.MatchersSet {
		matchers, err := fromLabelMatchers(matchers.Matchers)
		if err != nil {
			return 0, 0, nil, err
		}
		matchersSet = append(matchersSet, matchers)
	}
	from := model.Time(req.StartTimestampMs)
	to := model.Time(req.EndTimestampMs)
	return from, to, matchersSet, nil
}

// FromMetricsForLabelMatchersResponse unpacks a MetricsForLabelMatchersResponse proto
func FromMetricsForLabelMatchersResponse(resp *MetricsForLabelMatchersResponse) []model.Metric {
	metrics := []model.Metric{}
	for _, m := range resp.Metric {
		metrics = append(metrics, FromLabelPairs(m.Labels))
	}
	return metrics
}

func toLabelMatchers(matchers []*labels.Matcher) ([]*LabelMatcher, error) {
	result := make([]*LabelMatcher, 0, len(matchers))
	for _, matcher := range matchers {
		var mType MatchType
		switch matcher.Type {
		case labels.MatchEqual:
			mType = EQUAL
		case labels.MatchNotEqual:
			mType = NOT_EQUAL
		case labels.MatchRegexp:
			mType = REGEX_MATCH
		case labels.MatchNotRegexp:
			mType = REGEX_NO_MATCH
		default:
			return nil, fmt.Errorf("invalid matcher type")
		}
		result = append(result, &LabelMatcher{
			Type:  mType,
			Name:  string(matcher.Name),
			Value: string(matcher.Value),
		})
	}
	return result, nil
}

func fromLabelMatchers(matchers []*LabelMatcher) ([]*labels.Matcher, error) {
	result := make([]*labels.Matcher, 0, len(matchers))
	for _, matcher := range matchers {
		var mtype labels.MatchType
		switch matcher.Type {
		case EQUAL:
			mtype = labels.MatchEqual
		case NOT_EQUAL:
			mtype = labels.MatchNotEqual
		case REGEX_MATCH:
			mtype = labels.MatchRegexp
		case REGEX_NO_MATCH:
			mtype = labels.MatchNotRegexp
		default:
			return nil, fmt.Errorf("invalid matcher type")
		}
		matcher, err := labels.NewMatcher(mtype, matcher.Name, matcher.Value)
		if err != nil {
			return nil, err
		}
		result = append(result, matcher)
	}
	return result, nil
}

// ToLabelPairs builds a []LabelPair from a model.Metric
func ToLabelPairs(metric model.Metric) []LabelPair {
	labelPairs := make([]LabelPair, 0, len(metric))
	for k, v := range metric {
		labelPairs = append(labelPairs, LabelPair{
			Name:  []byte(k),
			Value: []byte(v),
		})
	}
	sort.Sort(byLabel(labelPairs)) // The labels should be sorted upon initialisation.
	return labelPairs
}

type byLabel []LabelPair

func (s byLabel) Len() int           { return len(s) }
func (s byLabel) Less(i, j int) bool { return bytes.Compare(s[i].Name, s[j].Name) < 0 }
func (s byLabel) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }

// FromLabelPairs unpack a []LabelPair to a model.Metric
func FromLabelPairs(labelPairs []LabelPair) model.Metric {
	metric := make(model.Metric, len(labelPairs))
	for _, l := range labelPairs {
		metric[model.LabelName(l.Name)] = model.LabelValue(l.Value)
	}
	return metric
}

// FromLabelPairsToLabels unpack a []LabelPair to a labels.Labels
func FromLabelPairsToLabels(labelPairs []LabelPair) labels.Labels {
	ls := make(labels.Labels, 0, len(labelPairs))
	for _, l := range labelPairs {
		ls = append(ls, labels.Label{
			Name:  string(l.Name),
			Value: string(l.Value),
		})
	}
	return ls
}

// FromLabelsToLabelPairs converts labels.Labels to []LabelPair
func FromLabelsToLabelPairs(s labels.Labels) []LabelPair {
	labelPairs := make([]LabelPair, 0, len(s))
	for _, v := range s {
		labelPairs = append(labelPairs, LabelPair{
			Name:  []byte(v.Name),
			Value: []byte(v.Value),
		})
	}
	return labelPairs // note already sorted
}

// FastFingerprint runs the same algorithm as Prometheus labelSetToFastFingerprint()
func FastFingerprint(labelPairs []LabelPair) model.Fingerprint {
	if len(labelPairs) == 0 {
		return model.Metric(nil).FastFingerprint()
	}

	var result uint64
	for _, pair := range labelPairs {
		sum := hashNew()
		sum = hashAdd(sum, pair.Name)
		sum = hashAddByte(sum, model.SeparatorByte)
		sum = hashAdd(sum, pair.Value)
		result ^= sum
	}
	return model.Fingerprint(result)
}

// MarshalJSON implements json.Marshaler.
func (s Sample) MarshalJSON() ([]byte, error) {
	t, err := json.Marshal(model.Time(s.TimestampMs))
	if err != nil {
		return nil, err
	}
	v, err := json.Marshal(model.SampleValue(s.Value))
	if err != nil {
		return nil, err
	}
	return []byte(fmt.Sprintf("[%s,%s]", t, v)), nil
}

// UnmarshalJSON implements json.Unmarshaler.
func (s *Sample) UnmarshalJSON(b []byte) error {
	var t model.Time
	var v model.SampleValue
	vs := [...]stdjson.Unmarshaler{&t, &v}
	if err := json.Unmarshal(b, &vs); err != nil {
		return err
	}
	s.TimestampMs = int64(t)
	s.Value = float64(v)
	return nil
}

func init() {

	jsoniter.RegisterTypeEncoderFunc("client.Sample", func(ptr unsafe.Pointer, stream *jsoniter.Stream) {
		sample := (*Sample)(ptr)

		stream.WriteArrayStart()
		stream.WriteFloat64(float64(sample.TimestampMs) / float64(time.Second/time.Millisecond))
		stream.WriteMore()
		stream.WriteString(model.SampleValue(sample.Value).String())
		stream.WriteArrayEnd()
	}, func(unsafe.Pointer) bool {
		return false
	})

	jsoniter.RegisterTypeDecoderFunc("client.Sample", func(ptr unsafe.Pointer, iter *jsoniter.Iterator) {
		if !iter.ReadArray() {
			iter.ReportError("client.Sample", "expected [")
			return
		}

		t := model.Time(iter.ReadFloat64() * float64(time.Second/time.Millisecond))

		if !iter.ReadArray() {
			iter.ReportError("client.Sample", "expected ,")
			return
		}

		bs := iter.ReadStringAsSlice()
		ss := *(*string)(unsafe.Pointer(&bs))
		v, err := strconv.ParseFloat(ss, 64)
		if err != nil {
			iter.ReportError("client.Sample", err.Error())
			return
		}

		if iter.ReadArray() {
			iter.ReportError("client.Sample", "expected ]")
		}

		*(*Sample)(ptr) = Sample{
			TimestampMs: int64(t),
			Value:       v,
		}
	})
}
