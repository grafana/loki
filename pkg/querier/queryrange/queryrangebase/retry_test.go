package queryrangebase

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/go-kit/log"
	"github.com/gogo/status"
	"github.com/grafana/dskit/httpgrpc"
	"github.com/stretchr/testify/require"
	"go.uber.org/atomic"
	"google.golang.org/grpc/codes"

	"github.com/grafana/loki/v3/pkg/util/constants"
)

func TestRetry(t *testing.T) {
	var try atomic.Int32

	for _, tc := range []struct {
		name          string
		handler       Handler
		resp          Response
		err           error
		expectedCalls int
	}{
		{
			name: "retry failures",
			handler: HandlerFunc(func(_ context.Context, _ Request) (Response, error) {
				if try.Inc() == 5 {
					return &PrometheusResponse{Status: "Hello World"}, nil
				}
				return nil, fmt.Errorf("fail")
			}),
			resp:          &PrometheusResponse{Status: "Hello World"},
			expectedCalls: 5,
		},
		{
			name: "don't retry 400s",
			handler: HandlerFunc(func(_ context.Context, _ Request) (Response, error) {
				try.Inc()
				return nil, httpgrpc.Errorf(http.StatusBadRequest, "Bad Request")
			}),
			err:           httpgrpc.Errorf(http.StatusBadRequest, "Bad Request"),
			expectedCalls: 1,
		},
		{
			name: "retry 500s",
			handler: HandlerFunc(func(_ context.Context, _ Request) (Response, error) {
				try.Inc()
				return nil, httpgrpc.Errorf(http.StatusInternalServerError, "Internal Server Error")
			}),
			err:           httpgrpc.Errorf(http.StatusInternalServerError, "Internal Server Error"),
			expectedCalls: 5,
		},
		{
			name: "last error",
			handler: HandlerFunc(func(_ context.Context, _ Request) (Response, error) {
				if try.Inc() == 5 {
					return nil, httpgrpc.Errorf(http.StatusBadRequest, "Bad Request")
				}
				return nil, httpgrpc.Errorf(http.StatusInternalServerError, "Internal Server Error")
			}),
			err:           httpgrpc.Errorf(http.StatusBadRequest, "Bad Request"),
			expectedCalls: 5,
		},
		// previous entires in this table use httpgrpc.Errorf for generating the error which also populates the Details field with the marshalled http response.
		// Next set of tests validate the retry behavior when using protobuf encoding where the status does not include the details.
		{
			name: "protobuf enc don't retry 400s",
			handler: HandlerFunc(func(_ context.Context, _ Request) (Response, error) {
				try.Inc()
				return nil, status.New(codes.Code(http.StatusBadRequest), "Bad Request").Err()
			}),
			err:           status.New(codes.Code(http.StatusBadRequest), "Bad Request").Err(),
			expectedCalls: 1,
		},
		{
			name: "protobuf enc retry 500s",
			handler: HandlerFunc(func(_ context.Context, _ Request) (Response, error) {
				try.Inc()
				return nil, status.New(codes.Code(http.StatusInternalServerError), "Internal Server Error").Err()
			}),
			err:           status.New(codes.Code(http.StatusInternalServerError), "Internal Server Error").Err(),
			expectedCalls: 5,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			try.Store(0)
			h := NewRetryMiddleware(log.NewNopLogger(), 5, nil, constants.Loki).Wrap(tc.handler)
			req := &PrometheusRequest{
				Query: `{env="test"} |= "error"`,
			}
			resp, err := h.Do(context.Background(), req)
			require.Equal(t, tc.err, err)
			require.Equal(t, tc.resp, resp)
			require.EqualValues(t, tc.expectedCalls, try.Load())
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
	_, err := NewRetryMiddleware(log.NewNopLogger(), 5, nil, constants.Loki).Wrap(
		HandlerFunc(func(_ context.Context, _ Request) (Response, error) {
			try.Inc()
			return nil, ctx.Err()
		}),
	).Do(ctx, req)
	require.Equal(t, int32(0), try.Load())
	require.Equal(t, ctx.Err(), err)

	ctx, cancel = context.WithCancel(context.Background())
	_, err = NewRetryMiddleware(log.NewNopLogger(), 5, nil, constants.Loki).Wrap(
		HandlerFunc(func(_ context.Context, _ Request) (Response, error) {
			try.Inc()
			cancel()
			return nil, errors.New("failed")
		}),
	).Do(ctx, req)
	require.Equal(t, int32(1), try.Load())
	require.Equal(t, ctx.Err(), err)
}
