package codec

import (
	"github.com/grafana/loki/v4/pkg/lokifrontend/frontend/transport"
	"github.com/grafana/loki/v4/pkg/querier/worker"
)

// Codec defines methods to encode and decode requests from HTTP, httpgrpc and Protobuf.
type Codec interface {
	transport.Codec
	worker.RequestCodec
}
