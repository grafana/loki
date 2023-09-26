// Provenance-includes-location: https://github.com/weaveworks/common/blob/main/grpc/cancel.go
// Provenance-includes-license: Apache-2.0
// Provenance-includes-copyright: Weaveworks Ltd.

package grpcutil

import (
	"context"
	"errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// IsCanceled checks whether an error comes from an operation being canceled
func IsCanceled(err error) bool {
	if errors.Is(err, context.Canceled) {
		return true
	}
	s, ok := status.FromError(err)
	if ok && s.Code() == codes.Canceled {
		return true
	}
	return false
}
