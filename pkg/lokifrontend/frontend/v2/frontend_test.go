package v2

import (
	"context"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/flagext"
	"github.com/grafana/dskit/httpgrpc"
	"github.com/grafana/dskit/services"
	"github.com/grafana/dskit/user"
	"github.com/stretchr/testify/require"
	"go.uber.org/atomic"
	"google.golang.org/grpc"

	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/lokifrontend/frontend/v2/frontendv2pb"
	"github.com/grafana/loki/v3/pkg/querier/plan"
	"github.com/grafana/loki/v3/pkg/querier/queryrange"
	"github.com/grafana/loki/v3/pkg/querier/stats"
	"github.com/grafana/loki/v3/pkg/scheduler/schedulerpb"
	"github.com/grafana/loki/v3/pkg/util/constants"
	"github.com/grafana/loki/v3/pkg/util/test"
)

const testFrontendWorkerConcurrency = 5

func setupFrontend(t *testing.T, cfg Config, schedulerReplyFunc func(f *Frontend, msg *schedulerpb.FrontendToScheduler) *schedulerpb.SchedulerToFrontend) (*Frontend, *mockScheduler) {
	l, err := net.Listen("tcp", "")
	require.NoError(t, err)

	server := grpc.NewServer()

	h, p, err := net.SplitHostPort(l.Addr().String())
	require.NoError(t, err)

	grpcPort, err := strconv.Atoi(p)
	require.NoError(t, err)

	cfg.SchedulerAddress = l.Addr().String()
	cfg.WorkerConcurrency = testFrontendWorkerConcurrency
	cfg.Addr = h
	cfg.Port = grpcPort

	logger := log.NewNopLogger()
	f, err := NewFrontend(cfg, nil, logger, nil, queryrange.DefaultCodec, constants.Loki)
	require.NoError(t, err)

	frontendv2pb.RegisterFrontendForQuerierServer(server, f)

	ms := newMockScheduler(t, f, schedulerReplyFunc)
	schedulerpb.RegisterSchedulerForFrontendServer(server, ms)

	require.NoError(t, services.StartAndAwaitRunning(context.Background(), f))
	t.Cleanup(func() {
		_ = services.StopAndAwaitTerminated(context.Background(), f)
	})

	go func() {
		_ = server.Serve(l)
	}()

	t.Cleanup(func() {
		_ = l.Close()
	})

	// Wait for frontend to connect to scheduler.
	test.Poll(t, 1*time.Second, 1, func() interface{} {
		ms.mu.Lock()
		defer ms.mu.Unlock()

		return len(ms.frontendAddr)
	})

	return f, ms
}

func sendResponseWithDelay(f *Frontend, delay time.Duration, userID string, queryID uint64, resp *httpgrpc.HTTPResponse) {
	if delay > 0 {
		time.Sleep(delay)
	}

	ctx := user.InjectOrgID(context.Background(), userID)
	_, _ = f.QueryResult(ctx, &frontendv2pb.QueryResultRequest{
		QueryID: queryID,
		Response: &frontendv2pb.QueryResultRequest_HttpResponse{
			HttpResponse: resp,
		},
		Stats: &stats.Stats{},
	})
}

func TestFrontendBasicWorkflow(t *testing.T) {
	const (
		body   = "all fine here"
		userID = "test"
	)

	cfg := Config{}
	flagext.DefaultValues(&cfg)
	f, _ := setupFrontend(t, cfg, func(f *Frontend, msg *schedulerpb.FrontendToScheduler) *schedulerpb.SchedulerToFrontend {
		// We cannot call QueryResult directly, as Frontend is not yet waiting for the response.
		// It first needs to be told that enqueuing has succeeded.
		go sendResponseWithDelay(f, 100*time.Millisecond, userID, msg.QueryID, &httpgrpc.HTTPResponse{
			Code: 200,
			Body: []byte(body),
		})

		return &schedulerpb.SchedulerToFrontend{Status: schedulerpb.OK}
	})

	resp, err := f.RoundTripGRPC(user.InjectOrgID(context.Background(), userID), &httpgrpc.HTTPRequest{})
	require.NoError(t, err)
	require.Equal(t, int32(200), resp.Code)
	require.Equal(t, []byte(body), resp.Body)
}

func TestFrontendBasicWorkflowProto(t *testing.T) {
	const (
		userID = "test"
	)

	ctx := user.InjectOrgID(context.Background(), userID)

	req := &queryrange.LokiRequest{
		Query: `{foo="bar"} | json`,
		Plan: &plan.QueryPlan{
			AST: syntax.MustParseExpr(`{foo="bar"} | json`),
		},
	}

	resp, err := queryrange.NewEmptyResponse(req)
	require.NoError(t, err)
	httpReq := &httpgrpc.HTTPRequest{Url: "/loki/api/v1/query_range"}
	httpResp, err := queryrange.DefaultCodec.EncodeHTTPGrpcResponse(ctx, httpReq, resp)
	require.NoError(t, err)

	cfg := Config{}
	flagext.DefaultValues(&cfg)
	cfg.Encoding = EncodingProtobuf
	f, _ := setupFrontend(t, cfg, func(f *Frontend, msg *schedulerpb.FrontendToScheduler) *schedulerpb.SchedulerToFrontend {
		// We cannot call QueryResult directly, as Frontend is not yet waiting for the response.
		// It first needs to be told that enqueuing has succeeded.
		go sendResponseWithDelay(f, 100*time.Millisecond, userID, msg.QueryID, httpResp)

		return &schedulerpb.SchedulerToFrontend{Status: schedulerpb.OK}
	})
	actualResp, err := f.Do(ctx, req)
	require.NoError(t, err)
	require.Equal(t, resp.(*queryrange.LokiResponse).Data, actualResp.(*queryrange.LokiResponse).Data)
}

func TestFrontendRetryEnqueue(t *testing.T) {
	// Frontend uses worker concurrency to compute number of retries. We use one less failure.
	failures := atomic.NewInt64(testFrontendWorkerConcurrency - 1)
	const (
		body   = "hello world"
		userID = "test"
	)

	cfg := Config{}
	flagext.DefaultValues(&cfg)
	f, _ := setupFrontend(t, cfg, func(f *Frontend, msg *schedulerpb.FrontendToScheduler) *schedulerpb.SchedulerToFrontend {
		fail := failures.Dec()
		if fail >= 0 {
			return &schedulerpb.SchedulerToFrontend{Status: schedulerpb.SHUTTING_DOWN}
		}

		go sendResponseWithDelay(f, 100*time.Millisecond, userID, msg.QueryID, &httpgrpc.HTTPResponse{
			Code: 200,
			Body: []byte(body),
		})

		return &schedulerpb.SchedulerToFrontend{Status: schedulerpb.OK}
	})
	_, err := f.RoundTripGRPC(user.InjectOrgID(context.Background(), userID), &httpgrpc.HTTPRequest{})
	require.NoError(t, err)
}

func TestFrontendEnqueueFailure(t *testing.T) {
	cfg := Config{}
	flagext.DefaultValues(&cfg)
	f, _ := setupFrontend(t, cfg, func(_ *Frontend, _ *schedulerpb.FrontendToScheduler) *schedulerpb.SchedulerToFrontend {
		return &schedulerpb.SchedulerToFrontend{Status: schedulerpb.SHUTTING_DOWN}
	})

	_, err := f.RoundTripGRPC(user.InjectOrgID(context.Background(), "test"), &httpgrpc.HTTPRequest{})
	require.Error(t, err)
	require.True(t, strings.Contains(err.Error(), "failed to enqueue request"))
}

func TestFrontendCancellation(t *testing.T) {
	cfg := Config{}
	flagext.DefaultValues(&cfg)
	f, ms := setupFrontend(t, cfg, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	resp, err := f.RoundTripGRPC(user.InjectOrgID(ctx, "test"), &httpgrpc.HTTPRequest{})
	require.EqualError(t, err, context.DeadlineExceeded.Error())
	require.Nil(t, resp)

	// We wait a bit to make sure scheduler receives the cancellation request.
	test.Poll(t, time.Second, 2, func() interface{} {
		ms.mu.Lock()
		defer ms.mu.Unlock()

		return len(ms.msgs)
	})

	ms.checkWithLock(func() {
		require.Equal(t, 2, len(ms.msgs))
		require.True(t, ms.msgs[0].Type == schedulerpb.ENQUEUE)
		require.True(t, ms.msgs[1].Type == schedulerpb.CANCEL)
		require.True(t, ms.msgs[0].QueryID == ms.msgs[1].QueryID)
	})
}

// If FrontendWorkers are busy, cancellation passed by Query frontend may not reach
// all the frontend workers thus not reaching the scheduler as well.
// Issue: https://github.com/grafana/loki/issues/5132
func TestFrontendWorkerCancellation(t *testing.T) {
	cfg := Config{}
	flagext.DefaultValues(&cfg)
	f, ms := setupFrontend(t, cfg, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	// send multiple requests > maxconcurrency of scheduler. So that it keeps all the frontend worker busy in serving requests.
	reqCount := testFrontendWorkerConcurrency + 5
	var wg sync.WaitGroup
	for i := 0; i < reqCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, err := f.RoundTripGRPC(user.InjectOrgID(ctx, "test"), &httpgrpc.HTTPRequest{})
			require.EqualError(t, err, context.DeadlineExceeded.Error())
			require.Nil(t, resp)
		}()
	}

	wg.Wait()

	// We wait a bit to make sure scheduler receives the cancellation request.
	// 2 * reqCount because for every request, should also be corresponding cancel request
	test.Poll(t, 5*time.Second, 2*reqCount, func() interface{} {
		ms.mu.Lock()
		defer ms.mu.Unlock()

		return len(ms.msgs)
	})

	ms.checkWithLock(func() {
		require.Equal(t, 2*reqCount, len(ms.msgs))
	})
}

func TestFrontendFailedCancellation(t *testing.T) {
	cfg := Config{}
	flagext.DefaultValues(&cfg)
	f, ms := setupFrontend(t, cfg, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		time.Sleep(100 * time.Millisecond)

		// stop scheduler workers
		addr := ""
		f.schedulerWorkers.mu.Lock()
		for k := range f.schedulerWorkers.workers {
			addr = k
			break
		}
		f.schedulerWorkers.mu.Unlock()

		f.schedulerWorkers.AddressRemoved(addr)

		// Wait for worker goroutines to stop.
		time.Sleep(100 * time.Millisecond)

		// Cancel request. Frontend will try to send cancellation to scheduler, but that will fail (not visible to user).
		// Everything else should still work fine.
		cancel()
	}()

	// send request
	resp, err := f.RoundTripGRPC(user.InjectOrgID(ctx, "test"), &httpgrpc.HTTPRequest{})
	require.EqualError(t, err, context.Canceled.Error())
	require.Nil(t, resp)

	ms.checkWithLock(func() {
		require.Equal(t, 1, len(ms.msgs))
	})
}

func TestFrontendStoppingWaitsForEmptyInflightRequests(t *testing.T) {
	delayResponse := 10 * time.Millisecond
	cfg := Config{}
	flagext.DefaultValues(&cfg)
	f, _ := setupFrontend(t, cfg, func(f *Frontend, msg *schedulerpb.FrontendToScheduler) *schedulerpb.SchedulerToFrontend {
		// We cannot call QueryResult directly, as Frontend is not yet waiting for the response.
		// It first needs to be told that enqueuing has succeeded.
		go sendResponseWithDelay(f, 2*delayResponse, "test", msg.QueryID, &httpgrpc.HTTPResponse{
			Code: 200,
			Body: []byte("ok"),
		})

		// give enough time to fill up inflight requests
		time.Sleep(delayResponse)
		return &schedulerpb.SchedulerToFrontend{Status: schedulerpb.OK}
	})

	inflightRequests := 10
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	for i := 0; i < inflightRequests; i++ {
		go func() {
			_, err := f.RoundTripGRPC(user.InjectOrgID(ctx, "test"), &httpgrpc.HTTPRequest{})
			require.NoError(t, err)
		}()
	}

	require.Eventually(t, func() bool {
		return f.requests.count() == inflightRequests
	},
		3*delayResponse, // wait at least 3*delayResponse to wait for queries to be finished and removed from inlight requests map
		5*time.Millisecond,
	)

	// blocks until all inflight requests are done
	_ = f.stopping(nil)
	require.Equal(t, 0, f.requests.count())
}

func TestFrontendShuttingDownLetsSubRequestsPass(t *testing.T) {
	delayResponse := 100 * time.Millisecond
	cfg := Config{}
	flagext.DefaultValues(&cfg)
	f, _ := setupFrontend(t, cfg, func(f *Frontend, msg *schedulerpb.FrontendToScheduler) *schedulerpb.SchedulerToFrontend {
		// We cannot call QueryResult directly, as Frontend is not yet waiting for the response.
		// It first needs to be told that enqueuing has succeeded.
		go sendResponseWithDelay(f, delayResponse, "test", msg.QueryID, &httpgrpc.HTTPResponse{
			Code: 200,
			Body: []byte("ok"),
		})

		return &schedulerpb.SchedulerToFrontend{Status: schedulerpb.OK}
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	require.Equal(t, services.Running, f.State())

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		_, err := f.RoundTripGRPC(user.InjectOrgID(ctx, "test"), &httpgrpc.HTTPRequest{})
		require.NoError(t, err)
	}()

	// Wait less than delayResponse to make sure we have an inflight request that
	// already was sent to the scheduler and the service stays in Stopping state
	// for some time.
	time.Sleep(delayResponse / 10)

	f.StopAsync()

	// wait for Stopping state
	require.Eventually(t, func() bool {
		t.Log(f.State())
		return f.State() == services.Stopping
	}, delayResponse/2, 2*time.Millisecond)

	// send (sub-)request
	// This request still needs to be able to pass the RoundTripGRCP function,
	// because it may be a sub-request of a query request that was split earlier
	// into multiple sub-requests.
	_, err := f.RoundTripGRPC(user.InjectOrgID(ctx, "test"), &httpgrpc.HTTPRequest{})
	require.NoError(t, err)

	wg.Wait()
}

func TestProtobufBackwardsCompatibility(t *testing.T) {
	expected := &frontendv2pb.QueryResultRequest{
		QueryID: 42,
		Response: &frontendv2pb.QueryResultRequest_HttpResponse{
			HttpResponse: &httpgrpc.HTTPResponse{
				Code:    200,
				Headers: []*httpgrpc.Header{{Key: "foo", Values: []string{"bar"}}},
				Body:    []byte("Hello world!"),
			},
		},
		Stats: &stats.Stats{
			FetchedSeriesCount: 100,
			FetchedChunkBytes:  1024,
		},
	}

	b, err := os.ReadFile("testdata/k173.bin")
	require.NoError(t, err)

	actual := &frontendv2pb.QueryResultRequest{}
	err = actual.Unmarshal(b)
	require.NoError(t, err)

	require.IsType(t, &frontendv2pb.QueryResultRequest_HttpResponse{}, actual.Response)
	require.EqualValues(t, expected, actual)
}

type mockScheduler struct {
	t *testing.T
	f *Frontend

	replyFunc func(f *Frontend, msg *schedulerpb.FrontendToScheduler) *schedulerpb.SchedulerToFrontend

	mu           sync.Mutex
	frontendAddr map[string]int
	msgs         []*schedulerpb.FrontendToScheduler
}

func newMockScheduler(t *testing.T, f *Frontend, replyFunc func(f *Frontend, msg *schedulerpb.FrontendToScheduler) *schedulerpb.SchedulerToFrontend) *mockScheduler {
	return &mockScheduler{t: t, f: f, frontendAddr: map[string]int{}, replyFunc: replyFunc}
}

func (m *mockScheduler) checkWithLock(fn func()) {
	m.mu.Lock()
	defer m.mu.Unlock()

	fn()
}

func (m *mockScheduler) FrontendLoop(frontend schedulerpb.SchedulerForFrontend_FrontendLoopServer) error {
	init, err := frontend.Recv()
	if err != nil {
		return err
	}

	m.mu.Lock()
	m.frontendAddr[init.FrontendAddress]++
	m.mu.Unlock()

	// Ack INIT from frontend.
	if err := frontend.Send(&schedulerpb.SchedulerToFrontend{Status: schedulerpb.OK}); err != nil {
		return err
	}

	for {
		msg, err := frontend.Recv()
		if err != nil {
			return err
		}

		m.mu.Lock()
		m.msgs = append(m.msgs, msg)
		m.mu.Unlock()

		reply := &schedulerpb.SchedulerToFrontend{Status: schedulerpb.OK}
		if m.replyFunc != nil {
			reply = m.replyFunc(m.f, msg)
		}

		if err := frontend.Send(reply); err != nil {
			return err
		}
	}
}
