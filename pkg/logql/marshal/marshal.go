// Package marshal converts internal objects to loghttp model objects.  This package is designed to work with
//  models in pkg/loghttp.
package marshal

import (
	"encoding/json"
	"io"

	"github.com/gorilla/websocket"
	"github.com/grafana/loki/pkg/loghttp"
	legacy "github.com/grafana/loki/pkg/loghttp/legacy"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/prometheus/prometheus/promql"
)

// WriteQueryResponseJSON marshals the promql.Value to v1 loghttp JSON and then writes it to the provided io.Writer
func WriteQueryResponseJSON(v promql.Value, w io.Writer) error {

	value, err := NewResultValue(v)

	if err != nil {
		return err
	}

	q := loghttp.QueryResponse{
		Status: "success",
		Data: loghttp.QueryResponseData{
			ResultType: value.Type(),
			Result:     value,
		},
	}

	return json.NewEncoder(w).Encode(q)
}

// WriteLabelResponseJSON marshals a logproto.LabelResponse to v1 loghttp JSON and then writes it to the provided io.Writer
//  Note that it simply directly marshals the value passed in.  This is because the label currently marshals
//  cleanly to the v1 http protocol.  If this ever changes, it will be caught by testing.
func WriteLabelResponseJSON(l logproto.LabelResponse, w io.Writer) error {
	return json.NewEncoder(w).Encode(l)
}

// WriteTailResponseJSON marshals the legacy.TailResponse to v1 loghttp JSON and then writes it to the provided connection
func WriteTailResponseJSON(r legacy.TailResponse, c *websocket.Conn) error {
	v1Response, err := NewTailResponse(r)

	if err != nil {
		return err
	}

	return c.WriteJSON(v1Response)
}
