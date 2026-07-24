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

// saturatedErrMessage identifies saturation rejections. Clients match on both the
// status code and the message: Unavailable alone is ambiguous since it is also
// returned for connection-level failures, and the message alone could collide with
// unrelated errors that happen to contain it.
const saturatedErrMessage = "index gateway is saturated"

func newSaturatedError(reason string) error {
	return status.Errorf(saturationStatusCode, "%s (limiting reason: %s), please retry on another instance", saturatedErrMessage, reason)
}

// IsSaturatedError reports whether err (possibly wrapped) is a rejection from a
// saturated index gateway.
func IsSaturatedError(err error) bool {
	s, ok := status.FromError(err)
	return ok && s.Code() == saturationStatusCode && strings.Contains(s.Message(), saturatedErrMessage)
}
