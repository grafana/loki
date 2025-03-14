package queryrange

import (
	"fmt"

	"github.com/grafana/jsonparser"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
)

// To satisfy queryrange.Response interface(https://github.com/cortexproject/cortex/blob/21bad57b346c730d684d6d0205efef133422ab28/pkg/querier/queryrange/query_range.go#L88)
// we need to have following method as well on response types:
// GetHeaders() []*queryrange.PrometheusResponseHeader.
// This could have been done by adding "Headers" field with custom type in proto definition for various Response types which doesn't work because
// gogoproto doesn't generate getters for custom types so adding them here.
// See issue https://github.com/gogo/protobuf/issues/477 for more details.
// It also has issue of generating slices without pointer to the custom type for Repeated customtype fields, see https://github.com/gogo/protobuf/issues/478
// which is why we also have to do conversion from non-pointer to pointer type.

func (m *LokiLabelNamesResponse) GetHeaders() []*queryrangebase.PrometheusResponseHeader {
	if m != nil {
		return convertPrometheusResponseHeadersToPointers(m.Headers)
	}
	return nil
}

func (m *LokiLabelNamesResponse) SetHeader(name, value string) {
	m.Headers = setHeader(m.Headers, name, value)
}

func (m *LokiLabelNamesResponse) WithHeaders(h []queryrangebase.PrometheusResponseHeader) queryrangebase.Response {
	m.Headers = h
	return m
}

func (m *LokiSeriesResponse) GetHeaders() []*queryrangebase.PrometheusResponseHeader {
	if m != nil {
		return convertPrometheusResponseHeadersToPointers(m.Headers)
	}
	return nil
}

func (m *LokiSeriesResponse) SetHeader(name, value string) {
	m.Headers = setHeader(m.Headers, name, value)
}

func (m *LokiSeriesResponse) WithHeaders(h []queryrangebase.PrometheusResponseHeader) queryrangebase.Response {
	m.Headers = h
	return m
}

// UnmarshalJSON decodes from loghttpSeriesResponse JSON format directly into
// the protobuf LokiSeriesResponse.
func (m *LokiSeriesResponse) UnmarshalJSON(data []byte) error {
	var err error
	m.Status, err = jsonparser.GetString(data, "status")
	if err != nil {
		return err
	}

	var parseErr error
	_, err = jsonparser.ArrayEach(data, func(value []byte, vt jsonparser.ValueType, _ int, _ error) {
		if vt != jsonparser.Object {
			parseErr = fmt.Errorf("unexpected data type: got(%s), expected (object)", vt)
			return
		}

		identifier := logproto.SeriesIdentifier{}
		parseErr = jsonparser.ObjectEach(value, func(key, val []byte, vt jsonparser.ValueType, _ int) error {
			if vt != jsonparser.String {
				return fmt.Errorf("unexpected label value type: got(%s), expected (string)", vt)
			}
			v, err := jsonparser.ParseString(val)
			if err != nil {
				return err
			}
			k, err := jsonparser.ParseString(key)
			if err != nil {
				return err
			}

			identifier.Labels = append(identifier.Labels, logproto.SeriesIdentifier_LabelsEntry{Key: k, Value: v})
			return nil
		})

		if parseErr != nil {
			return
		}
		m.Data = append(m.Data, identifier)
	}, "data")
	if parseErr != nil {
		return parseErr
	}
	return err
}

func (m *LokiPromResponse) GetHeaders() []*queryrangebase.PrometheusResponseHeader {
	if m != nil {
		return m.Response.GetHeaders()
	}
	return nil
}

func (m *LokiPromResponse) SetHeader(name, value string) {
	m.Response.SetHeader(name, value)
}

func (m *LokiPromResponse) WithHeaders(h []queryrangebase.PrometheusResponseHeader) queryrangebase.Response {
	m.Response.Headers = convertPrometheusResponseHeadersToPointers(h)
	return m
}

func (m *LokiResponse) GetHeaders() []*queryrangebase.PrometheusResponseHeader {
	if m != nil {
		return convertPrometheusResponseHeadersToPointers(m.Headers)
	}
	return nil
}

func (m *LokiResponse) SetHeader(name, value string) {
	m.Headers = setHeader(m.Headers, name, value)
}

func (m *LokiResponse) WithHeaders(h []queryrangebase.PrometheusResponseHeader) queryrangebase.Response {
	m.Headers = h
	return m
}

func convertPrometheusResponseHeadersToPointers(h []queryrangebase.PrometheusResponseHeader) []*queryrangebase.PrometheusResponseHeader {
	if h == nil {
		return nil
	}

	resp := make([]*queryrangebase.PrometheusResponseHeader, len(h))
	for i := range h {
		resp[i] = &h[i]
	}

	return resp
}

// setHeader returns the passed headers with the new key-valur pair. Existing
// entries with the same key are overridden. The order is *not* maintained.
func setHeader(headers []queryrangebase.PrometheusResponseHeader, key, value string) []queryrangebase.PrometheusResponseHeader {
	for i, h := range headers {
		if h.Name == key {
			headers[i].Values = []string{value}
			return headers
		}
	}

	return append(headers, queryrangebase.PrometheusResponseHeader{Name: key, Values: []string{value}})
}

// GetHeaders returns the HTTP headers in the response.
func (m *IndexStatsResponse) GetHeaders() []*queryrangebase.PrometheusResponseHeader {
	if m != nil {
		return convertPrometheusResponseHeadersToPointers(m.Headers)
	}
	return nil
}

func (m *IndexStatsResponse) SetHeader(name, value string) {
	m.Headers = setHeader(m.Headers, name, value)
}

func (m *IndexStatsResponse) WithHeaders(h []queryrangebase.PrometheusResponseHeader) queryrangebase.Response {
	m.Headers = h
	return m
}

// GetHeaders returns the HTTP headers in the response.
func (m *VolumeResponse) GetHeaders() []*queryrangebase.PrometheusResponseHeader {
	if m != nil {
		return convertPrometheusResponseHeadersToPointers(m.Headers)
	}
	return nil
}

func (m *VolumeResponse) SetHeader(name, value string) {
	m.Headers = setHeader(m.Headers, name, value)
}

func (m *VolumeResponse) WithHeaders(h []queryrangebase.PrometheusResponseHeader) queryrangebase.Response {
	m.Headers = h
	return m
}

// GetHeaders returns the HTTP headers in the response.
func (m *TopKSketchesResponse) GetHeaders() []*queryrangebase.PrometheusResponseHeader {
	if m != nil {
		return convertPrometheusResponseHeadersToPointers(m.Headers)
	}
	return nil
}

func (m *TopKSketchesResponse) SetHeader(name, value string) {
	m.Headers = setHeader(m.Headers, name, value)
}

func (m *TopKSketchesResponse) WithHeaders(h []queryrangebase.PrometheusResponseHeader) queryrangebase.Response {
	m.Headers = h
	return m
}

// GetHeaders returns the HTTP headers in the response.
func (m *QuantileSketchResponse) GetHeaders() []*queryrangebase.PrometheusResponseHeader {
	if m != nil {
		return convertPrometheusResponseHeadersToPointers(m.Headers)
	}
	return nil
}

func (m *QuantileSketchResponse) SetHeader(name, value string) {
	m.Headers = setHeader(m.Headers, name, value)
}

func (m *QuantileSketchResponse) WithHeaders(h []queryrangebase.PrometheusResponseHeader) queryrangebase.Response {
	m.Headers = h
	return m
}

// GetHeaders returns the HTTP headers in the response.
func (m *CountMinSketchResponse) GetHeaders() []*queryrangebase.PrometheusResponseHeader {
	if m != nil {
		return convertPrometheusResponseHeadersToPointers(m.Headers)
	}
	return nil
}

func (m *CountMinSketchResponse) SetHeader(name, value string) {
	m.Headers = setHeader(m.Headers, name, value)
}

func (m *CountMinSketchResponse) WithHeaders(h []queryrangebase.PrometheusResponseHeader) queryrangebase.Response {
	m.Headers = h
	return m
}

func (m *ShardsResponse) GetHeaders() []*queryrangebase.PrometheusResponseHeader {
	if m != nil {
		return convertPrometheusResponseHeadersToPointers(m.Headers)
	}
	return nil
}

func (m *ShardsResponse) SetHeader(name, value string) {
	m.Headers = setHeader(m.Headers, name, value)
}

func (m *ShardsResponse) WithHeaders(h []queryrangebase.PrometheusResponseHeader) queryrangebase.Response {
	m.Headers = h
	return m
}

// GetHeaders returns the HTTP headers in the response.
func (m *DetectedFieldsResponse) GetHeaders() []*queryrangebase.PrometheusResponseHeader {
	if m != nil {
		return convertPrometheusResponseHeadersToPointers(m.Headers)
	}
	return nil
}

func (m *DetectedFieldsResponse) SetHeader(name, value string) {
	m.Headers = setHeader(m.Headers, name, value)
}

func (m *DetectedFieldsResponse) WithHeaders(h []queryrangebase.PrometheusResponseHeader) queryrangebase.Response {
	m.Headers = h
	return m
}

// GetHeaders returns the HTTP headers in the response.
func (m *QueryPatternsResponse) GetHeaders() []*queryrangebase.PrometheusResponseHeader {
	if m != nil {
		return convertPrometheusResponseHeadersToPointers(m.Headers)
	}
	return nil
}

// GetHeaders returns the HTTP headers in the response.
func (m *DetectedLabelsResponse) GetHeaders() []*queryrangebase.PrometheusResponseHeader {
	if m != nil {
		return convertPrometheusResponseHeadersToPointers(m.Headers)
	}
	return nil
}

func (m *QueryPatternsResponse) SetHeader(name, value string) {
	m.Headers = setHeader(m.Headers, name, value)
}

func (m *QueryPatternsResponse) WithHeaders(h []queryrangebase.PrometheusResponseHeader) queryrangebase.Response {
	m.Headers = h
	return m
}

func (m *DetectedLabelsResponse) SetHeader(name, value string) {
	m.Headers = setHeader(m.Headers, name, value)
}

func (m *DetectedLabelsResponse) WithHeaders(h []queryrangebase.PrometheusResponseHeader) queryrangebase.Response {
	m.Headers = h
	return m
}
