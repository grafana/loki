// Package marshal converts internal objects to loghttp model objects.  This
// package is designed to work with models in pkg/loghttp.
package marshal

import (
	"io"

	"github.com/grafana/loki/pkg/logql"

	"github.com/gorilla/websocket"
	json "github.com/json-iterator/go"

	"github.com/grafana/loki/pkg/loghttp"
	legacy "github.com/grafana/loki/pkg/loghttp/legacy"
	"github.com/grafana/loki/pkg/logproto"
)

// WriteQueryResponseJSON marshals the promql.Value to v1 loghttp JSON and then
// writes it to the provided io.Writer.
func WriteQueryResponseJSON(v logql.Result, w io.Writer) error {

	value, err := NewResultValue(v.Data)

	if err != nil {
		return err
	}

	q := loghttp.QueryResponse{
		Status: "success",
		Data: loghttp.QueryResponseData{
			ResultType: value.Type(),
			Result:     value,
			Statistics: v.Statistics,
		},
	}

	return json.NewEncoder(w).Encode(q)
}

// WriteLabelResponseJSON marshals a logproto.LabelResponse to v1 loghttp JSON
// and then writes it to the provided io.Writer.
func WriteLabelResponseJSON(l logproto.LabelResponse, w io.Writer) error {
	v1Response := loghttp.LabelResponse{
		Status: "success",
		Data:   l.GetValues(),
	}

	return json.NewEncoder(w).Encode(v1Response)
}

// WriteTailResponseJSON marshals the legacy.TailResponse to v1 loghttp JSON and
// then writes it to the provided connection.
func WriteTailResponseJSON(r legacy.TailResponse, c *websocket.Conn) error {
	v1Response, err := NewTailResponse(r)

	if err != nil {
		return err
	}

	return c.WriteJSON(v1Response)
}

// WriteSeriesResponseJSON marshals a logproto.SeriesResponse to v1 loghttp JSON and then
// writes it to the provided io.Writer.
func WriteSeriesResponseJSON(r logproto.SeriesResponse, w io.Writer) error {
	adapter := &seriesResponseAdapter{
		Status: "success",
		Data:   make([]map[string]string, 0, len(r.GetSeries())),
	}

	for _, series := range r.GetSeries() {
		adapter.Data = append(adapter.Data, series.GetLabels())
	}

	return json.NewEncoder(w).Encode(adapter)
}

// This struct exists primarily because we can't specify a repeated map in proto v3.
// Otherwise, we'd use that + gogoproto.jsontag to avoid this layer of indirection
type seriesResponseAdapter struct {
	Status string              `json:"status"`
	Data   []map[string]string `json:"data"`
}
