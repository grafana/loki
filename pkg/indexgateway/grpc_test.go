package indexgateway

import (
	"bytes"
	"context"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/status"
)

type fakeUtilizationChecker struct {
	reason string
}

func (c *fakeUtilizationChecker) LimitingReason() string {
	return c.reason
}

func TestSaturationCheckInterceptors(t *testing.T) {
	const (
		unaryMethod    = "/indexgatewaypb.IndexGateway/GetChunkRef"
		streamMethod   = "/indexgatewaypb.IndexGateway/GetShards"
		externalMethod = "/logproto.Querier/Query"
	)

	setup := func(reasons ...string) (grpc.UnaryServerInterceptor, grpc.StreamServerInterceptor, prometheus.Gatherer) {
		reg := prometheus.NewPedanticRegistry()
		checkers := make([]UtilizationChecker, 0, len(reasons))
		for _, reason := range reasons {
			checkers = append(checkers, &fakeUtilizationChecker{reason: reason})
		}
		unary, stream := NewSaturationCheckInterceptors(reg, checkers...)
		return unary, stream, reg
	}

	requireLimitedRequests := func(t *testing.T, reg prometheus.Gatherer, expected string) {
		t.Helper()
		require.NoError(t, testutil.GatherAndCompare(reg, bytes.NewBufferString(expected),
			"loki_index_gateway_utilization_limited_requests_total"))
	}

	t.Run("requests pass through when not limiting", func(t *testing.T) {
		unary, stream, reg := setup("")

		unaryCalled := false
		resp, err := unary(context.Background(), nil, &grpc.UnaryServerInfo{FullMethod: unaryMethod},
			func(context.Context, any) (any, error) {
				unaryCalled = true
				return "response", nil
			})
		require.NoError(t, err)
		require.Equal(t, "response", resp)
		require.True(t, unaryCalled)

		streamCalled := false
		err = stream(nil, nil, &grpc.StreamServerInfo{FullMethod: streamMethod},
			func(any, grpc.ServerStream) error {
				streamCalled = true
				return nil
			})
		require.NoError(t, err)
		require.True(t, streamCalled)

		requireLimitedRequests(t, reg, "")
	})

	t.Run("unary requests are rejected when limiting", func(t *testing.T) {
		unary, _, reg := setup("cpu")

		resp, err := unary(context.Background(), nil, &grpc.UnaryServerInfo{FullMethod: unaryMethod},
			func(context.Context, any) (any, error) {
				t.Fatal("handler should not be invoked")
				return nil, nil
			})
		require.Nil(t, resp)
		require.True(t, IsSaturatedError(err))
		require.Equal(t, saturationStatusCode, status.Code(err))

		requireLimitedRequests(t, reg, `
			# HELP loki_index_gateway_utilization_limited_requests_total Total number of requests rejected by the index gateway because of resource utilization based limiting.
			# TYPE loki_index_gateway_utilization_limited_requests_total counter
			loki_index_gateway_utilization_limited_requests_total{reason="cpu"} 1
		`)
	})

	t.Run("stream requests are rejected when limiting", func(t *testing.T) {
		_, stream, reg := setup("memory")

		err := stream(nil, nil, &grpc.StreamServerInfo{FullMethod: streamMethod},
			func(any, grpc.ServerStream) error {
				t.Fatal("handler should not be invoked")
				return nil
			})
		require.True(t, IsSaturatedError(err))
		require.Equal(t, saturationStatusCode, status.Code(err))

		requireLimitedRequests(t, reg, `
			# HELP loki_index_gateway_utilization_limited_requests_total Total number of requests rejected by the index gateway because of resource utilization based limiting.
			# TYPE loki_index_gateway_utilization_limited_requests_total counter
			loki_index_gateway_utilization_limited_requests_total{reason="memory"} 1
		`)
	})

	t.Run("the first non-empty reason wins with multiple checkers", func(t *testing.T) {
		unary, _, reg := setup("cpu", "scheduler")

		_, err := unary(context.Background(), nil, &grpc.UnaryServerInfo{FullMethod: unaryMethod},
			func(context.Context, any) (any, error) {
				t.Fatal("handler should not be invoked")
				return nil, nil
			})
		require.True(t, IsSaturatedError(err))

		requireLimitedRequests(t, reg, `
			# HELP loki_index_gateway_utilization_limited_requests_total Total number of requests rejected by the index gateway because of resource utilization based limiting.
			# TYPE loki_index_gateway_utilization_limited_requests_total counter
			loki_index_gateway_utilization_limited_requests_total{reason="cpu"} 1
		`)
	})

	t.Run("later checkers are consulted when earlier ones report no reason", func(t *testing.T) {
		unary, _, reg := setup("", "scheduler")

		_, err := unary(context.Background(), nil, &grpc.UnaryServerInfo{FullMethod: unaryMethod},
			func(context.Context, any) (any, error) {
				t.Fatal("handler should not be invoked")
				return nil, nil
			})
		require.True(t, IsSaturatedError(err))

		requireLimitedRequests(t, reg, `
			# HELP loki_index_gateway_utilization_limited_requests_total Total number of requests rejected by the index gateway because of resource utilization based limiting.
			# TYPE loki_index_gateway_utilization_limited_requests_total counter
			loki_index_gateway_utilization_limited_requests_total{reason="scheduler"} 1
		`)
	})

	t.Run("requests pass through when no checker reports a reason", func(t *testing.T) {
		unary, _, reg := setup("", "")

		called := false
		_, err := unary(context.Background(), nil, &grpc.UnaryServerInfo{FullMethod: unaryMethod},
			func(context.Context, any) (any, error) {
				called = true
				return nil, nil
			})
		require.NoError(t, err)
		require.True(t, called)
		requireLimitedRequests(t, reg, "")
	})

	t.Run("other services are not affected by limiting", func(t *testing.T) {
		unary, stream, reg := setup("cpu")

		unaryCalled := false
		_, err := unary(context.Background(), nil, &grpc.UnaryServerInfo{FullMethod: externalMethod},
			func(context.Context, any) (any, error) {
				unaryCalled = true
				return nil, nil
			})
		require.NoError(t, err)
		require.True(t, unaryCalled)

		streamCalled := false
		err = stream(nil, nil, &grpc.StreamServerInfo{FullMethod: externalMethod},
			func(any, grpc.ServerStream) error {
				streamCalled = true
				return nil
			})
		require.NoError(t, err)
		require.True(t, streamCalled)

		requireLimitedRequests(t, reg, "")
	})
}
