package scheduler

import (
	"context"
	"flag"
	"io"
	"net/http"
	"net/textproto"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/grpcclient"
	"github.com/grafana/dskit/kv"
	"github.com/grafana/dskit/ring"
	"github.com/grafana/dskit/services"
	otgrpc "github.com/opentracing-contrib/go-grpc"
	"github.com/opentracing/opentracing-go"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/weaveworks/common/httpgrpc"
	"github.com/weaveworks/common/middleware"
	"github.com/weaveworks/common/user"
	"go.uber.org/atomic"
	"google.golang.org/grpc"

	"github.com/grafana/dskit/tenant"

	"github.com/grafana/loki/pkg/lokifrontend/frontend/v2/frontendv2pb"
	"github.com/grafana/loki/pkg/scheduler/queue"
	"github.com/grafana/loki/pkg/scheduler/schedulerpb"
	"github.com/grafana/loki/pkg/util"
	lokiutil "github.com/grafana/loki/pkg/util"
	lokigrpc "github.com/grafana/loki/pkg/util/httpgrpc"
	lokihttpreq "github.com/grafana/loki/pkg/util/httpreq"
	util_log "github.com/grafana/loki/pkg/util/log"
	"github.com/grafana/loki/pkg/util/validation"
)

var errSchedulerIsNotRunning = errors.New("scheduler is not running")

const (
	// ringAutoForgetUnhealthyPeriods is how many consecutive timeout periods an unhealthy instance
	// in the ring will be automatically removed.
	ringAutoForgetUnhealthyPeriods = 10

	// ringKey is the key under which we store the store gateways ring in the KVStore.
	ringKey = "scheduler"

	// ringNameForServer is the name of the ring used by the compactor server.
	ringNameForServer = "scheduler"

	// ringReplicationFactor should be 2 because we want 2 schedulers.
	ringReplicationFactor = 2

	// ringNumTokens sets our single token in the ring,
	// we only need to insert 1 token to be used for leader election purposes.
	ringNumTokens = 1

	// ringCheckPeriod is how often we check the ring to see if this instance is still in
	// the replicaset of instances to act as schedulers.
	ringCheckPeriod = 3 * time.Second
)

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

	// Metrics.
	queueLength              *prometheus.GaugeVec
	discardedRequests        *prometheus.CounterVec
	connectedQuerierClients  prometheus.GaugeFunc
	connectedFrontendClients prometheus.GaugeFunc
	queueDuration            prometheus.Histogram
	schedulerRunning         prometheus.Gauge
	inflightRequests         prometheus.Summary

	// Ring used for finding schedulers
	ringLifecycler *ring.BasicLifecycler
	ring           *ring.Ring

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
	QuerierForgetDelay      time.Duration     `yaml:"querier_forget_delay"`
	GRPCClientConfig        grpcclient.Config `yaml:"grpc_client_config" doc:"description=This configures the gRPC client used to report errors back to the query-frontend."`
	// Schedulers ring
	UseSchedulerRing bool                `yaml:"use_scheduler_ring"`
	SchedulerRing    lokiutil.RingConfig `yaml:"scheduler_ring,omitempty" doc:"description=The hash ring configuration. This option is required only if use_scheduler_ring is true."`
}

func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	f.IntVar(&cfg.MaxOutstandingPerTenant, "query-scheduler.max-outstanding-requests-per-tenant", 100, "Maximum number of outstanding requests per tenant per query-scheduler. In-flight requests above this limit will fail with HTTP response status code 429.")
	f.DurationVar(&cfg.QuerierForgetDelay, "query-scheduler.querier-forget-delay", 0, "If a querier disconnects without sending notification about graceful shutdown, the query-scheduler will keep the querier in the tenant's shard until the forget delay has passed. This feature is useful to reduce the blast radius when shuffle-sharding is enabled.")
	cfg.GRPCClientConfig.RegisterFlagsWithPrefix("query-scheduler.grpc-client-config", f)
	f.BoolVar(&cfg.UseSchedulerRing, "query-scheduler.use-scheduler-ring", false, "Set to true to have the query schedulers create and place themselves in a ring. If no frontend_address or scheduler_address are present anywhere else in the configuration, Loki will toggle this value to true.")
	cfg.SchedulerRing.RegisterFlagsWithPrefix("query-scheduler.", "collectors/", f)
}

// NewScheduler creates a new Scheduler.
func NewScheduler(cfg Config, limits Limits, log log.Logger, registerer prometheus.Registerer) (*Scheduler, error) {
	s := &Scheduler{
		cfg:    cfg,
		log:    log,
		limits: limits,

		pendingRequests:    map[requestKey]*schedulerRequest{},
		connectedFrontends: map[string]*connectedFrontend{},
	}

	s.queueLength = promauto.With(registerer).NewGaugeVec(prometheus.GaugeOpts{
		Name: "cortex_query_scheduler_queue_length",
		Help: "Number of queries in the queue.",
	}, []string{"user"})

	s.discardedRequests = promauto.With(registerer).NewCounterVec(prometheus.CounterOpts{
		Name: "cortex_query_scheduler_discarded_requests_total",
		Help: "Total number of query requests discarded.",
	}, []string{"user"})
	s.requestQueue = queue.NewRequestQueue(cfg.MaxOutstandingPerTenant, cfg.QuerierForgetDelay, s.queueLength, s.discardedRequests)

	s.queueDuration = promauto.With(registerer).NewHistogram(prometheus.HistogramOpts{
		Name:    "cortex_query_scheduler_queue_duration_seconds",
		Help:    "Time spend by requests in queue before getting picked up by a querier.",
		Buckets: prometheus.DefBuckets,
	})
	s.connectedQuerierClients = promauto.With(registerer).NewGaugeFunc(prometheus.GaugeOpts{
		Name: "cortex_query_scheduler_connected_querier_clients",
		Help: "Number of querier worker clients currently connected to the query-scheduler.",
	}, s.requestQueue.GetConnectedQuerierWorkersMetric)
	s.connectedFrontendClients = promauto.With(registerer).NewGaugeFunc(prometheus.GaugeOpts{
		Name: "cortex_query_scheduler_connected_frontend_clients",
		Help: "Number of query-frontend worker clients currently connected to the query-scheduler.",
	}, s.getConnectedFrontendClientsMetric)
	s.schedulerRunning = promauto.With(registerer).NewGauge(prometheus.GaugeOpts{
		Name: "cortex_query_scheduler_running",
		Help: "Value will be 1 if the scheduler is in the ReplicationSet and actively receiving/processing requests",
	})
	s.inflightRequests = promauto.With(registerer).NewSummary(prometheus.SummaryOpts{
		Name:       "cortex_query_scheduler_inflight_requests",
		Help:       "Number of inflight requests (either queued or processing) sampled at a regular interval. Quantile buckets keep track of inflight requests over the last 60s.",
		Objectives: map[float64]float64{0.5: 0.05, 0.75: 0.02, 0.8: 0.02, 0.9: 0.01, 0.95: 0.01, 0.99: 0.001},
		MaxAge:     time.Minute,
		AgeBuckets: 6,
	})

	s.activeUsers = util.NewActiveUsersCleanupWithDefaultValues(s.cleanupMetricsForInactiveUser)

	svcs := []services.Service{s.requestQueue, s.activeUsers}

	if cfg.UseSchedulerRing {
		s.shouldRun.Store(false)
		ringStore, err := kv.NewClient(
			cfg.SchedulerRing.KVStore,
			ring.GetCodec(),
			kv.RegistererWithKVName(prometheus.WrapRegistererWithPrefix("loki_", registerer), "scheduler"),
			log,
		)
		if err != nil {
			return nil, errors.Wrap(err, "create KV store client")
		}
		lifecyclerCfg, err := cfg.SchedulerRing.ToLifecyclerConfig(ringNumTokens, log)
		if err != nil {
			return nil, errors.Wrap(err, "invalid ring lifecycler config")
		}

		// Define lifecycler delegates in reverse order (last to be called defined first because they're
		// chained via "next delegate").
		delegate := ring.BasicLifecyclerDelegate(s)
		delegate = ring.NewLeaveOnStoppingDelegate(delegate, log)
		delegate = ring.NewTokensPersistencyDelegate(cfg.SchedulerRing.TokensFilePath, ring.JOINING, delegate, log)
		delegate = ring.NewAutoForgetDelegate(ringAutoForgetUnhealthyPeriods*cfg.SchedulerRing.HeartbeatTimeout, delegate, log)

		s.ringLifecycler, err = ring.NewBasicLifecycler(lifecyclerCfg, ringNameForServer, ringKey, ringStore, delegate, log, registerer)
		if err != nil {
			return nil, errors.Wrap(err, "create ring lifecycler")
		}

		ringCfg := cfg.SchedulerRing.ToRingConfig(ringReplicationFactor)
		s.ring, err = ring.NewWithStoreClientAndStrategy(ringCfg, ringNameForServer, ringKey, ringStore, ring.NewIgnoreUnhealthyInstancesReplicationStrategy(), prometheus.WrapRegistererWithPrefix("cortex_", registerer), util_log.Logger)
		if err != nil {
			return nil, errors.Wrap(err, "create ring client")
		}

		svcs = append(svcs, s.ringLifecycler, s.ring)
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

// Limits needed for the Query Scheduler - interface used for decoupling.
type Limits interface {
	// MaxQueriersPerUser returns max queriers to use per tenant, or 0 if shuffle sharding is disabled.
	MaxQueriersPerUser(user string) int
}

type schedulerRequest struct {
	frontendAddress string
	userID          string
	queryID         uint64
	request         *httpgrpc.HTTPRequest
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
	parentSpanContext, err := lokigrpc.GetParentSpanForRequest(tracer, msg.HttpRequest)
	if err != nil {
		return err
	}

	userID := msg.GetUserID()

	req := &schedulerRequest{
		frontendAddress: frontendAddr,
		userID:          msg.UserID,
		queryID:         msg.QueryID,
		request:         msg.HttpRequest,
		statsEnabled:    msg.StatsEnabled,
	}

	now := time.Now()

	req.parentSpanContext = parentSpanContext
	req.queueSpan, req.ctx = opentracing.StartSpanFromContextWithTracer(ctx, tracer, "queued", opentracing.ChildOf(parentSpanContext))
	req.queueTime = now
	req.ctxCancel = cancel

	// aggregate the max queriers limit in the case of a multi tenant query
	tenantIDs, err := tenant.TenantIDsFromOrgID(userID)
	if err != nil {
		return err
	}
	maxQueriers := validation.SmallestPositiveNonZeroIntPerTenant(tenantIDs, s.limits.MaxQueriersPerUser)

	s.activeUsers.UpdateUserTimestamp(userID, now)
	return s.requestQueue.EnqueueRequest(userID, req, maxQueriers, func() {
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

	s.requestQueue.RegisterQuerierConnection(querierID)
	defer s.requestQueue.UnregisterQuerierConnection(querierID)

	lastUserIndex := queue.FirstUser

	// In stopping state scheduler is not accepting new queries, but still dispatching queries in the queues.
	for s.isRunningOrStopping() {
		req, idx, err := s.requestQueue.GetNextRequestForQuerier(querier.Context(), lastUserIndex, querierID)
		if err != nil {
			return err
		}
		lastUserIndex = idx

		r := req.(*schedulerRequest)

		reqQueueTime := time.Since(r.queueTime)
		s.queueDuration.Observe(reqQueueTime.Seconds())
		r.queueSpan.Finish()

		// Add HTTP header to the request containing the query queue time
		r.request.Headers = append(r.request.Headers, &httpgrpc.Header{
			Key:    textproto.CanonicalMIMEHeaderKey(string(lokihttpreq.QueryQueueTimeHTTPHeader)),
			Values: []string{reqQueueTime.String()},
		})

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

			lastUserIndex = lastUserIndex.ReuseLastUser()
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
	s.requestQueue.NotifyQuerierShutdown(req.GetQuerierID())

	return &schedulerpb.NotifyQuerierShutdownResponse{}, nil
}

func (s *Scheduler) forwardRequestToQuerier(querier schedulerpb.SchedulerForQuerier_QuerierLoopServer, req *schedulerRequest) error {
	// Make sure to cancel request at the end to cleanup resources.
	defer s.cancelRequestAndRemoveFromPending(req.frontendAddress, req.queryID)

	// Handle the stream sending & receiving on a goroutine so we can
	// monitoring the contexts in a select and cancel things appropriately.
	errCh := make(chan error, 1)
	go func() {
		err := querier.Send(&schedulerpb.SchedulerToQuerier{
			UserID:          req.userID,
			QueryID:         req.queryID,
			FrontendAddress: req.frontendAddress,
			HttpRequest:     req.request,
			StatsEnabled:    req.statsEnabled,
		})
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

	conn, err := grpc.DialContext(ctx, req.frontendAddress, opts...)
	if err != nil {
		level.Warn(s.log).Log("msg", "failed to create gRPC connection to frontend to report error", "frontend", req.frontendAddress, "err", err, "requestErr", requestErr)
		return
	}

	defer func() {
		_ = conn.Close()
	}()

	client := frontendv2pb.NewFrontendForQuerierClient(conn)

	userCtx := user.InjectOrgID(ctx, req.userID)
	_, err = client.QueryResult(userCtx, &frontendv2pb.QueryResultRequest{
		QueryID: req.queryID,
		HttpResponse: &httpgrpc.HTTPResponse{
			Code: http.StatusInternalServerError,
			Body: []byte(requestErr.Error()),
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
	// In case this function will return error we want to unregister the instance
	// from the ring. We do it ensuring dependencies are gracefully stopped if they
	// were already started.
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

	if s.cfg.UseSchedulerRing {
		// The BasicLifecycler does not automatically move state to ACTIVE such that any additional work that
		// someone wants to do can be done before becoming ACTIVE. For the query scheduler we don't currently
		// have any additional work so we can become ACTIVE right away.

		// Wait until the ring client detected this instance in the JOINING state to
		// make sure that when we'll run the initial sync we already know  the tokens
		// assigned to this instance.
		level.Info(s.log).Log("msg", "waiting until scheduler is JOINING in the ring")
		if err := ring.WaitInstanceState(ctx, s.ring, s.ringLifecycler.GetInstanceID(), ring.JOINING); err != nil {
			return err
		}
		level.Info(s.log).Log("msg", "scheduler is JOINING in the ring")

		// Change ring state to ACTIVE
		if err = s.ringLifecycler.ChangeState(ctx, ring.ACTIVE); err != nil {
			return errors.Wrapf(err, "switch instance to %s in the ring", ring.ACTIVE)
		}

		// Wait until the ring client detected this instance in the ACTIVE state to
		// make sure that when we'll run the loop it won't be detected as a ring
		// topology change.
		level.Info(s.log).Log("msg", "waiting until scheduler is ACTIVE in the ring")
		if err := ring.WaitInstanceState(ctx, s.ring, s.ringLifecycler.GetInstanceID(), ring.ACTIVE); err != nil {
			return err
		}
		level.Info(s.log).Log("msg", "scheduler is ACTIVE in the ring")
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

	ringCheckTicker := time.NewTicker(ringCheckPeriod)
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
			isInSet, err := lokiutil.IsInReplicationSet(s.ring, lokiutil.RingKeyOfLeader, s.ringLifecycler.GetInstanceAddr())
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
	s.queueLength.DeleteLabelValues(user)
	s.discardedRequests.DeleteLabelValues(user)
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
func SafeReadRing(s *Scheduler) ring.ReadRing {
	if s == nil || s.ring == nil || !s.cfg.UseSchedulerRing {
		return nil
	}

	return s.ring
}

func (s *Scheduler) OnRingInstanceRegister(_ *ring.BasicLifecycler, ringDesc ring.Desc, instanceExists bool, instanceID string, instanceDesc ring.InstanceDesc) (ring.InstanceState, ring.Tokens) {
	// When we initialize the scheduler instance in the ring we want to start from
	// a clean situation, so whatever is the state we set it JOINING, while we keep existing
	// tokens (if any) or the ones loaded from file.
	var tokens []uint32
	if instanceExists {
		tokens = instanceDesc.GetTokens()
	}

	takenTokens := ringDesc.GetTokens()
	newTokens := ring.GenerateTokens(ringNumTokens-len(tokens), takenTokens)

	// Tokens sorting will be enforced by the parent caller.
	tokens = append(tokens, newTokens...)

	return ring.JOINING, tokens
}

func (s *Scheduler) OnRingInstanceTokens(_ *ring.BasicLifecycler, _ ring.Tokens) {}
func (s *Scheduler) OnRingInstanceStopping(_ *ring.BasicLifecycler)              {}
func (s *Scheduler) OnRingInstanceHeartbeat(_ *ring.BasicLifecycler, _ *ring.Desc, _ *ring.InstanceDesc) {
}

func (s *Scheduler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if s.cfg.UseSchedulerRing {
		s.ring.ServeHTTP(w, req)
	} else {
		_, _ = w.Write([]byte("QueryScheduler running with '-query-scheduler.use-scheduler-ring' set to false."))
	}
}
