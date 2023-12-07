package logproto

import (
	"encoding/binary"
	stdjson "encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
	"unsafe"

	"github.com/cespare/xxhash/v2"
	jsoniter "github.com/json-iterator/go"
	"github.com/opentracing/opentracing-go"
	otlog "github.com/opentracing/opentracing-go/log"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/model/timestamp"

	"github.com/grafana/loki/pkg/querier/queryrange/queryrangebase/definitions"
	"github.com/grafana/loki/pkg/storage/chunk/cache/resultscache"
	"github.com/grafana/loki/pkg/util"
)

// ToWriteRequest converts matched slices of Labels, Samples and Metadata into a WriteRequest proto.
// It gets timeseries from the pool, so ReuseSlice() should be called when done.
func ToWriteRequest(lbls []labels.Labels, samples []LegacySample, metadata []*MetricMetadata, source WriteRequest_SourceEnum) *WriteRequest {
	req := &WriteRequest{
		Timeseries: PreallocTimeseriesSliceFromPool(),
		Metadata:   metadata,
		Source:     source,
	}

	for i, s := range samples {
		ts := TimeseriesFromPool()
		ts.Labels = append(ts.Labels, FromLabelsToLabelAdapters(lbls[i])...)
		ts.Samples = append(ts.Samples, s)
		req.Timeseries = append(req.Timeseries, PreallocTimeseries{TimeSeries: ts})
	}

	return req
}

// FromLabelAdaptersToLabels casts []LabelAdapter to labels.Labels.
// It uses unsafe, but as LabelAdapter == labels.Label this should be safe.
// This allows us to use labels.Labels directly in protos.
//
// Note: while resulting labels.Labels is supposedly sorted, this function
// doesn't enforce that. If input is not sorted, output will be wrong.
func FromLabelAdaptersToLabels(ls []LabelAdapter) labels.Labels {
	return *(*labels.Labels)(unsafe.Pointer(&ls))
}

// FromLabelsToLabelAdapters casts labels.Labels to []LabelAdapter.
// It uses unsafe, but as LabelAdapter == labels.Label this should be safe.
// This allows us to use labels.Labels directly in protos.
func FromLabelsToLabelAdapters(ls labels.Labels) []LabelAdapter {
	return *(*[]LabelAdapter)(unsafe.Pointer(&ls))
}

// FromLabelAdaptersToMetric converts []LabelAdapter to a model.Metric.
// Don't do this on any performance sensitive paths.
func FromLabelAdaptersToMetric(ls []LabelAdapter) model.Metric {
	return util.LabelsToMetric(FromLabelAdaptersToLabels(ls))
}

// FromMetricsToLabelAdapters converts model.Metric to []LabelAdapter.
// Don't do this on any performance sensitive paths.
// The result is sorted.
func FromMetricsToLabelAdapters(metric model.Metric) []LabelAdapter {
	result := make([]LabelAdapter, 0, len(metric))
	for k, v := range metric {
		result = append(result, LabelAdapter{
			Name:  string(k),
			Value: string(v),
		})
	}
	sort.Sort(byLabel(result)) // The labels should be sorted upon initialisation.
	return result
}

type byLabel []LabelAdapter

func (s byLabel) Len() int           { return len(s) }
func (s byLabel) Less(i, j int) bool { return strings.Compare(s[i].Name, s[j].Name) < 0 }
func (s byLabel) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }

// isTesting is only set from tests to get special behaviour to verify that custom sample encode and decode is used,
// both when using jsonitor or standard json package.
var isTesting = false

// MarshalJSON implements json.Marshaler.
func (s LegacySample) MarshalJSON() ([]byte, error) {
	if isTesting && math.IsNaN(s.Value) {
		return nil, fmt.Errorf("test sample")
	}

	t, err := jsoniter.ConfigCompatibleWithStandardLibrary.Marshal(model.Time(s.TimestampMs))
	if err != nil {
		return nil, err
	}
	v, err := jsoniter.ConfigCompatibleWithStandardLibrary.Marshal(model.SampleValue(s.Value))
	if err != nil {
		return nil, err
	}
	return []byte(fmt.Sprintf("[%s,%s]", t, v)), nil
}

// UnmarshalJSON implements json.Unmarshaler.
func (s *LegacySample) UnmarshalJSON(b []byte) error {
	var t model.Time
	var v model.SampleValue
	vs := [...]stdjson.Unmarshaler{&t, &v}
	if err := jsoniter.ConfigCompatibleWithStandardLibrary.Unmarshal(b, &vs); err != nil {
		return err
	}
	s.TimestampMs = int64(t)
	s.Value = float64(v)

	if isTesting && math.IsNaN(float64(v)) {
		return fmt.Errorf("test sample")
	}
	return nil
}

func SampleJsoniterEncode(ptr unsafe.Pointer, stream *jsoniter.Stream) {
	legacySample := (*LegacySample)(ptr)

	if isTesting && math.IsNaN(legacySample.Value) {
		stream.Error = fmt.Errorf("test sample")
		return
	}

	stream.WriteArrayStart()
	stream.WriteFloat64(float64(legacySample.TimestampMs) / float64(time.Second/time.Millisecond))
	stream.WriteMore()
	stream.WriteString(model.SampleValue(legacySample.Value).String())
	stream.WriteArrayEnd()
}

func SampleJsoniterDecode(ptr unsafe.Pointer, iter *jsoniter.Iterator) {
	if !iter.ReadArray() {
		iter.ReportError("logproto.LegacySample", "expected [")
		return
	}

	t := model.Time(iter.ReadFloat64() * float64(time.Second/time.Millisecond))

	if !iter.ReadArray() {
		iter.ReportError("logproto.LegacySample", "expected ,")
		return
	}

	bs := iter.ReadStringAsSlice()
	ss := *(*string)(unsafe.Pointer(&bs))
	v, err := strconv.ParseFloat(ss, 64)
	if err != nil {
		iter.ReportError("logproto.LegacySample", err.Error())
		return
	}

	if isTesting && math.IsNaN(v) {
		iter.Error = fmt.Errorf("test sample")
		return
	}

	if iter.ReadArray() {
		iter.ReportError("logproto.LegacySample", "expected ]")
	}

	*(*LegacySample)(ptr) = LegacySample{
		TimestampMs: int64(t),
		Value:       v,
	}
}

func init() {
	jsoniter.RegisterTypeEncoderFunc("logproto.LegacySample", SampleJsoniterEncode, func(unsafe.Pointer) bool { return false })
	jsoniter.RegisterTypeDecoderFunc("logproto.LegacySample", SampleJsoniterDecode)
}

// Combine unique values from multiple LabelResponses into a single, sorted LabelResponse.
func MergeLabelResponses(responses []*LabelResponse) (*LabelResponse, error) {
	if len(responses) == 0 {
		return &LabelResponse{}, nil
	} else if len(responses) == 1 {
		return responses[0], nil
	}

	unique := map[string]struct{}{}

	for _, r := range responses {
		for _, v := range r.Values {
			if _, ok := unique[v]; !ok {
				unique[v] = struct{}{}
			} else {
				continue
			}
		}
	}

	result := &LabelResponse{Values: make([]string, 0, len(unique))}

	for value := range unique {
		result.Values = append(result.Values, value)
	}

	// Sort the unique values before returning because we can't rely on map key ordering
	sort.Strings(result.Values)

	return result, nil
}

// Combine unique label sets from multiple SeriesResponse and return a single SeriesResponse.
func MergeSeriesResponses(responses []*SeriesResponse) (*SeriesResponse, error) {
	if len(responses) == 0 {
		return &SeriesResponse{}, nil
	} else if len(responses) == 1 {
		return responses[0], nil
	}

	result := &SeriesResponse{
		Series: make([]SeriesIdentifier, 0, len(responses)),
	}

	for _, r := range responses {
		result.Series = append(result.Series, r.Series...)
	}

	return result, nil
}

// Satisfy definitions.Request

// GetStart returns the start timestamp of the request in milliseconds.
func (m *IndexStatsRequest) GetStart() time.Time {
	return time.Unix(0, m.From.UnixNano())
}

// GetEnd returns the end timestamp of the request in milliseconds.
func (m *IndexStatsRequest) GetEnd() time.Time {
	return time.Unix(0, m.Through.UnixNano())
}

// GetStep returns the step of the request in milliseconds.
func (m *IndexStatsRequest) GetStep() int64 { return 0 }

// GetQuery returns the query of the request.
func (m *IndexStatsRequest) GetQuery() string {
	return m.Matchers
}

// GetCachingOptions returns the caching options.
func (m *IndexStatsRequest) GetCachingOptions() (res definitions.CachingOptions) { return }

// WithStartEnd clone the current request with different start and end timestamp.
func (m *IndexStatsRequest) WithStartEnd(start, end time.Time) definitions.Request {
	clone := *m
	clone.From = model.TimeFromUnixNano(start.UnixNano())
	clone.Through = model.TimeFromUnixNano(end.UnixNano())
	return &clone
}

// WithStartEndForCache implements resultscache.Request.
func (m *IndexStatsRequest) WithStartEndForCache(start, end time.Time) resultscache.Request {
	return m.WithStartEnd(start, end).(resultscache.Request)
}

// WithQuery clone the current request with a different query.
func (m *IndexStatsRequest) WithQuery(query string) definitions.Request {
	clone := *m
	clone.Matchers = query
	return &clone
}

// LogToSpan writes information about this request to an OpenTracing span
func (m *IndexStatsRequest) LogToSpan(sp opentracing.Span) {
	sp.LogFields(
		otlog.String("query", m.GetQuery()),
		otlog.String("start", timestamp.Time(int64(m.From)).String()),
		otlog.String("end", timestamp.Time(int64(m.Through)).String()),
	)
}

func (i *IndexStatsResponse) GetHeaders() []*definitions.PrometheusResponseHeader {
	return nil
}

// Satisfy definitions.Request for Volume

// GetStart returns the start timestamp of the request in milliseconds.
func (m *VolumeRequest) GetStart() time.Time {
	return time.UnixMilli(int64(m.From))
}

// GetEnd returns the end timestamp of the request in milliseconds.
func (m *VolumeRequest) GetEnd() time.Time {
	return time.UnixMilli(int64(m.Through))
}

// GetQuery returns the query of the request.
func (m *VolumeRequest) GetQuery() string {
	return m.Matchers
}

// GetCachingOptions returns the caching options.
func (m *VolumeRequest) GetCachingOptions() (res definitions.CachingOptions) { return }

// WithStartEnd clone the current request with different start and end timestamp.
func (m *VolumeRequest) WithStartEnd(start, end time.Time) definitions.Request {
	clone := *m
	clone.From = model.TimeFromUnixNano(start.UnixNano())
	clone.Through = model.TimeFromUnixNano(end.UnixNano())
	return &clone
}

// WithStartEndForCache implements resultscache.Request.
func (m *VolumeRequest) WithStartEndForCache(start, end time.Time) resultscache.Request {
	return m.WithStartEnd(start, end).(resultscache.Request)
}

// WithQuery clone the current request with a different query.
func (m *VolumeRequest) WithQuery(query string) definitions.Request {
	clone := *m
	clone.Matchers = query
	return &clone
}

// LogToSpan writes information about this request to an OpenTracing span
func (m *VolumeRequest) LogToSpan(sp opentracing.Span) {
	sp.LogFields(
		otlog.String("query", m.GetQuery()),
		otlog.String("start", timestamp.Time(int64(m.From)).String()),
		otlog.String("end", timestamp.Time(int64(m.Through)).String()),
	)
}

// Satisfy definitions.Request for FilterChunkRefRequest

// GetStart returns the start timestamp of the request in milliseconds.
func (m *FilterChunkRefRequest) GetStart() time.Time {
	return time.UnixMilli(int64(m.From))
}

// GetEnd returns the end timestamp of the request in milliseconds.
func (m *FilterChunkRefRequest) GetEnd() time.Time {
	return time.UnixMilli(int64(m.Through))
}

// GetStep returns the step of the request in milliseconds. Always 0.
func (m *FilterChunkRefRequest) GetStep() int64 {
	return 0
}

// GetQuery returns the query of the request.
// The query is the hash for the input chunks refs and the filter expressions.
func (m *FilterChunkRefRequest) GetQuery() string {
	var encodeBuf []byte
	var chunksHash uint64
	if len(m.Refs) > 0 {
		h := xxhash.New()
		for _, ref := range m.Refs {
			_, _ = h.Write(binary.AppendUvarint(encodeBuf[:0], ref.Fingerprint))
		}
		chunksHash = h.Sum64()
	}

	// Short circuit if there are no filters.
	if len(m.Filters) == 0 {
		return fmt.Sprintf("%d", chunksHash)
	}

	var sb strings.Builder
	for i, filter := range m.Filters {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.Write(fmt.Appendf(encodeBuf[:0], "%d", filter.Operator))
		sb.WriteString("-")
		sb.WriteString(filter.Match)
	}

	return fmt.Sprintf("%d/%s", chunksHash, sb.String())
}

// GetCachingOptions returns the caching options.
func (m *FilterChunkRefRequest) GetCachingOptions() (res resultscache.CachingOptions) { return }

// WithStartEndForCache implements resultscache.Request.
func (m *FilterChunkRefRequest) WithStartEndForCache(start, end time.Time) resultscache.Request {
	// We Remove the chunks that are not within the given time range.
	chunkRefs := make([]*GroupedChunkRefs, 0, len(m.Refs))
	for _, chunkRef := range m.Refs {
		refs := make([]*ShortRef, 0, len(chunkRef.Refs))
		for _, ref := range chunkRef.Refs {
			if end.Before(ref.From.Time()) || ref.Through.Time().Before(start) {
				continue
			}
			refs = append(refs, ref)
		}
		if len(refs) > 0 {
			chunkRefs = append(chunkRefs, &GroupedChunkRefs{
				Fingerprint: chunkRef.Fingerprint,
				Tenant:      chunkRef.Tenant,
				Refs:        refs,
			})
		}
	}

	clone := *m
	clone.From = model.TimeFromUnixNano(start.UnixNano())
	clone.Through = model.TimeFromUnixNano(end.UnixNano())
	clone.Refs = chunkRefs

	return &clone
}
