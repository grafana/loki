// Package marshal converts internal objects to loghttp model objects.  This package is designed to work with
// models in pkg/loghttp/legacy.
package marshal

import (
	jsonv1 "encoding/json"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"io"

	"github.com/gorilla/websocket"

	loghttp "github.com/grafana/loki/v3/pkg/loghttp/legacy"
	"github.com/grafana/loki/v3/pkg/logproto"
	logqlmodel "github.com/grafana/loki/v3/pkg/logql/model"
)

// Note that the below methods directly marshal the values passed in.  This is because these objects currently marshal
// cleanly to the legacy http protocol (because that was how it was initially implemented).  If this ever changes,
// it will be caught by testing and we will have to handle legacy like we do v1:  1) exchange a variety of structs for
// for loghttp model objects 2) marshal the loghttp model objects

// WriteQueryResponseJSON marshals promql.Value to legacy loghttp JSON and then writes it to the provided io.Writer
func WriteQueryResponseJSON(v logqlmodel.Result, w io.Writer) error {
	if v.Data.Type() != logqlmodel.ValueTypeStreams {
		return fmt.Errorf("legacy endpoints only support %s result type, current type is %s", logqlmodel.ValueTypeStreams, v.Data.Type())
	}

	j := map[string]interface{}{
		"streams": v.Data,
		"stats":   v.Statistics,
	}

	return writeJSON(w, j)
}

// WriteLabelResponseJSON marshals the logproto.LabelResponse to legacy loghttp JSON and then writes it to the provided writer
func WriteLabelResponseJSON(l logproto.LabelResponse, w io.Writer) error {
	return writeJSON(w, l)
}

// WriteTailResponseJSON marshals the TailResponse to legacy loghttp JSON and then writes it to the provided connection
func WriteTailResponseJSON(r loghttp.TailResponse, c *websocket.Conn) error {
	return c.WriteJSON(r)
}

// writeJSON marshals v to w using the semantics of encoding/json v1, followed
// by a newline.
func writeJSON(w io.Writer, v any) error {
	return json.MarshalEncode(jsontext.NewEncoder(w), v, jsonv1.DefaultOptionsV1())
}
