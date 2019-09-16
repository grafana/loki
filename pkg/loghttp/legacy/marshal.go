package legacy

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/gorilla/websocket"
	"github.com/grafana/loki/pkg/logproto"

	"github.com/grafana/loki/pkg/logql"
	"github.com/prometheus/prometheus/promql"
)

//WriteQueryResponseJSON marshals promql.Value to JSON and then writes it to the provided io.Writer
//  Note that it simply directly marshals the value passed in.  This is because promql currently marshals
//  cleanly to the legacy http protocol.  If this ever changes, it will be caught by testing and we will have to handle
//  legacy like we do v1:  1) exchange promql.Value for model objects 2) marshal the model objects
func WriteQueryResponseJSON(v promql.Value, w io.Writer) error {
	if v.Type() != logql.ValueTypeStreams { // jpe - consider defining this in legacy
		return fmt.Errorf("Legacy endpoints only support %s result type, current type is %s", logql.ValueTypeStreams, v.Type())
	}

	j := map[string]interface{}{
		"streams": v,
	}

	return json.NewEncoder(w).Encode(j)
}

func WriteLabelResponseJSON(l logproto.LabelResponse, w io.Writer) error {
	return json.NewEncoder(w).Encode(l)
}

func WriteTailResponseJSON(r TailResponse, c *websocket.Conn) error {
	return c.WriteJSON(r)
}
