package unmarshal

import (
	"io"

	jsoniter "github.com/json-iterator/go"

	"github.com/grafana/loki/v3/pkg/logproto"
)

// DecodePushRequest directly decodes json to a logproto.PushRequest
func DecodePushRequest(b io.Reader, r *logproto.PushRequest) error {
	dec := jsoniter.NewDecoder(b)
	dec.DisallowUnknownFields() // return error when parsing unknown field)
	return dec.Decode(r)
}
