package queryrangebase

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/httpgrpc"
	"github.com/stretchr/testify/require"
	"go.uber.org/atomic"
)

func TestRetry(t *testing.T) {
	var try atomic.Int32

	for _, tc := range []struct {
		name    string
		handler Handler
		resp    Response
		err     error
	}{
		{
			name: "retry failures",
			handler: HandlerFunc(func(_ context.Context, req Request) (Response, error) {
				if try.Inc() == 5 {
					return &PrometheusResponse{Status: "Hello World"}, nil
				}
				return nil, fmt.Errorf("fail")
			}),
			resp: &PrometheusResponse{Status: "Hello World"},
		},
		{
			name: "don't retry 400s",
			handler: HandlerFunc(func(_ context.Context, req Request) (Response, error) {
				return nil, httpgrpc.Errorf(http.StatusBadRequest, "Bad Request")
			}),
			err: httpgrpc.Errorf(http.StatusBadRequest, "Bad Request"),
		},
		{
			name: "retry 500s",
			handler: HandlerFunc(func(_ context.Context, req Request) (Response, error) {
				return nil, httpgrpc.Errorf(http.StatusInternalServerError, "Internal Server Error")
			}),
			err: httpgrpc.Errorf(http.StatusInternalServerError, "Internal Server Error"),
		},
		{
			name: "last error",
			handler: HandlerFunc(func(_ context.Context, req Request) (Response, error) {
				if try.Inc() == 5 {
					return nil, httpgrpc.Errorf(http.StatusBadRequest, "Bad Request")
				}
				return nil, httpgrpc.Errorf(http.StatusInternalServerError, "Internal Server Error")
			}),
			err: httpgrpc.Errorf(http.StatusBadRequest, "Bad Request"),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			try.Store(0)
			h := NewRetryMiddleware(log.NewNopLogger(), 5, nil).Wrap(tc.handler)
			req := &PrometheusRequest{
				Query: `{env="test"} |= "error"`,
			}
			resp, err := h.Do(context.Background(), req)
			require.Equal(t, tc.err, err)
			require.Equal(t, tc.resp, resp)
		})
	}
}

func Test_RetryMiddlewareCancel(t *testing.T) {
	req := &PrometheusRequest{
		Query: `{env="test"} |= "error"`,
	}

	var try atomic.Int32
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := NewRetryMiddleware(log.NewNopLogger(), 5, nil).Wrap(
		HandlerFunc(func(c context.Context, r Request) (Response, error) {
			try.Inc()
			return nil, ctx.Err()
		}),
	).Do(ctx, req)
	require.Equal(t, int32(0), try.Load())
	require.Equal(t, ctx.Err(), err)

	ctx, cancel = context.WithCancel(context.Background())
	_, err = NewRetryMiddleware(log.NewNopLogger(), 5, nil).Wrap(
		HandlerFunc(func(c context.Context, r Request) (Response, error) {
			try.Inc()
			cancel()
			return nil, errors.New("failed")
		}),
	).Do(ctx, req)
	require.Equal(t, int32(1), try.Load())
	require.Equal(t, ctx.Err(), err)
}
