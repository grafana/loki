// Package marshal converts internal objects to loghttp model objects.  This
// package is designed to work with models in pkg/loghttp.
package marshal

import (
	"fmt"
	"io"
	"net/http"

	"github.com/gorilla/websocket"
	jsoniter "github.com/json-iterator/go"
	"github.com/prometheus/prometheus/promql/parser"

	"github.com/grafana/loki/v3/pkg/loghttp"
	legacy "github.com/grafana/loki/v3/pkg/loghttp/legacy"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logqlmodel"
	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
	indexStats "github.com/grafana/loki/v3/pkg/storage/stores/index/stats"
	"github.com/grafana/loki/v3/pkg/util/httpreq"
	marshal_legacy "github.com/grafana/loki/v3/pkg/util/marshal/legacy"
)

func WriteResponseJSON(r *http.Request, v any, w http.ResponseWriter) error {
	switch result := v.(type) {
	case logqlmodel.Result:
		version := loghttp.GetVersion(r.RequestURI)
		encodeFlags := httpreq.ExtractEncodingFlags(r)
		if version == loghttp.VersionV1 {
			return WriteQueryResponseJSON(result.Data, result.Warnings, result.Statistics, w, encodeFlags)
		}

		return marshal_legacy.WriteQueryResponseJSON(result, w)
	case *logproto.LabelResponse:
		version := loghttp.GetVersion(r.RequestURI)
		if version == loghttp.VersionV1 {
			return WriteLabelResponseJSON(result.GetValues(), w)
		}

		return marshal_legacy.WriteLabelResponseJSON(*result, w)
	case *logproto.SeriesResponse:
		return WriteSeriesResponseJSON(result.GetSeries(), w)
	case *indexStats.Stats:
		return WriteIndexStatsResponseJSON(result, w)
	case *logproto.VolumeResponse:
		return WriteVolumeResponseJSON(result, w)
	case *logproto.QueryPatternsResponse:
		return WriteQueryPatternsResponseJSON(result, w)
	}
	return fmt.Errorf("unknown response type %T", v)
}

// WriteQueryResponseJSON marshals the promql.Value to v1 loghttp JSON and then
// writes it to the provided io.Writer.
func WriteQueryResponseJSON(data parser.Value, warnings []string, statistics stats.Result, w io.Writer, encodeFlags httpreq.EncodingFlags) error {
	s := jsoniter.ConfigFastest.BorrowStream(w)
	defer jsoniter.ConfigFastest.ReturnStream(s)
	err := EncodeResult(data, warnings, statistics, s, encodeFlags)
	if err != nil {
		return fmt.Errorf("could not write JSON response: %w", err)
	}
	s.WriteRaw("\n")
	return s.Flush()
}

// WriteLabelResponseJSON marshals a logproto.LabelResponse to v1 loghttp JSON
// and then writes it to the provided io.Writer.
func WriteLabelResponseJSON(data []string, w io.Writer) error {
	v1Response := loghttp.LabelResponse{
		Status: "success",
		Data:   data,
	}

	s := jsoniter.ConfigFastest.BorrowStream(w)
	defer jsoniter.ConfigFastest.ReturnStream(s)
	s.WriteVal(v1Response)
	s.WriteRaw("\n")
	return s.Flush()
}

// WebsocketWriter knows how to write message to a websocket connection.
type WebsocketWriter interface {
	WriteMessage(int, []byte) error
}

type websocketJSONWriter struct {
	WebsocketWriter
}

func (w *websocketJSONWriter) Write(p []byte) (n int, err error) {
	err = w.WriteMessage(websocket.TextMessage, p)
	if err != nil {
		return 0, err
	}
	return len(p), nil
}

func NewWebsocketJSONWriter(ws WebsocketWriter) io.Writer {
	return &websocketJSONWriter{ws}
}

// WriteTailResponseJSON marshals the legacy.TailResponse to v1 loghttp JSON and
// then writes it to the provided writer.
func WriteTailResponseJSON(r legacy.TailResponse, w io.Writer, encodeFlags httpreq.EncodingFlags) error {
	// TODO(salvacorts): I think we can dismiss the new TailResponse and be an alias of legacy.TailResponse
	// v1Response, err := NewTailResponse(r)
	// if err != nil {
	// 	return err
	// }
	s := jsoniter.ConfigFastest.BorrowStream(w)
	defer jsoniter.ConfigFastest.ReturnStream(s)

	err := EncodeTailResult(r, s, encodeFlags)
	if err != nil {
		return fmt.Errorf("could not write JSON tail response: %w", err)
	}
	return s.Flush()
}

// WriteSeriesResponseJSON marshals a logproto.SeriesResponse to v1 loghttp JSON and then
// writes it to the provided io.Writer.
func WriteSeriesResponseJSON(series []logproto.SeriesIdentifier, w io.Writer) error {
	adapter := &seriesResponseAdapter{
		Status: "success",
		Data:   make([]map[string]string, 0, len(series)),
	}

	for _, series := range series {
		m := make(map[string]string, 0)
		for _, pair := range series.GetLabels() {
			m[pair.Key] = pair.Value
		}
		adapter.Data = append(adapter.Data, m)
	}

	s := jsoniter.ConfigFastest.BorrowStream(w)
	defer jsoniter.ConfigFastest.ReturnStream(s)
	s.WriteVal(adapter)
	s.WriteRaw("\n")
	return s.Flush()
}

// This struct exists primarily because we can't specify a repeated map in proto v3.
// Otherwise, we'd use that + gogoproto.jsontag to avoid this layer of indirection
type seriesResponseAdapter struct {
	Status string              `json:"status"`
	Data   []map[string]string `json:"data"`
}

// WriteIndexStatsResponseJSON marshals a gatewaypb.Stats to JSON and then
// writes it to the provided io.Writer.
func WriteIndexStatsResponseJSON(r *indexStats.Stats, w io.Writer) error {
	s := jsoniter.ConfigFastest.BorrowStream(w)
	defer jsoniter.ConfigFastest.ReturnStream(s)
	s.WriteVal(r)
	s.WriteRaw("\n")
	return s.Flush()
}

// WriteIndexShardsResponseJSON marshals a indexgatewaypb.ShardsResponse to JSON and then
// writes it to the provided io.Writer.
func WriteIndexShardsResponseJSON(r *logproto.ShardsResponse, w io.Writer) error {
	s := jsoniter.ConfigFastest.BorrowStream(w)
	defer jsoniter.ConfigFastest.ReturnStream(s)
	s.WriteVal(r)
	s.WriteRaw("\n")
	return s.Flush()
}

// WriteVolumeResponseJSON marshals a logproto.VolumeResponse to JSON and then
// writes it to the provided io.Writer.
func WriteVolumeResponseJSON(r *logproto.VolumeResponse, w io.Writer) error {
	s := jsoniter.ConfigFastest.BorrowStream(w)
	defer jsoniter.ConfigFastest.ReturnStream(s)
	s.WriteVal(r)
	s.WriteRaw("\n")
	return s.Flush()
}

// WriteDetectedFieldsResponseJSON marshals a logproto.DetectedFieldsResponse to JSON and then
// writes it to the provided io.Writer.
func WriteDetectedFieldsResponseJSON(r *logproto.DetectedFieldsResponse, w io.Writer) error {
	s := jsoniter.ConfigFastest.BorrowStream(w)
	defer jsoniter.ConfigFastest.ReturnStream(s)
	s.WriteVal(r)
	s.WriteRaw("\n")
	return s.Flush()
}

// WriteQueryPatternsResponseJSON marshals a logproto.QueryPatternsResponse to JSON and then
// writes it to the provided io.Writer.
func WriteQueryPatternsResponseJSON(r *logproto.QueryPatternsResponse, w io.Writer) error {
	s := jsoniter.ConfigFastest.BorrowStream(w)
	defer jsoniter.ConfigFastest.ReturnStream(s)
	s.WriteObjectStart()
	s.WriteObjectField("status")
	s.WriteString("success")

	s.WriteMore()
	s.WriteObjectField("data")
	s.WriteArrayStart()
	if len(r.Series) > 0 {
		for i, series := range r.Series {
			s.WriteObjectStart()
			s.WriteObjectField("pattern")
			s.WriteStringWithHTMLEscaped(series.Pattern)
			s.WriteMore()
			s.WriteObjectField("samples")
			s.WriteArrayStart()
			for j, sample := range series.Samples {
				s.WriteArrayStart()
				s.WriteInt64(sample.Timestamp.Unix())
				s.WriteMore()
				s.WriteInt64(sample.Value)
				s.WriteArrayEnd()
				if j < len(series.Samples)-1 {
					s.WriteMore()
				}
			}
			s.WriteArrayEnd()
			s.WriteObjectEnd()
			if i < len(r.Series)-1 {
				s.WriteMore()
			}
		}
	}
	s.WriteArrayEnd()
	s.WriteObjectEnd()
	s.WriteRaw("\n")
	return s.Flush()
}

// WriteDetectedLabelsResponseJSON marshals a logproto.DetectedLabelsResponse to JSON and then
// writes it to the provided io.Writer.
func WriteDetectedLabelsResponseJSON(r *logproto.DetectedLabelsResponse, w io.Writer) error {
	s := jsoniter.ConfigFastest.BorrowStream(w)
	defer jsoniter.ConfigFastest.ReturnStream(s)
	s.WriteVal(r)
	s.WriteRaw("\n")
	return s.Flush()
}
