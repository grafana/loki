package querytee

import (
	"fmt"

	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
)

// NonDecodableResponse is a minimal response type used when returning errors.
// It satisfies the queryrangebase.Response interface, and allows the querytee
// to capture responses that would otherwise be lost.
type NonDecodableResponse struct {
	StatusCode int
	Body       []byte
}

// Reset implements proto.Message
func (e *NonDecodableResponse) Reset() {}

// String implements proto.Message
func (e *NonDecodableResponse) String() string {
	return fmt.Sprintf("ErrorResponse{StatusCode: %d}", e.StatusCode)
}

// ProtoMessage implements proto.Message
func (e *NonDecodableResponse) ProtoMessage() {}

// GetHeaders implements queryrangebase.Response
func (e *NonDecodableResponse) GetHeaders() []*queryrangebase.PrometheusResponseHeader {
	return []*queryrangebase.PrometheusResponseHeader{}
}

// WithHeaders implements queryrangebase.Response
func (e *NonDecodableResponse) WithHeaders(_ []queryrangebase.PrometheusResponseHeader) queryrangebase.Response {
	return &NonDecodableResponse{
		StatusCode: e.StatusCode,
		Body:       e.Body,
	}
}

// SetHeader implements queryrangebase.Response
func (e *NonDecodableResponse) SetHeader(_, _ string) {}
