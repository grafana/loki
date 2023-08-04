package queryrange

import "github.com/grafana/loki/pkg/querier/queryrange/queryrangebase"

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

func (m *LokiSeriesResponse) WithHeaders(h []queryrangebase.PrometheusResponseHeader) queryrangebase.Response {
	m.Headers = h
	return m
}

func (m *LokiPromResponse) GetHeaders() []*queryrangebase.PrometheusResponseHeader {
	if m != nil {
		return m.Response.GetHeaders()
	}
	return nil
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

// GetHeaders returns the HTTP headers in the response.
func (m *IndexStatsResponse) GetHeaders() []*queryrangebase.PrometheusResponseHeader {
	if m != nil {
		return convertPrometheusResponseHeadersToPointers(m.Headers)
	}
	return nil
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

func (m *TopKSketchesResponse) WithHeaders(h []queryrangebase.PrometheusResponseHeader) queryrangebase.Response {
	m.Headers = h
	return m
}
