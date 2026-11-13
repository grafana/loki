package v2

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
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

	"github.com/grafana/loki/v3/pkg/lokifrontend/frontend/v2/frontendv2pb"
	"github.com/grafana/loki/v3/pkg/querier/queryrange"
	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
	"github.com/grafana/loki/v3/pkg/querier/stats"
	"github.com/grafana/loki/v3/pkg/querier/testutil"
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
		Plan:  testutil.MustPlan(`{foo="bar"} | json`),
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

// When the frontend stops using a scheduler (it left the ring or DNS, or the frontend is stopping), the scheduler
// cancels the requests it holds for this frontend, so requests waiting for a response must fail instead of waiting
// for a response that will never come.
func TestFrontendFailsRequestsWhenSchedulerIsRemoved(t *testing.T) {
	cfg := Config{}
	flagext.DefaultValues(&cfg)
	f, ms := setupFrontend(t, cfg, nil)

	// Safety net, so that the test fails instead of hanging.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
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
	}()

	// send request
	start := time.Now()
	resp, err := f.RoundTripGRPC(user.InjectOrgID(ctx, "test"), &httpgrpc.HTTPRequest{})
	require.Nil(t, resp)
	requireRetryableSchedulerError(t, err)
	require.Less(t, time.Since(start), 5*time.Second)

	ms.checkWithLock(func() {
		require.Equal(t, 1, len(ms.msgs))
	})
}

func requireRetryableSchedulerError(t *testing.T, err error) {
	t.Helper()
	require.Error(t, err)
	require.NotErrorIs(t, err, context.DeadlineExceeded, "request should have failed before the test deadline")
	httpResp, ok := httpgrpc.HTTPResponseFromError(err)
	require.True(t, ok, "expected an httpgrpc error, got %v", err)
	require.Equal(t, int32(http.StatusServiceUnavailable), httpResp.Code)
}

// A scheduler accepts a request and then goes away before any querier answers it: the request must fail promptly
// with a retryable error instead of waiting for the caller's context.
func TestFrontendFailsRequestsWhenSchedulerStreamEnds(t *testing.T) {
	for _, tc := range []struct {
		name       string
		afterReply func(frontend schedulerpb.SchedulerForFrontend_FrontendLoopServer) (bool, error)
	}{
		{
			// What Scheduler.FrontendLoop does when it stops: send SHUTTING_DOWN and end the stream.
			name: "scheduler stops",
			afterReply: func(frontend schedulerpb.SchedulerForFrontend_FrontendLoopServer) (bool, error) {
				return true, frontend.Send(&schedulerpb.SchedulerToFrontend{Status: schedulerpb.SHUTTING_DOWN})
			},
		},
		{
			name: "stream fails",
			afterReply: func(_ schedulerpb.SchedulerForFrontend_FrontendLoopServer) (bool, error) {
				return true, errors.New("connection reset")
			},
		},
		{
			// What Scheduler.setRunState does when the scheduler leaves the ring: an unsolicited SHUTTING_DOWN,
			// while the stream stays open.
			name: "unsolicited shutting down",
			afterReply: func(frontend schedulerpb.SchedulerForFrontend_FrontendLoopServer) (bool, error) {
				return false, frontend.Send(&schedulerpb.SchedulerToFrontend{Status: schedulerpb.SHUTTING_DOWN})
			},
		},
	} {
		for _, useDo := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/Do=%t", tc.name, useDo), func(t *testing.T) {
				cfg := Config{}
				flagext.DefaultValues(&cfg)
				if useDo {
					cfg.Encoding = EncodingProtobuf
				} else {
					cfg.Encoding = EncodingJSON
				}
				// The scheduler accepts the request, but no querier ever answers it.
				f, ms := setupFrontend(t, cfg, nil)
				ms.setAfterReply(func(frontend schedulerpb.SchedulerForFrontend_FrontendLoopServer, msg *schedulerpb.FrontendToScheduler) (bool, error) {
					if msg.Type != schedulerpb.ENQUEUE {
						return false, nil
					}
					return tc.afterReply(frontend)
				})

				// Safety net, so that the test fails instead of hanging.
				ctx, cancel := context.WithTimeout(user.InjectOrgID(context.Background(), "test"), 10*time.Second)
				defer cancel()

				start := time.Now()
				if useDo {
					resp, err := f.Do(ctx, &queryrange.LokiRequest{
						Query: `{foo="bar"} | json`,
						Plan:  testutil.MustPlan(`{foo="bar"} | json`),
					})
					require.Nil(t, resp)
					requireRetryableSchedulerError(t, err)
				} else {
					resp, err := f.RoundTripGRPC(ctx, &httpgrpc.HTTPRequest{})
					require.Nil(t, resp)
					requireRetryableSchedulerError(t, err)
				}
				require.Less(t, time.Since(start), 5*time.Second)
			})
		}
	}
}

// The error returned for a request lost with its scheduler is retried by the query-range retry middleware, which
// enqueues the request again.
func TestFrontendRequestLostWithSchedulerIsRetried(t *testing.T) {
	const userID = "test"
	ctx, cancel := context.WithTimeout(user.InjectOrgID(context.Background(), userID), 10*time.Second)
	defer cancel()

	req := &queryrange.LokiRequest{
		Query: `{foo="bar"} | json`,
		Plan:  testutil.MustPlan(`{foo="bar"} | json`),
	}
	expected, err := queryrange.NewEmptyResponse(req)
	require.NoError(t, err)
	httpResp, err := queryrange.DefaultCodec.EncodeHTTPGrpcResponse(ctx, &httpgrpc.HTTPRequest{Url: "/loki/api/v1/query_range"}, expected)
	require.NoError(t, err)

	enqueues := atomic.NewInt64(0)
	cfg := Config{}
	flagext.DefaultValues(&cfg)
	cfg.Encoding = EncodingProtobuf
	f, ms := setupFrontend(t, cfg, func(f *Frontend, msg *schedulerpb.FrontendToScheduler) *schedulerpb.SchedulerToFrontend {
		// Only the second attempt gets an answer.
		if msg.Type == schedulerpb.ENQUEUE && enqueues.Inc() > 1 {
			go sendResponseWithDelay(f, 10*time.Millisecond, userID, msg.QueryID, httpResp)
		}
		return &schedulerpb.SchedulerToFrontend{Status: schedulerpb.OK}
	})
	// The first attempt is lost with its scheduler.
	ms.setAfterReply(func(frontend schedulerpb.SchedulerForFrontend_FrontendLoopServer, msg *schedulerpb.FrontendToScheduler) (bool, error) {
		if msg.Type == schedulerpb.ENQUEUE && enqueues.Load() == 1 {
			return true, frontend.Send(&schedulerpb.SchedulerToFrontend{Status: schedulerpb.SHUTTING_DOWN})
		}
		return false, nil
	})

	handler := queryrangebase.NewRetryMiddleware(log.NewNopLogger(), 5, nil, constants.Loki).Wrap(f)
	resp, err := handler.Do(ctx, req)
	require.NoError(t, err)
	require.Equal(t, expected.(*queryrange.LokiResponse).Data, resp.(*queryrange.LokiResponse).Data)
	require.Equal(t, int64(2), enqueues.Load())
}

// A response from the querier can arrive at the same time as the stream to the scheduler ends. The caller must get
// exactly one of them, and neither side may block.
func TestFrontendResponseRacingWithSchedulerStreamEnd(t *testing.T) {
	const userID = "test"
	enqueues := atomic.NewInt64(0)
	cfg := Config{}
	flagext.DefaultValues(&cfg)
	f, ms := setupFrontend(t, cfg, func(f *Frontend, msg *schedulerpb.FrontendToScheduler) *schedulerpb.SchedulerToFrontend {
		// Vary the delay, so that the response arrives sometimes before and sometimes after the stream ends.
		delay := time.Duration(enqueues.Inc()%10) * 200 * time.Microsecond
		go sendResponseWithDelay(f, delay, userID, msg.QueryID, &httpgrpc.HTTPResponse{Code: 200, Body: []byte("ok")})
		return &schedulerpb.SchedulerToFrontend{Status: schedulerpb.OK}
	})
	ms.setAfterReply(func(frontend schedulerpb.SchedulerForFrontend_FrontendLoopServer, msg *schedulerpb.FrontendToScheduler) (bool, error) {
		if msg.Type != schedulerpb.ENQUEUE {
			return false, nil
		}
		return true, frontend.Send(&schedulerpb.SchedulerToFrontend{Status: schedulerpb.SHUTTING_DOWN})
	})

	ctx, cancel := context.WithTimeout(user.InjectOrgID(context.Background(), userID), 20*time.Second)
	defer cancel()

	var ok, failed int
	for i := 0; i < 10; i++ {
		resp, err := f.RoundTripGRPC(ctx, &httpgrpc.HTTPRequest{})
		if err != nil {
			requireRetryableSchedulerError(t, err)
			failed++
			continue
		}
		require.Equal(t, int32(200), resp.Code)
		ok++
	}
	t.Logf("responses: %d, failed with the stream: %d", ok, failed)
}

func TestEnqueuedRequests(t *testing.T) {
	newReq := func(id uint64) *frontendRequest {
		return &frontendRequest{queryID: id, response: make(chan ResponseTuple, 1)}
	}

	e := newEnqueuedRequests()
	completed, answered, pending := newReq(1), newReq(2), newReq(3)
	e.add(completed)
	e.add(answered)
	e.add(pending)
	require.Equal(t, 3, e.count())

	// A completed request is no longer tracked.
	e.remover(completed.queryID)()
	require.Equal(t, 2, e.count())

	// A request whose response is already waiting in its channel keeps that response.
	answered.response <- ResponseTuple{&frontendv2pb.QueryResultRequest{QueryID: answered.queryID}, nil}

	e.failAll(log.NewNopLogger(), "scheduler:9095")
	require.Equal(t, 0, e.count())

	require.Empty(t, completed.response)
	require.NoError(t, (<-answered.response).error)
	requireRetryableSchedulerError(t, (<-pending.response).error)

	// Removing after failAll is a no-op, and failing again does not block nor deliver anything.
	e.remover(pending.queryID)()
	e.failAll(log.NewNopLogger(), "scheduler:9095")
	require.Empty(t, pending.response)
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

	mu             sync.Mutex
	frontendAddr   map[string]int
	msgs           []*schedulerpb.FrontendToScheduler
	afterReplyFunc func(frontend schedulerpb.SchedulerForFrontend_FrontendLoopServer, msg *schedulerpb.FrontendToScheduler) (bool, error)
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

		m.mu.Lock()
		afterReply := m.afterReplyFunc
		m.mu.Unlock()
		if afterReply != nil {
			if done, err := afterReply(frontend, msg); done || err != nil {
				return err
			}
		}
	}
}

// setAfterReply sets a function that is called after the scheduler has replied to a message. If it returns
// done=true or an error, the scheduler ends the stream (with that error, if any).
func (m *mockScheduler) setAfterReply(fn func(frontend schedulerpb.SchedulerForFrontend_FrontendLoopServer, msg *schedulerpb.FrontendToScheduler) (done bool, err error)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.afterReplyFunc = fn
}

// Errors reported by the scheduler at enqueue time are returned by RoundTripGRPC too, rather than dereferencing the
// missing response.
func TestFrontendSchedulerErrorRoundTripGRPC(t *testing.T) {
	cfg := Config{}
	flagext.DefaultValues(&cfg)
	f, _ := setupFrontend(t, cfg, func(_ *Frontend, _ *schedulerpb.FrontendToScheduler) *schedulerpb.SchedulerToFrontend {
		return &schedulerpb.SchedulerToFrontend{Status: schedulerpb.TOO_MANY_REQUESTS_PER_TENANT}
	})

	resp, err := f.RoundTripGRPC(user.InjectOrgID(context.Background(), "test"), &httpgrpc.HTTPRequest{})
	require.Nil(t, resp)
	httpResp, ok := httpgrpc.HTTPResponseFromError(err)
	require.True(t, ok, "expected an httpgrpc error, got %v", err)
	require.Equal(t, int32(http.StatusTooManyRequests), httpResp.Code)
}
