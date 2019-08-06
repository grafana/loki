package queryrange

import (
	"context"
	fmt "fmt"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/go-kit/kit/log"
	"github.com/stretchr/testify/require"
	"github.com/weaveworks/common/httpgrpc"
)

func TestRetry(t *testing.T) {
	var try int32

	for _, tc := range []struct {
		name    string
		handler Handler
		resp    *APIResponse
		err     error
	}{
		{
			name: "retry failures",
			handler: HandlerFunc(func(_ context.Context, req *Request) (*APIResponse, error) {
				if atomic.AddInt32(&try, 1) == 5 {
					return &APIResponse{Status: "Hello World"}, nil
				}
				return nil, fmt.Errorf("fail")
			}),
			resp: &APIResponse{Status: "Hello World"},
		},
		{
			name: "don't retry 400s",
			handler: HandlerFunc(func(_ context.Context, req *Request) (*APIResponse, error) {
				return nil, httpgrpc.Errorf(http.StatusBadRequest, "Bad Request")
			}),
			err: httpgrpc.Errorf(http.StatusBadRequest, "Bad Request"),
		},
		{
			name: "retry 500s",
			handler: HandlerFunc(func(_ context.Context, req *Request) (*APIResponse, error) {
				return nil, httpgrpc.Errorf(http.StatusInternalServerError, "Internal Server Error")
			}),
			err: httpgrpc.Errorf(http.StatusInternalServerError, "Internal Server Error"),
		},
		{
			name: "last error",
			handler: HandlerFunc(func(_ context.Context, req *Request) (*APIResponse, error) {
				if atomic.AddInt32(&try, 1) == 5 {
					return nil, httpgrpc.Errorf(http.StatusBadRequest, "Bad Request")
				}
				return nil, httpgrpc.Errorf(http.StatusInternalServerError, "Internal Server Error")
			}),
			err: httpgrpc.Errorf(http.StatusBadRequest, "Bad Request"),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			try = 0
			h := NewRetryMiddleware(log.NewNopLogger(), 5).Wrap(tc.handler)
			resp, err := h.Do(nil, nil)
			require.Equal(t, tc.err, err)
			require.Equal(t, tc.resp, resp)
		})
	}
}
