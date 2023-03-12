package ruler

import (
	"context"
	"fmt"
	"math/rand"
	"net/http"
	"testing"
	"time"

	"github.com/grafana/dskit/flagext"
	"github.com/stretchr/testify/require"
	"github.com/weaveworks/common/httpgrpc"
	"github.com/weaveworks/common/user"
	"google.golang.org/grpc"

	"github.com/grafana/loki/pkg/util/log"
	"github.com/grafana/loki/pkg/validation"
)

type mockClient struct {
	handleFn func(ctx context.Context, in *httpgrpc.HTTPRequest, opts ...grpc.CallOption) (*httpgrpc.HTTPResponse, error)
}

func (m mockClient) Handle(ctx context.Context, in *httpgrpc.HTTPRequest, opts ...grpc.CallOption) (*httpgrpc.HTTPResponse, error) {
	if m.handleFn == nil {
		return nil, fmt.Errorf("no handle function set")
	}

	return m.handleFn(ctx, in, opts...)
}

func TestRemoteEvalQueryTimeout(t *testing.T) {
	const timeout = 200 * time.Millisecond

	defaultLimits := defaultLimitsTestConfig()
	defaultLimits.RulerRemoteEvaluationTimeout = timeout

	limits, err := validation.NewOverrides(defaultLimits, nil)
	require.NoError(t, err)

	cli := mockClient{
		handleFn: func(ctx context.Context, in *httpgrpc.HTTPRequest, opts ...grpc.CallOption) (*httpgrpc.HTTPResponse, error) {
			// sleep for slightly longer than the timeout
			time.Sleep(timeout + time.Millisecond)
			return &httpgrpc.HTTPResponse{
				Code:    http.StatusInternalServerError,
				Headers: nil,
				Body:    []byte("will not get here, will timeout before"),
			}, nil
		},
	}

	ev, err := NewRemoteEvaluator(cli, limits, log.Logger)
	require.NoError(t, err)

	ctx := context.Background()
	ctx = user.InjectOrgID(ctx, "test")

	_, err = ev.Eval(ctx, "sum(rate({foo=\"bar\"}[5m]))", time.Now())
	require.Error(t, err)
	// cannot use require.ErrorIs(t, err, context.DeadlineExceeded) because the original error is not wrapped correctly
	require.ErrorContains(t, err, "context deadline exceeded")
}

func TestRemoteEvalMaxResponseSize(t *testing.T) {
	const maxSize = 2 * 1024 * 1024 // 2MiB
	const exceededSize = maxSize + 1

	defaultLimits := defaultLimitsTestConfig()
	defaultLimits.RulerRemoteEvaluationMaxResponseSize = maxSize

	limits, err := validation.NewOverrides(defaultLimits, nil)
	require.NoError(t, err)

	cli := mockClient{
		handleFn: func(ctx context.Context, in *httpgrpc.HTTPRequest, opts ...grpc.CallOption) (*httpgrpc.HTTPResponse, error) {
			// generate a response of random bytes that's just too big for the max response size
			var resp = make([]byte, exceededSize)
			rand.Read(resp)

			return &httpgrpc.HTTPResponse{
				Code:    http.StatusOK,
				Headers: nil,
				Body:    resp,
			}, nil
		},
	}

	ev, err := NewRemoteEvaluator(cli, limits, log.Logger)
	require.NoError(t, err)

	ctx := context.Background()
	ctx = user.InjectOrgID(ctx, "test")

	_, err = ev.Eval(ctx, "sum(rate({foo=\"bar\"}[5m]))", time.Now())
	require.Error(t, err)
	// cannot use require.ErrorIs(t, err, context.DeadlineExceeded) because the original error is not wrapped correctly
	require.ErrorContains(t, err, fmt.Sprintf("%d bytes exceeds response size limit of %d", exceededSize, maxSize))
}

func defaultLimitsTestConfig() validation.Limits {
	limits := validation.Limits{}
	flagext.DefaultValues(&limits)
	return limits
}
