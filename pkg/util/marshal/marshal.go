// Package marshal converts internal objects to loghttp model objects.  This
// package is designed to work with models in pkg/loghttp.
package marshal

import (
	"bytes"
	jsonv1 "encoding/json"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"io"
	"net/http"

	"github.com/gorilla/websocket"
	"github.com/prometheus/prometheus/promql/parser"

	"github.com/grafana/loki/v3/pkg/loghttp"
	legacy "github.com/grafana/loki/v3/pkg/loghttp/legacy"
	"github.com/grafana/loki/v3/pkg/logproto"
	logqlmodel "github.com/grafana/loki/v3/pkg/logql/model"
	"github.com/grafana/loki/v3/pkg/logql/model/stats"
	"github.com/grafana/loki/v3/pkg/util/httpreq"
	utiljson "github.com/grafana/loki/v3/pkg/util/json"
	marshal_legacy "github.com/grafana/loki/v3/pkg/util/marshal/legacy"
)

// newEncoder returns an Encoder suitable for writing Loki API responses to w.
// Log lines are HTML-escaped and invalid UTF-8 is replaced rather than rejected.
// The names we write are fixed, so skip the duplicate-name check, which allocates for every object member.
func newEncoder(w io.Writer) *jsontext.Encoder {
	return jsontext.NewEncoder(w, jsontext.EscapeForHTML(true), jsontext.AllowInvalidUTF8(true), jsontext.AllowDuplicateNames(true))
}

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
	case *logproto.IndexStatsResponse:
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
	// The Encoder terminates the top-level value with a newline.
	err := EncodeResult(data, warnings, statistics, newEncoder(w), encodeFlags)
	if err != nil {
		return fmt.Errorf("could not write JSON response: %w", err)
	}
	return nil
}

// WriteLabelResponseJSON marshals a logproto.LabelResponse to v1 loghttp JSON
// and then writes it to the provided io.Writer.
func WriteLabelResponseJSON(data []string, w io.Writer) error {
	v1Response := loghttp.LabelResponse{
		Status: "success",
		Data:   data,
	}

	return writeJSON(w, v1Response)
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
	// Tail responses are sent as websocket messages, so buffer the whole
	// response and drop the newline the Encoder adds after it.
	var buf bytes.Buffer
	err := EncodeTailResult(r, newEncoder(&buf), encodeFlags)
	if err != nil {
		return fmt.Errorf("could not write JSON tail response: %w", err)
	}
	_, err = w.Write(bytes.TrimSuffix(buf.Bytes(), []byte("\n")))
	return err
}

// WriteSeriesResponseJSON marshals a logproto.SeriesResponse to v1 loghttp JSON and then
// writes it to the provided io.Writer.
func WriteSeriesResponseJSON(series []logproto.SeriesIdentifier, w io.Writer) error {
	adapter := &seriesResponseAdapter{
		Status: "success",
		Data:   make([]map[string]string, 0, len(series)),
	}

	for _, series := range series {
		labels := series.GetLabels()
		m := make(map[string]string, len(labels))
		for _, pair := range labels {
			m[pair.Key] = pair.Value
		}
		adapter.Data = append(adapter.Data, m)
	}

	return writeJSON(w, adapter)
}

// This struct exists primarily because we can't specify a repeated map in proto v3.
// Otherwise, we'd use that + gogoproto.jsontag to avoid this layer of indirection
type seriesResponseAdapter struct {
	Status string              `json:"status"`
	Data   []map[string]string `json:"data"`
}

// WriteIndexStatsResponseJSON marshals an IndexStatsResponse to JSON and then
// writes it to the provided io.Writer.
func WriteIndexStatsResponseJSON(r *logproto.IndexStatsResponse, w io.Writer) error {
	return writeJSON(w, r)
}

// WriteIndexShardsResponseJSON marshals a indexgatewaypb.ShardsResponse to JSON and then
// writes it to the provided io.Writer.
func WriteIndexShardsResponseJSON(r *logproto.ShardsResponse, w io.Writer) error {
	return writeJSON(w, r)
}

// WriteVolumeResponseJSON marshals a logproto.VolumeResponse to JSON and then
// writes it to the provided io.Writer.
func WriteVolumeResponseJSON(r *logproto.VolumeResponse, w io.Writer) error {
	return writeJSON(w, r)
}

// WriteDetectedFieldsResponseJSON writes detected fields or values and the
// statistics of the underlying log query as a JSON response.
func WriteDetectedFieldsResponseJSON(r *logproto.DetectedFieldsResponse, statistics stats.Result, w io.Writer) error {
	return writeJSON(w, struct {
		*logproto.DetectedFieldsResponse
		Statistics stats.Result `json:"stats"`
	}{r, statistics})
}

// WriteQueryPatternsResponseJSON marshals a logproto.QueryPatternsResponse to JSON and then
// writes it to the provided io.Writer.
func WriteQueryPatternsResponseJSON(r *logproto.QueryPatternsResponse, w io.Writer) error {
	e := utiljson.Latching(newEncoder(w))
	e.Tokens(jsontext.BeginObject)
	e.StringField("status", "success")
	e.Tokens(jsontext.String("data"), jsontext.BeginArray)
	for _, series := range r.Series {
		e.Tokens(jsontext.BeginObject)
		e.StringField("pattern", series.Pattern)
		e.StringField("level", series.Level)
		e.Tokens(jsontext.String("samples"), jsontext.BeginArray)
		for _, sample := range series.Samples {
			e.Tokens(
				jsontext.BeginArray,
				jsontext.Int(sample.Timestamp.Unix()),
				jsontext.Int(sample.Value),
				jsontext.EndArray,
			)
		}
		e.Tokens(jsontext.EndArray, jsontext.EndObject)
	}
	e.Tokens(jsontext.EndArray, jsontext.EndObject)
	return e.Err()
}

// WriteDetectedLabelsResponseJSON marshals a logproto.DetectedLabelsResponse to JSON and then
// writes it to the provided io.Writer.
func WriteDetectedLabelsResponseJSON(r *logproto.DetectedLabelsResponse, w io.Writer) error {
	return writeJSON(w, r)
}

// writeJSON marshals v to w using the semantics of encoding/json v1, followed
// by a newline.
func writeJSON(w io.Writer, v any) error {
	return json.MarshalEncode(jsontext.NewEncoder(w), v, jsonv1.DefaultOptionsV1())
}
