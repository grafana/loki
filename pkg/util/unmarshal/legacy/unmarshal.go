package unmarshal

import (
	jsonv1 "encoding/json"
	"encoding/json/v2"
	"io"

	"github.com/grafana/loki/v3/pkg/logproto"
)

// DecodePushRequest directly decodes json to a logproto.PushRequest
func DecodePushRequest(b io.Reader, r *logproto.PushRequest) error {
	// Return an error when parsing an unknown field.
	return json.UnmarshalRead(b, r, jsonv1.DefaultOptionsV1(), json.RejectUnknownMembers(true))
}
