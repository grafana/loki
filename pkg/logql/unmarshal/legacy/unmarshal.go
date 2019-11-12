package unmarshal

import (
	"io"

	"github.com/grafana/loki/pkg/logproto"
	json "github.com/json-iterator/go"
)

// DecodePushRequest directly decodes json to a logproto.PushRequest
func DecodePushRequest(b io.Reader, r *logproto.PushRequest) error {
	return json.NewDecoder(b).Decode(r)
}
