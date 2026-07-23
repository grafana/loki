package indexgateway

import (
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// saturationStatusCode is returned when the gateway rejects a request because it is
// saturated. Unavailable carries 503 semantics: the rejection is the server's capacity
// problem, not the client's fault, and the request is safe to retry elsewhere.
const saturationStatusCode = codes.Unavailable

// saturatedErrMessage identifies saturation rejections. Clients match on the message
// rather than the status code, since Unavailable is also returned for connection-level
// failures.
const saturatedErrMessage = "index gateway is saturated"

func newSaturatedError(reason string) error {
	return status.Errorf(saturationStatusCode, "%s (limiting reason: %s), please retry on another instance", saturatedErrMessage, reason)
}

// IsSaturatedError reports whether err (possibly wrapped) is a rejection from a
// saturated index gateway.
func IsSaturatedError(err error) bool {
	s, ok := status.FromError(err)
	return ok && strings.Contains(s.Message(), saturatedErrMessage)
}
