package unmarshal

import (
	"encoding/json"
	"io"

	"github.com/grafana/loki/internal/logproto"
)

// DecodePushRequest directly decodes json to a logproto.PushRequest
func DecodePushRequest(b io.Reader, r *logproto.PushRequest) error {
	return json.NewDecoder(b).Decode(r)
}
