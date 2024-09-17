package ruler

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/grafana/dskit/flagext"
	"github.com/grafana/dskit/httpgrpc"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/promql"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	"github.com/grafana/loki/v3/pkg/loghttp"
	"github.com/grafana/loki/v3/pkg/util/log"
	"github.com/grafana/loki/v3/pkg/validation"
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
		handleFn: func(_ context.Context, _ *httpgrpc.HTTPRequest, _ ...grpc.CallOption) (*httpgrpc.HTTPResponse, error) {
			// sleep for slightly longer than the timeout
			time.Sleep(timeout + (100 * time.Millisecond))
			return &httpgrpc.HTTPResponse{
				Code:    http.StatusInternalServerError,
				Headers: nil,
				Body:    []byte("will not get here, will timeout before"),
			}, nil
		},
	}

	ev, err := NewRemoteEvaluator(cli, limits, log.Logger, prometheus.NewRegistry())
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
		handleFn: func(_ context.Context, _ *httpgrpc.HTTPRequest, _ ...grpc.CallOption) (*httpgrpc.HTTPResponse, error) {
			// generate a response of random bytes that's just too big for the max response size
			var resp = make([]byte, exceededSize)
			_, err = rand.Read(resp)
			require.NoError(t, err)

			return &httpgrpc.HTTPResponse{
				Code:    http.StatusOK,
				Headers: nil,
				Body:    resp,
			}, nil
		},
	}

	ev, err := NewRemoteEvaluator(cli, limits, log.Logger, prometheus.NewRegistry())
	require.NoError(t, err)

	ctx := context.Background()
	ctx = user.InjectOrgID(ctx, "test")

	_, err = ev.Eval(ctx, "sum(rate({foo=\"bar\"}[5m]))", time.Now())
	require.Error(t, err)
	// cannot use require.ErrorIs(t, err, context.DeadlineExceeded) because the original error is not wrapped correctly
	require.ErrorContains(t, err, fmt.Sprintf("%d bytes exceeds response size limit of %d", exceededSize, maxSize))
}

func TestRemoteEvalScalar(t *testing.T) {
	defaultLimits := defaultLimitsTestConfig()
	limits, err := validation.NewOverrides(defaultLimits, nil)
	require.NoError(t, err)

	var (
		now   = time.Now()
		value = 19
	)

	cli := mockClient{
		handleFn: func(_ context.Context, _ *httpgrpc.HTTPRequest, _ ...grpc.CallOption) (*httpgrpc.HTTPResponse, error) {
			// this is somewhat bleeding the abstraction, but it's more idiomatic/readable than constructing
			// the expected JSON response by hand
			resp := loghttp.QueryResponse{
				Status: loghttp.QueryStatusSuccess,
				Data: loghttp.QueryResponseData{
					ResultType: loghttp.ResultTypeScalar,
					Result: loghttp.Scalar{
						Value:     model.SampleValue(value),
						Timestamp: model.TimeFromUnixNano(now.UnixNano()),
					},
				},
			}

			out, err := json.Marshal(resp)
			require.NoError(t, err)

			return &httpgrpc.HTTPResponse{
				Code:    http.StatusOK,
				Headers: nil,
				Body:    out,
			}, nil
		},
	}

	ev, err := NewRemoteEvaluator(cli, limits, log.Logger, prometheus.NewRegistry())
	require.NoError(t, err)

	ctx := context.Background()
	ctx = user.InjectOrgID(ctx, "test")

	res, err := ev.Eval(ctx, "19", now)
	require.NoError(t, err)
	require.IsType(t, promql.Scalar{}, res.Data)
	require.EqualValues(t, value, res.Data.(promql.Scalar).V)
	// we lose nanosecond precision due to promql types
	require.Equal(t, now.Unix(), res.Data.(promql.Scalar).T)
}

// TestRemoteEvalEmptyScalarResponse validates that an empty scalar response is valid and does not cause an error
func TestRemoteEvalEmptyScalarResponse(t *testing.T) {
	defaultLimits := defaultLimitsTestConfig()
	limits, err := validation.NewOverrides(defaultLimits, nil)
	require.NoError(t, err)

	cli := mockClient{
		handleFn: func(_ context.Context, _ *httpgrpc.HTTPRequest, _ ...grpc.CallOption) (*httpgrpc.HTTPResponse, error) {
			// this is somewhat bleeding the abstraction, but it's more idiomatic/readable than constructing
			// the expected JSON response by hand
			resp := loghttp.QueryResponse{
				Status: loghttp.QueryStatusSuccess,
				Data: loghttp.QueryResponseData{
					ResultType: loghttp.ResultTypeScalar,
					Result:     loghttp.Scalar{},
				},
			}

			out, err := json.Marshal(resp)
			require.NoError(t, err)

			return &httpgrpc.HTTPResponse{
				Code:    http.StatusOK,
				Headers: nil,
				Body:    out,
			}, nil
		},
	}

	ev, err := NewRemoteEvaluator(cli, limits, log.Logger, prometheus.NewRegistry())
	require.NoError(t, err)

	ctx := context.Background()
	ctx = user.InjectOrgID(ctx, "test")

	res, err := ev.Eval(ctx, "sum(rate({foo=\"bar\"}[5m]))", time.Now())
	require.NoError(t, err)
	require.Empty(t, res.Data)
}

// TestRemoteEvalVectorResponse validates that an empty vector response is valid and does not cause an error
func TestRemoteEvalVectorResponse(t *testing.T) {
	defaultLimits := defaultLimitsTestConfig()
	limits, err := validation.NewOverrides(defaultLimits, nil)
	require.NoError(t, err)

	now := time.Now()
	value := 35891

	cli := mockClient{
		handleFn: func(_ context.Context, _ *httpgrpc.HTTPRequest, _ ...grpc.CallOption) (*httpgrpc.HTTPResponse, error) {
			// this is somewhat bleeding the abstraction, but it's more idiomatic/readable than constructing
			// the expected JSON response by hand
			resp := loghttp.QueryResponse{
				Status: loghttp.QueryStatusSuccess,
				Data: loghttp.QueryResponseData{
					ResultType: loghttp.ResultTypeVector,
					Result: loghttp.Vector{
						{
							Timestamp: model.TimeFromUnixNano(now.UnixNano()),
							Value:     model.SampleValue(value),
							Metric: map[model.LabelName]model.LabelValue{
								model.LabelName("foo"): model.LabelValue("bar"),
							},
						},
						{
							Timestamp: model.TimeFromUnixNano(now.UnixNano()),
							Value:     model.SampleValue(value),
							Metric: map[model.LabelName]model.LabelValue{
								model.LabelName("bar"): model.LabelValue("baz"),
							},
						},
					},
				},
			}

			out, err := json.Marshal(resp)
			require.NoError(t, err)

			return &httpgrpc.HTTPResponse{
				Code:    http.StatusOK,
				Headers: nil,
				Body:    out,
			}, nil
		},
	}

	ev, err := NewRemoteEvaluator(cli, limits, log.Logger, prometheus.NewRegistry())
	require.NoError(t, err)

	ctx := context.Background()
	ctx = user.InjectOrgID(ctx, "test")

	res, err := ev.Eval(ctx, "sum(rate({foo=\"bar\"}[5m]))", now)
	require.NoError(t, err)
	require.Len(t, res.Data, 2)
	require.IsType(t, promql.Vector{}, res.Data)
	vector := res.Data.(promql.Vector)
	require.EqualValues(t, now.UnixMilli(), vector[0].T)
	require.EqualValues(t, value, vector[0].F)
	require.EqualValues(t, map[string]string{
		"foo": "bar",
	}, vector[0].Metric.Map())
}

// TestRemoteEvalEmptyVectorResponse validates that an empty vector response is valid and does not cause an error
func TestRemoteEvalEmptyVectorResponse(t *testing.T) {
	defaultLimits := defaultLimitsTestConfig()
	limits, err := validation.NewOverrides(defaultLimits, nil)
	require.NoError(t, err)

	cli := mockClient{
		handleFn: func(_ context.Context, _ *httpgrpc.HTTPRequest, _ ...grpc.CallOption) (*httpgrpc.HTTPResponse, error) {
			// this is somewhat bleeding the abstraction, but it's more idiomatic/readable than constructing
			// the expected JSON response by hand
			resp := loghttp.QueryResponse{
				Status: loghttp.QueryStatusSuccess,
				Data: loghttp.QueryResponseData{
					ResultType: loghttp.ResultTypeVector,
					Result:     loghttp.Vector{},
				},
			}

			out, err := json.Marshal(resp)
			require.NoError(t, err)

			return &httpgrpc.HTTPResponse{
				Code:    http.StatusOK,
				Headers: nil,
				Body:    out,
			}, nil
		},
	}

	ev, err := NewRemoteEvaluator(cli, limits, log.Logger, prometheus.NewRegistry())
	require.NoError(t, err)

	ctx := context.Background()
	ctx = user.InjectOrgID(ctx, "test")

	_, err = ev.Eval(ctx, "sum(rate({foo=\"bar\"}[5m]))", time.Now())
	require.NoError(t, err)
}

func TestRemoteEvalErrorResponse(t *testing.T) {
	defaultLimits := defaultLimitsTestConfig()
	limits, err := validation.NewOverrides(defaultLimits, nil)
	require.NoError(t, err)

	var respErr = fmt.Errorf("some error occurred")

	cli := mockClient{
		handleFn: func(_ context.Context, _ *httpgrpc.HTTPRequest, _ ...grpc.CallOption) (*httpgrpc.HTTPResponse, error) {
			return nil, respErr
		},
	}

	ev, err := NewRemoteEvaluator(cli, limits, log.Logger, prometheus.NewRegistry())
	require.NoError(t, err)

	ctx := context.Background()
	ctx = user.InjectOrgID(ctx, "test")

	_, err = ev.Eval(ctx, "sum(rate({foo=\"bar\"}[5m]))", time.Now())
	require.ErrorContains(t, err, "rule evaluation failed")
	require.ErrorContains(t, err, respErr.Error())
}

func TestRemoteEvalNon2xxResponse(t *testing.T) {
	defaultLimits := defaultLimitsTestConfig()
	limits, err := validation.NewOverrides(defaultLimits, nil)
	require.NoError(t, err)

	const httpErr = http.StatusInternalServerError

	cli := mockClient{
		handleFn: func(_ context.Context, _ *httpgrpc.HTTPRequest, _ ...grpc.CallOption) (*httpgrpc.HTTPResponse, error) {
			return &httpgrpc.HTTPResponse{
				Code: httpErr,
			}, nil
		},
	}

	ev, err := NewRemoteEvaluator(cli, limits, log.Logger, prometheus.NewRegistry())
	require.NoError(t, err)

	ctx := context.Background()
	ctx = user.InjectOrgID(ctx, "test")

	_, err = ev.Eval(ctx, "sum(rate({foo=\"bar\"}[5m]))", time.Now())
	require.ErrorContains(t, err, fmt.Sprintf("unsuccessful/unexpected response - status code %d", httpErr))
}

func TestRemoteEvalNonJSONResponse(t *testing.T) {
	defaultLimits := defaultLimitsTestConfig()
	limits, err := validation.NewOverrides(defaultLimits, nil)
	require.NoError(t, err)

	cli := mockClient{
		handleFn: func(_ context.Context, _ *httpgrpc.HTTPRequest, _ ...grpc.CallOption) (*httpgrpc.HTTPResponse, error) {
			return &httpgrpc.HTTPResponse{
				Code: http.StatusOK,
				Body: []byte("this is not json"),
			}, nil
		},
	}

	ev, err := NewRemoteEvaluator(cli, limits, log.Logger, prometheus.NewRegistry())
	require.NoError(t, err)

	ctx := context.Background()
	ctx = user.InjectOrgID(ctx, "test")

	_, err = ev.Eval(ctx, "sum(rate({foo=\"bar\"}[5m]))", time.Now())
	require.ErrorContains(t, err, "unexpected body encoding, not valid JSON")
}

func TestRemoteEvalUnsupportedResultResponse(t *testing.T) {
	defaultLimits := defaultLimitsTestConfig()
	limits, err := validation.NewOverrides(defaultLimits, nil)
	require.NoError(t, err)

	cli := mockClient{
		handleFn: func(_ context.Context, _ *httpgrpc.HTTPRequest, _ ...grpc.CallOption) (*httpgrpc.HTTPResponse, error) {
			// this is somewhat bleeding the abstraction, but it's more idiomatic/readable than constructing
			// the expected JSON response by hand
			resp := loghttp.QueryResponse{
				Status: loghttp.QueryStatusSuccess,
				Data: loghttp.QueryResponseData{
					// stream responses are not yet supported
					ResultType: loghttp.ResultTypeStream,
					Result:     loghttp.Streams{},
				},
			}

			out, err := json.Marshal(resp)
			require.NoError(t, err)

			return &httpgrpc.HTTPResponse{
				Code:    http.StatusOK,
				Headers: nil,
				Body:    out,
			}, nil
		},
	}

	ev, err := NewRemoteEvaluator(cli, limits, log.Logger, prometheus.NewRegistry())
	require.NoError(t, err)

	ctx := context.Background()
	ctx = user.InjectOrgID(ctx, "test")

	_, err = ev.Eval(ctx, "sum(rate({foo=\"bar\"}[5m]))", time.Now())
	require.ErrorContains(t, err, fmt.Sprintf("unsupported result type: %q", loghttp.ResultTypeStream))
}

func defaultLimitsTestConfig() validation.Limits {
	limits := validation.Limits{}
	flagext.DefaultValues(&limits)
	return limits
}
