package v1

import (
	"encoding/json"
	"io"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/prometheus/prometheus/promql"
)

func WriteQueryResponseJSON(v promql.Value, w io.Writer) error {

}

//WriteLabelResponseJSON marshals a logproto.LabelResponse to JSON and then writes it to the provided io.Writer
//  Note that it simply directly marshals the value passed in.  This is because the label currently marshals
//  cleanly to the v1 http protocol.  If this ever changes, it will be caught by testing.
func WriteLabelResponseJSON(l logproto.LabelResponse, w io.Writer) error {
	return json.NewEncoder(w).Encode(l)
}
