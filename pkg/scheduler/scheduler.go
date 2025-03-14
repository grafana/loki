package scheduler

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/textproto"
	"strings"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/grpcclient"
	"github.com/grafana/dskit/httpgrpc"
	"github.com/grafana/dskit/middleware"
	"github.com/grafana/dskit/ring"
	"github.com/grafana/dskit/services"
	"github.com/grafana/dskit/user"
	otgrpc "github.com/opentracing-contrib/go-grpc"
	"github.com/opentracing/opentracing-go"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"go.uber.org/atomic"
	"google.golang.org/grpc"

	"github.com/grafana/loki/v3/pkg/lokifrontend/frontend/v2/frontendv2pb"
	"github.com/grafana/loki/v3/pkg/querier/queryrange"
	"github.com/grafana/loki/v3/pkg/queue"
	"github.com/grafana/loki/v3/pkg/scheduler/limits"
	"github.com/grafana/loki/v3/pkg/scheduler/schedulerpb"
	"github.com/grafana/loki/v3/pkg/util"
	lokigrpc "github.com/grafana/loki/v3/pkg/util/httpgrpc"
	lokihttpreq "github.com/grafana/loki/v3/pkg/util/httpreq"
	lokiring "github.com/grafana/loki/v3/pkg/util/ring"
)

const (
	// NumTokens is 1 since we only need to insert 1 token to be used for leader election purposes.
	NumTokens = 1
	// ReplicationFactor should be 2 because we want 2 schedulers.
	ReplicationFactor = 2
)

var errSchedulerIsNotRunning = errors.New("scheduler is not running")

// Scheduler is responsible for queueing and dispatching queries to Queriers.
type Scheduler struct {
	services.Service

	cfg Config
	log log.Logger

	limits Limits

	connectedFrontendsMu sync.Mutex
	connectedFrontends   map[string]*connectedFrontend

	requestQueue *queue.RequestQueue
	activeUsers  *util.ActiveUsersCleanupService

	pendingRequestsMu sync.Mutex
	pendingRequests   map[requestKey]*schedulerRequest // Request is kept in this map even after being dispatched to querier. It can still be canceled at that time.

	// Subservices manager.
	subservices        *services.Manager
	subservicesWatcher *services.FailureWatcher

	// queue metrics
	queueMetrics *queue.Metrics

	// scheduler metrics.
	connectedQuerierClients  prometheus.GaugeFunc
	connectedFrontendClients prometheus.GaugeFunc
	queueDuration            prometheus.Histogram
	schedulerRunning         prometheus.Gauge
	inflightRequests         prometheus.Summary

	// Ring used for finding schedulers
	ringManager *lokiring.RingManager

	// Controls for this being a chosen scheduler
	shouldRun atomic.Bool
}

type requestKey struct {
	frontendAddr string
	queryID      uint64
}

type connectedFrontend struct {
	connections int
	frontend    schedulerpb.SchedulerForFrontend_FrontendLoopServer

	// This context is used for running all queries from the same frontend.
	// When last frontend connection is closed, context is canceled.
	ctx    context.Context
	cancel context.CancelFunc
}

type Config struct {
	MaxOutstandingPerTenant int               `yaml:"max_outstanding_requests_per_tenant"`
	MaxQueueHierarchyLevels int               `yaml:"max_queue_hierarchy_levels"`
	QuerierForgetDelay      time.Duration     `yaml:"querier_forget_delay"`
	GRPCClientConfig        grpcclient.Config `yaml:"grpc_client_config" doc:"description=This configures the gRPC client used to report errors back to the query-frontend."`
	// Schedulers ring
	UseSchedulerRing bool                `yaml:"use_scheduler_ring"`
	SchedulerRing    lokiring.RingConfig `yaml:"scheduler_ring,omitempty" doc:"description=The hash ring configuration. This option is required only if use_scheduler_ring is true."`
}

func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	f.IntVar(&cfg.MaxOutstandingPerTenant, "query-scheduler.max-outstanding-requests-per-tenant", 32000, "Maximum number of outstanding requests per tenant per query-scheduler. In-flight requests above this limit will fail with HTTP response status code 429.")
	f.IntVar(&cfg.MaxQueueHierarchyLevels, "query-scheduler.max-queue-hierarchy-levels", 3, "Maximum number of levels of nesting of hierarchical queues. 0 means that hierarchical queues are disabled.")
	f.DurationVar(&cfg.QuerierForgetDelay, "query-scheduler.querier-forget-delay", 0, "If a querier disconnects without sending notification about graceful shutdown, the query-scheduler will keep the querier in the tenant's shard until the forget delay has passed. This feature is useful to reduce the blast radius when shuffle-sharding is enabled.")
	cfg.GRPCClientConfig.RegisterFlagsWithPrefix("query-scheduler.grpc-client-config", f)
	f.BoolVar(&cfg.UseSchedulerRing, "query-scheduler.use-scheduler-ring", false, "Set to true to have the query schedulers create and place themselves in a ring. If no frontend_address or scheduler_address are present anywhere else in the configuration, Loki will toggle this value to true.")

	// Ring
	skipFlags := []string{
		"query-scheduler.ring.num-tokens",
		"query-scheduler.ring.replication-factor",
	}
	cfg.SchedulerRing.RegisterFlagsWithPrefix("query-scheduler.", "collectors/", f, skipFlags...)
	f.IntVar(&cfg.SchedulerRing.NumTokens, "query-scheduler.ring.num-tokens", NumTokens, fmt.Sprintf("IGNORED: Num tokens is fixed to %d", NumTokens))
	f.IntVar(&cfg.SchedulerRing.ReplicationFactor, "query-scheduler.ring.replication-factor", ReplicationFactor, fmt.Sprintf("IGNORED: Replication factor is fixed to %d", ReplicationFactor))
}

func (cfg *Config) Validate() error {
	if cfg.SchedulerRing.NumTokens != NumTokens {
		return errors.New("Num tokens must not be changed as it will not take effect")
	}
	if cfg.SchedulerRing.ReplicationFactor != ReplicationFactor {
		return errors.New("Replication factor must not be changed as it will not take effect")
	}
	return nil
}

// NewScheduler creates a new Scheduler.
func NewScheduler(cfg Config, schedulerLimits Limits, log log.Logger, ringManager *lokiring.RingManager, registerer prometheus.Registerer, metricsNamespace string) (*Scheduler, error) {
	if cfg.UseSchedulerRing {
		if ringManager == nil {
			return nil, errors.New("ring manager can't be empty when use_scheduler_ring is true")
		} else if ringManager.Mode != lokiring.ServerMode {
			return nil, errors.New("ring manager must be initialized in ServerMode for query schedulers")
		}
	}

	queueMetrics := queue.NewMetrics(registerer, metricsNamespace, "query_scheduler")
	s := &Scheduler{
		cfg:    cfg,
		log:    log,
		limits: schedulerLimits,

		pendingRequests:    map[requestKey]*schedulerRequest{},
		connectedFrontends: map[string]*connectedFrontend{},
		queueMetrics:       queueMetrics,
		ringManager:        ringManager,
		requestQueue:       queue.NewRequestQueue(cfg.MaxOutstandingPerTenant, cfg.QuerierForgetDelay, limits.NewQueueLimits(schedulerLimits), queueMetrics),
	}

	s.queueDuration = promauto.With(registerer).NewHistogram(prometheus.HistogramOpts{
		Namespace: metricsNamespace,
		Name:      "query_scheduler_queue_duration_seconds",
		Help:      "Time spend by requests in queue before getting picked up by a querier.",
		Buckets:   prometheus.DefBuckets,
	})
	s.connectedQuerierClients = promauto.With(registerer).NewGaugeFunc(prometheus.GaugeOpts{
		Namespace: metricsNamespace,
		Name:      "query_scheduler_connected_querier_clients",
		Help:      "Number of querier worker clients currently connected to the query-scheduler.",
	}, s.requestQueue.GetConnectedConsumersMetric)
	s.connectedFrontendClients = promauto.With(registerer).NewGaugeFunc(prometheus.GaugeOpts{
		Namespace: metricsNamespace,
		Name:      "query_scheduler_connected_frontend_clients",
		Help:      "Number of query-frontend worker clients currently connected to the query-scheduler.",
	}, s.getConnectedFrontendClientsMetric)
	s.schedulerRunning = promauto.With(registerer).NewGauge(prometheus.GaugeOpts{
		Namespace: metricsNamespace,
		Name:      "query_scheduler_running",
		Help:      "Value will be 1 if the scheduler is in the ReplicationSet and actively receiving/processing requests",
	})
	s.inflightRequests = promauto.With(registerer).NewSummary(prometheus.SummaryOpts{
		Namespace:  metricsNamespace,
		Name:       "query_scheduler_inflight_requests",
		Help:       "Number of inflight requests (either queued or processing) sampled at a regular interval. Quantile buckets keep track of inflight requests over the last 60s.",
		Objectives: map[float64]float64{0.5: 0.05, 0.75: 0.02, 0.8: 0.02, 0.9: 0.01, 0.95: 0.01, 0.99: 0.001},
		MaxAge:     time.Minute,
		AgeBuckets: 6,
	})

	s.activeUsers = util.NewActiveUsersCleanupWithDefaultValues(s.cleanupMetricsForInactiveUser)

	svcs := []services.Service{s.requestQueue, s.activeUsers}

	if cfg.UseSchedulerRing {
		s.shouldRun.Store(false)
	} else {
		// Always run if no scheduler ring is being used.
		s.shouldRun.Store(true)
	}

	var err error
	s.subservices, err = services.NewManager(svcs...)
	if err != nil {
		return nil, err
	}
	s.subservicesWatcher = services.NewFailureWatcher()
	s.subservicesWatcher.WatchManager(s.subservices)

	s.Service = services.NewBasicService(s.starting, s.running, s.stopping)
	return s, nil
}

type Limits limits.Limits

type schedulerRequest struct {
	frontendAddress string
	tenantID        string
	queryID         uint64
	request         *httpgrpc.HTTPRequest
	queryRequest    *queryrange.QueryRequest
	statsEnabled    bool

	queueTime time.Time

	ctx       context.Context
	ctxCancel context.CancelFunc
	queueSpan opentracing.Span

	// This is only used for testing.
	parentSpanContext opentracing.SpanContext
}

// FrontendLoop handles connection from frontend.
func (s *Scheduler) FrontendLoop(frontend schedulerpb.SchedulerForFrontend_FrontendLoopServer) error {
	frontendAddress, frontendCtx, err := s.frontendConnected(frontend)
	if err != nil {
		return err
	}
	defer s.frontendDisconnected(frontendAddress)

	// Response to INIT. If scheduler is not running, we skip for-loop, send SHUTTING_DOWN and exit this method.
	if s.State() == services.Running && s.shouldRun.Load() {
		if err := frontend.Send(&schedulerpb.SchedulerToFrontend{Status: schedulerpb.OK}); err != nil {
			return err
		}
	}

	// We stop accepting new queries in Stopping state. By returning quickly, we disconnect frontends, which in turns
	// cancels all their queries.
	for s.State() == services.Running {
		msg, err := frontend.Recv()
		if err != nil {
			// No need to report this as error, it is expected when query-frontend performs SendClose() (as frontendSchedulerWorker does).
			if err == io.EOF {
				return nil
			}
			return err
		}

		if s.State() != services.Running {
			break // break out of the loop, and send SHUTTING_DOWN message.
		}

		var resp *schedulerpb.SchedulerToFrontend

		switch msg.GetType() {
		case schedulerpb.ENQUEUE:
			err = s.enqueueRequest(frontendCtx, frontendAddress, msg)
			switch err {
			case nil:
				resp = &schedulerpb.SchedulerToFrontend{Status: schedulerpb.OK}
			case queue.ErrTooManyRequests:
				resp = &schedulerpb.SchedulerToFrontend{Status: schedulerpb.TOO_MANY_REQUESTS_PER_TENANT}
			default:
				resp = &schedulerpb.SchedulerToFrontend{Status: schedulerpb.ERROR, Error: err.Error()}
			}

		case schedulerpb.CANCEL:
			s.cancelRequestAndRemoveFromPending(frontendAddress, msg.QueryID)
			resp = &schedulerpb.SchedulerToFrontend{Status: schedulerpb.OK}

		default:
			level.Error(s.log).Log("msg", "unknown request type from frontend", "addr", frontendAddress, "type", msg.GetType())
			return errors.New("unknown request type")
		}

		err = frontend.Send(resp)
		// Failure to send response results in ending this connection.
		if err != nil {
			return err
		}
	}

	// Report shutdown back to frontend, so that it can retry with different scheduler. Also stop the frontend loop.
	return frontend.Send(&schedulerpb.SchedulerToFrontend{Status: schedulerpb.SHUTTING_DOWN})
}

func (s *Scheduler) frontendConnected(frontend schedulerpb.SchedulerForFrontend_FrontendLoopServer) (string, context.Context, error) {
	msg, err := frontend.Recv()
	if err != nil {
		return "", nil, err
	}
	if msg.Type != schedulerpb.INIT || msg.FrontendAddress == "" {
		return "", nil, errors.New("no frontend address")
	}

	level.Debug(s.log).Log("msg", "frontend connected", "address", msg.FrontendAddress)

	s.connectedFrontendsMu.Lock()
	defer s.connectedFrontendsMu.Unlock()

	cf := s.connectedFrontends[msg.FrontendAddress]
	if cf == nil {
		cf = &connectedFrontend{
			connections: 0,
			frontend:    frontend,
		}
		cf.ctx, cf.cancel = context.WithCancel(context.Background())
		s.connectedFrontends[msg.FrontendAddress] = cf
	}

	cf.connections++
	return msg.FrontendAddress, cf.ctx, nil
}

func (s *Scheduler) frontendDisconnected(frontendAddress string) {
	s.connectedFrontendsMu.Lock()
	defer s.connectedFrontendsMu.Unlock()

	level.Debug(s.log).Log("msg", "frontend disconnected", "address", frontendAddress)

	cf := s.connectedFrontends[frontendAddress]
	cf.connections--
	if cf.connections == 0 {
		delete(s.connectedFrontends, frontendAddress)
		cf.cancel()
	}
}

func (s *Scheduler) enqueueRequest(frontendContext context.Context, frontendAddr string, msg *schedulerpb.FrontendToScheduler) error {
	// Create new context for this request, to support cancellation.
	ctx, cancel := context.WithCancel(frontendContext)
	shouldCancel := true
	defer func() {
		if shouldCancel {
			cancel()
		}
	}()

	// Extract tracing information from headers in HTTP request. FrontendContext doesn't have the correct tracing
	// information, since that is a long-running request.
	tracer := opentracing.GlobalTracer()
	parentSpanContext, err := lokigrpc.GetParentSpanForRequest(tracer, msg)
	if err != nil && err != opentracing.ErrSpanContextNotFound {
		return err
	}

	req := &schedulerRequest{
		frontendAddress: frontendAddr,
		tenantID:        msg.UserID,
		queryID:         msg.QueryID,
		request:         msg.GetHttpRequest(),
		queryRequest:    msg.GetQueryRequest(),
		statsEnabled:    msg.StatsEnabled,
	}

	now := time.Now()

	req.parentSpanContext = parentSpanContext
	req.queueSpan, req.ctx = opentracing.StartSpanFromContextWithTracer(ctx, tracer, "queued", opentracing.ChildOf(parentSpanContext))
	req.queueTime = now
	req.ctxCancel = cancel

	var queuePath []string
	if s.cfg.MaxQueueHierarchyLevels > 0 {
		queuePath = msg.QueuePath
		if len(queuePath) > s.cfg.MaxQueueHierarchyLevels {
			msg := fmt.Sprintf(
				"The header %s with value '%s' would result in a sub-queue which is "+
					"nested %d levels deep, however only %d levels are allowed based on the "+
					"configuration setting -query-scheduler.max-queue-hierarchy-levels",
				lokihttpreq.LokiActorPathHeader,
				strings.Join(queuePath, lokihttpreq.LokiActorPathDelimiter),
				len(queuePath),
				s.cfg.MaxQueueHierarchyLevels,
			)
			return fmt.Errorf("desired queue level exceeds maxium depth of queue hierarchy: %s", msg)
		}
	}

	s.activeUsers.UpdateUserTimestamp(req.tenantID, now)
	return s.requestQueue.Enqueue(req.tenantID, queuePath, req, func() {
		shouldCancel = false

		s.pendingRequestsMu.Lock()
		defer s.pendingRequestsMu.Unlock()
		s.pendingRequests[requestKey{frontendAddr: frontendAddr, queryID: msg.QueryID}] = req
	})
}

// This method doesn't do removal from the queue.
func (s *Scheduler) cancelRequestAndRemoveFromPending(frontendAddr string, queryID uint64) {
	s.pendingRequestsMu.Lock()
	defer s.pendingRequestsMu.Unlock()

	key := requestKey{frontendAddr: frontendAddr, queryID: queryID}
	req := s.pendingRequests[key]
	if req != nil {
		req.ctxCancel()
	}
	delete(s.pendingRequests, key)
}

// QuerierLoop is started by querier to receive queries from scheduler.
func (s *Scheduler) QuerierLoop(querier schedulerpb.SchedulerForQuerier_QuerierLoopServer) error {
	resp, err := querier.Recv()
	if err != nil {
		return err
	}

	querierID := resp.GetQuerierID()
	level.Debug(s.log).Log("msg", "querier connected", "querier", querierID)

	s.requestQueue.RegisterConsumerConnection(querierID)
	defer s.requestQueue.UnregisterConsumerConnection(querierID)

	lastIndex := queue.StartIndex

	// In stopping state scheduler is not accepting new queries, but still dispatching queries in the queues.
	for s.isRunningOrStopping() {
		req, idx, err := s.requestQueue.Dequeue(querier.Context(), lastIndex, querierID)
		if err != nil {
			return err
		}
		lastIndex = idx

		// This really should not happen, but log additional information before the scheduler panics.
		if req == nil {
			level.Error(s.log).Log("msg", "dequeue() call resulted in nil response", "querier", querierID)
		}
		r := req.(*schedulerRequest)

		reqQueueTime := time.Since(r.queueTime)
		s.queueDuration.Observe(reqQueueTime.Seconds())
		r.queueSpan.Finish()

		// Add HTTP header to the request containing the query queue time
		if r.request != nil {
			r.request.Headers = append(r.request.Headers, &httpgrpc.Header{
				Key:    textproto.CanonicalMIMEHeaderKey(string(lokihttpreq.QueryQueueTimeHTTPHeader)),
				Values: []string{reqQueueTime.String()},
			})
		}
		if r.queryRequest != nil {
			r.queryRequest.Metadata[string(lokihttpreq.QueryQueueTimeHTTPHeader)] = reqQueueTime.String()
		}

		/*
		  We want to dequeue the next unexpired request from the chosen tenant queue.
		  The chance of choosing a particular tenant for dequeueing is (1/active_tenants).
		  This is problematic under load, especially with other middleware enabled such as
		  querier.split-by-interval, where one request may fan out into many.
		  If expired requests aren't exhausted before checking another tenant, it would take
		  n_active_tenants * n_expired_requests_at_front_of_queue requests being processed
		  before an active request was handled for the tenant in question.
		  If this tenant meanwhile continued to queue requests,
		  it's possible that it's own queue would perpetually contain only expired requests.
		*/

		if r.ctx.Err() != nil {
			// Remove from pending requests.
			s.cancelRequestAndRemoveFromPending(r.frontendAddress, r.queryID)

			lastIndex = lastIndex.ReuseLastIndex()
			continue
		}

		if err := s.forwardRequestToQuerier(querier, r); err != nil {
			return err
		}
	}

	return errSchedulerIsNotRunning
}

func (s *Scheduler) NotifyQuerierShutdown(_ context.Context, req *schedulerpb.NotifyQuerierShutdownRequest) (*schedulerpb.NotifyQuerierShutdownResponse, error) {
	level.Debug(s.log).Log("msg", "received shutdown notification from querier", "querier", req.GetQuerierID())
	s.requestQueue.NotifyConsumerShutdown(req.GetQuerierID())

	return &schedulerpb.NotifyQuerierShutdownResponse{}, nil
}

func (s *Scheduler) forwardRequestToQuerier(querier schedulerpb.SchedulerForQuerier_QuerierLoopServer, req *schedulerRequest) error {
	// Make sure to cancel request at the end to cleanup resources.
	defer s.cancelRequestAndRemoveFromPending(req.frontendAddress, req.queryID)

	// Handle the stream sending & receiving on a goroutine so we can
	// monitoring the contexts in a select and cancel things appropriately.
	errCh := make(chan error, 1)
	go func() {
		msg := &schedulerpb.SchedulerToQuerier{
			UserID:          req.tenantID,
			QueryID:         req.queryID,
			FrontendAddress: req.frontendAddress,
			Request: &schedulerpb.SchedulerToQuerier_HttpRequest{
				HttpRequest: req.request,
			},
			StatsEnabled: req.statsEnabled,
		}
		// Override HttpRequest if new request type is set.
		if req.queryRequest != nil {
			msg.Request = &schedulerpb.SchedulerToQuerier_QueryRequest{
				QueryRequest: req.queryRequest,
			}
		}
		err := querier.Send(msg)
		if err != nil {
			errCh <- err
			return
		}

		_, err = querier.Recv()
		errCh <- err
	}()

	select {
	case <-req.ctx.Done():
		// If the upstream request is cancelled (eg. frontend issued CANCEL or closed connection),
		// we need to cancel the downstream req. Only way we can do that is to close the stream (by returning error here).
		// Querier is expecting this semantics.
		return req.ctx.Err()

	case err := <-errCh:
		// Is there was an error handling this request due to network IO,
		// then error out this upstream request _and_ stream.

		if err != nil {
			s.forwardErrorToFrontend(req.ctx, req, err)
		}
		return err
	}
}

func (s *Scheduler) forwardErrorToFrontend(ctx context.Context, req *schedulerRequest, requestErr error) {
	opts, err := s.cfg.GRPCClientConfig.DialOption([]grpc.UnaryClientInterceptor{
		otgrpc.OpenTracingClientInterceptor(opentracing.GlobalTracer()),
		middleware.ClientUserHeaderInterceptor,
	},
		nil)
	if err != nil {
		level.Warn(s.log).Log("msg", "failed to create gRPC options for the connection to frontend to report error", "frontend", req.frontendAddress, "err", err, "requestErr", requestErr)
		return
	}

	// nolint:staticcheck // grpc.DialContext() has been deprecated; we'll address it before upgrading to gRPC 2.
	conn, err := grpc.DialContext(ctx, req.frontendAddress, opts...)
	if err != nil {
		level.Warn(s.log).Log("msg", "failed to create gRPC connection to frontend to report error", "frontend", req.frontendAddress, "err", err, "requestErr", requestErr)
		return
	}

	defer func() {
		_ = conn.Close()
	}()

	client := frontendv2pb.NewFrontendForQuerierClient(conn)

	userCtx := user.InjectOrgID(ctx, req.tenantID)
	_, err = client.QueryResult(userCtx, &frontendv2pb.QueryResultRequest{
		QueryID: req.queryID,
		Response: &frontendv2pb.QueryResultRequest_HttpResponse{
			HttpResponse: &httpgrpc.HTTPResponse{
				Code: http.StatusInternalServerError,
				Body: []byte(requestErr.Error()),
			},
		},
	})

	if err != nil {
		level.Warn(s.log).Log("msg", "failed to forward error to frontend", "frontend", req.frontendAddress, "err", err, "requestErr", requestErr)
		return
	}
}

func (s *Scheduler) isRunningOrStopping() bool {
	st := s.State()
	return st == services.Running || st == services.Stopping
}

func (s *Scheduler) starting(ctx context.Context) (err error) {
	defer func() {
		if err == nil || s.subservices == nil {
			return
		}

		if stopErr := services.StopManagerAndAwaitStopped(context.Background(), s.subservices); stopErr != nil {
			level.Error(s.log).Log("msg", "failed to gracefully stop scheduler dependencies", "err", stopErr)
		}
	}()

	if err := services.StartManagerAndAwaitHealthy(ctx, s.subservices); err != nil {
		return errors.Wrap(err, "unable to start scheduler subservices")
	}

	return nil
}

func (s *Scheduler) running(ctx context.Context) error {
	// We observe inflight requests frequently and at regular intervals, to have a good
	// approximation of max inflight requests over percentiles of time. We also do it with
	// a ticker so that we keep tracking it even if we have no new queries but stuck inflight
	// requests (eg. queriers are all crashing).
	inflightRequestsTicker := time.NewTicker(250 * time.Millisecond)
	defer inflightRequestsTicker.Stop()

	ringCheckTicker := time.NewTicker(lokiring.RingCheckPeriod)
	defer ringCheckTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-s.subservicesWatcher.Chan():
			return errors.Wrap(err, "scheduler subservice failed")
		case <-ringCheckTicker.C:
			if !s.cfg.UseSchedulerRing {
				continue
			}
			isInSet, err := lokiring.IsInReplicationSet(s.ringManager.Ring, util.RingKeyOfLeader, s.ringManager.RingLifecycler.GetInstanceAddr())
			if err != nil {
				level.Error(s.log).Log("msg", "failed to query the ring to see if scheduler instance is in ReplicatonSet, will try again", "err", err)
				continue
			}
			s.setRunState(isInSet)
		case <-inflightRequestsTicker.C:
			s.pendingRequestsMu.Lock()
			inflight := len(s.pendingRequests)
			s.pendingRequestsMu.Unlock()

			s.inflightRequests.Observe(float64(inflight))
		}
	}
}

func (s *Scheduler) setRunState(isInSet bool) {
	if isInSet {
		if s.shouldRun.CompareAndSwap(false, true) {
			// Value was swapped, meaning this was a state change from stopped to running.
			level.Info(s.log).Log("msg", "this scheduler is in the ReplicationSet, will now accept requests.")
			s.schedulerRunning.Set(1)
		}
	} else {
		if s.shouldRun.CompareAndSwap(true, false) {
			// Value was swapped, meaning this was a state change from running to stopped,
			// we need to send shutdown to all the connected frontends.
			level.Info(s.log).Log("msg", "this scheduler is no longer in the ReplicationSet, disconnecting frontends, canceling queries and no longer accepting requests.")

			// Send a shutdown message to the connected frontends, there is no way to break the blocking Recv() in the FrontendLoop()
			// so we send a message to the frontend telling them we are shutting down so they will disconnect.
			// When FrontendLoop() exits for the connected querier all the inflight queries and queued queries will be canceled.
			s.connectedFrontendsMu.Lock()
			defer s.connectedFrontendsMu.Unlock()
			for _, f := range s.connectedFrontends {
				// We ignore any errors here because there isn't really an action to take and because
				// the frontends are also discovering the ring changes and may already be disconnected
				// or have disconnected.
				_ = f.frontend.Send(&schedulerpb.SchedulerToFrontend{Status: schedulerpb.SHUTTING_DOWN})
			}
			s.schedulerRunning.Set(0)
		}
	}
}

// Close the Scheduler.
func (s *Scheduler) stopping(_ error) error {
	// This will also stop the requests queue, which stop accepting new requests and errors out any pending requests.
	return services.StopManagerAndAwaitStopped(context.Background(), s.subservices)
}

func (s *Scheduler) cleanupMetricsForInactiveUser(user string) {
	s.queueMetrics.Cleanup(user)
}

func (s *Scheduler) getConnectedFrontendClientsMetric() float64 {
	s.connectedFrontendsMu.Lock()
	defer s.connectedFrontendsMu.Unlock()

	count := 0
	for _, workers := range s.connectedFrontends {
		count += workers.connections
	}

	return float64(count)
}

// SafeReadRing does a nil check on the Scheduler before attempting to return it's ring
// this is necessary as many callers of this function will only have a valid Scheduler
// reference if the QueryScheduler target has been specified, which is not guaranteed
func SafeReadRing(cfg Config, rm *lokiring.RingManager) ring.ReadRing {
	if rm == nil || rm.Ring == nil || !cfg.UseSchedulerRing {
		return nil
	}

	return rm.Ring
}
