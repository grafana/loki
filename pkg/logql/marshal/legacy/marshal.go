// Package marshal converts internal objects to loghttp model objects.  This package is designed to work with
//  models in pkg/loghttp/legacy.
package marshal

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/gorilla/websocket"
	loghttp "github.com/grafana/loki/pkg/loghttp/legacy"
	"github.com/grafana/loki/pkg/logproto"

	"github.com/grafana/loki/pkg/logql"
	"github.com/prometheus/prometheus/promql"
)

// Note that the below methods directly marshal the values passed in.  This is because these objects currently marshal
// cleanly to the legacy http protocol (because that was how it was initially implemented).  If this ever changes,
// it will be caught by testing and we will have to handle legacy like we do v1:  1) exchange a variety of structs for
// for loghttp model objects 2) marshal the loghttp model objects

// WriteQueryResponseJSON marshals promql.Value to legacy loghttp JSON and then writes it to the provided io.Writer
func WriteQueryResponseJSON(v promql.Value, w io.Writer) error {
	if v.Type() != logql.ValueTypeStreams {
		return fmt.Errorf("legacy endpoints only support %s result type, current type is %s", logql.ValueTypeStreams, v.Type())
	}

	j := map[string]interface{}{
		"streams": v,
	}

	return json.NewEncoder(w).Encode(j)
}

// WriteLabelResponseJSON marshals the logproto.LabelResponse to legacy loghttp JSON and then writes it to the provided writer
func WriteLabelResponseJSON(l logproto.LabelResponse, w io.Writer) error {
	return json.NewEncoder(w).Encode(l)
}

// WriteTailResponseJSON marshals the TailResponse to legacy loghttp JSON and then writes it to the provided connection
func WriteTailResponseJSON(r loghttp.TailResponse, c *websocket.Conn) error {
	return c.WriteJSON(r)
}
