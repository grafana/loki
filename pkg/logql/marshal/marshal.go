// Package marshal converts internal objects to loghttp model objects.  This package is designed to work with
//  models in pkg/loghttp.
package marshal

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/gorilla/websocket"
	"github.com/grafana/loki/pkg/loghttp"
	legacy "github.com/grafana/loki/pkg/loghttp/legacy"

	"github.com/grafana/loki/pkg/logql"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/prometheus/prometheus/promql"
)

// WriteQueryResponseJSON marshals the promql.Value to v1 loghttp JSON and then writes it to the provided io.Writer
func WriteQueryResponseJSON(v promql.Value, w io.Writer) error {

	var err error
	var value loghttp.ResultValue
	var resType loghttp.ResultType

	switch v.Type() {
	case loghttp.ResultTypeStream:
		resType = loghttp.ResultTypeStream
		s, ok := v.(logql.Streams)

		if !ok {
			return fmt.Errorf("unexpected type %T for streams", s)
		}

		value, err = NewStreams(s)

		if err != nil {
			return err
		}
	case loghttp.ResultTypeVector:
		resType = loghttp.ResultTypeVector
		vector, ok := v.(promql.Vector)

		if !ok {
			return fmt.Errorf("unexpected type %T for vector", vector)
		}

		value = NewVector(vector)
	case loghttp.ResultTypeMatrix:
		resType = loghttp.ResultTypeMatrix
		m, ok := v.(promql.Matrix)

		if !ok {
			return fmt.Errorf("unexpected type %T for matrix", m)
		}

		value = NewMatrix(m)
	default:
		return fmt.Errorf("v1 endpoints do not support type %s", v.Type())
	}

	j := loghttp.QueryResponse{
		Status: "success",
		Data: loghttp.QueryResponseData{
			ResultType: resType,
			Result:     value,
		},
	}

	return json.NewEncoder(w).Encode(j)
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
