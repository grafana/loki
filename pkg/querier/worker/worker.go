package worker

import (
	"context"
	"encoding/json"
	"flag"
	"os"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/grpcclient"
	"github.com/grafana/dskit/ring"
	"github.com/grafana/dskit/services"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/weaveworks/common/httpgrpc"
	"google.golang.org/grpc"

	loki_nats "github.com/grafana/loki/pkg/nats"
	"github.com/grafana/loki/pkg/util"
	lokiutil "github.com/grafana/loki/pkg/util"
)

type Config struct {
	FrontendAddress  string        `yaml:"frontend_address"`
	SchedulerAddress string        `yaml:"scheduler_address"`
	DNSLookupPeriod  time.Duration `yaml:"dns_lookup_duration"`

	Parallelism           int  `yaml:"parallelism"`
	MatchMaxConcurrency   bool `yaml:"match_max_concurrent"`
	MaxConcurrentRequests int  `yaml:"-"` // Must be same as passed to LogQL Engine.

	QuerierID string `yaml:"id"`

	GRPCClientConfig grpcclient.Config `yaml:"grpc_client_config"`
}

func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	f.StringVar(&cfg.SchedulerAddress, "querier.scheduler-address", "", "Hostname (and port) of scheduler that querier will periodically resolve, connect to and receive queries from. Only one of -querier.frontend-address or -querier.scheduler-address can be set. If neither is set, queries are only received via HTTP endpoint.")
	f.StringVar(&cfg.FrontendAddress, "querier.frontend-address", "", "Address of query frontend service, in host:port format. If -querier.scheduler-address is set as well, querier will use scheduler instead. Only one of -querier.frontend-address or -querier.scheduler-address can be set. If neither is set, queries are only received via HTTP endpoint.")

	f.DurationVar(&cfg.DNSLookupPeriod, "querier.dns-lookup-period", 3*time.Second, "How often to query DNS for query-frontend or query-scheduler address. Also used to determine how often to poll the scheduler-ring for addresses if the scheduler-ring is configured.")

	f.IntVar(&cfg.Parallelism, "querier.worker-parallelism", 10, "Number of simultaneous queries to process per query-frontend or query-scheduler.")
	f.BoolVar(&cfg.MatchMaxConcurrency, "querier.worker-match-max-concurrent", true, "Force worker concurrency to match the -querier.max-concurrent option. Overrides querier.worker-parallelism.")
	f.StringVar(&cfg.QuerierID, "querier.id", "", "Querier ID, sent to frontend service to identify requests from the same querier. Defaults to hostname.")

	cfg.GRPCClientConfig.RegisterFlagsWithPrefix("querier.frontend-client", f)
}

func (cfg *Config) Validate(log log.Logger) error {
	if cfg.FrontendAddress != "" && cfg.SchedulerAddress != "" {
		return errors.New("frontend address and scheduler address are mutually exclusive, please use only one")
	}
	return cfg.GRPCClientConfig.Validate(log)
}

// Handler for HTTP requests wrapped in protobuf messages.
type RequestHandler interface {
	Handle(context.Context, *httpgrpc.HTTPRequest) (*httpgrpc.HTTPResponse, error)
}

// Single processor handles all streaming operations to query-frontend or query-scheduler to fetch queries
// and process them.
type processor interface {
	// Each invocation of processQueriesOnSingleStream starts new streaming operation to query-frontend
	// or query-scheduler to fetch queries and execute them.
	//
	// This method must react on context being finished, and stop when that happens.
	//
	// processorManager (not processor) is responsible for starting as many goroutines as needed for each connection.
	processQueriesOnSingleStream(ctx context.Context, conn *grpc.ClientConn, address string)

	// notifyShutdown notifies the remote query-frontend or query-scheduler that the querier is
	// shutting down.
	notifyShutdown(ctx context.Context, conn *grpc.ClientConn, address string)

	processAsyncRequest(js jetstream.JetStream, id, subject string, req *httpgrpc.HTTPRequest)
}

type querierWorker struct {
	*services.BasicService

	cfg    Config
	logger log.Logger

	processor processor

	natsConnProvider *loki_nats.ConnProvider
	conn             *nats.Conn
	stream           jetstream.JetStream
	consumer         jetstream.Consumer

	subservices        *services.Manager
	subservicesWatcher *services.FailureWatcher

	mu sync.Mutex
	// Set to nil when stop is called... no more managers are created afterwards.
	managers map[string]*processorManager

	metrics *Metrics
}

func NewQuerierWorker(cfg Config, rng ring.ReadRing, handler RequestHandler, natsCfg loki_nats.Config, logger log.Logger, reg prometheus.Registerer) (services.Service, error) {
	if cfg.QuerierID == "" {
		hostname, err := os.Hostname()
		if err != nil {
			return nil, errors.Wrap(err, "failed to get hostname for configuring querier ID")
		}
		cfg.QuerierID = hostname
	}

	metrics := NewMetrics(cfg, reg)
	var processor processor
	var servs []services.Service
	var address string

	switch {
	case rng != nil:
		level.Info(logger).Log("msg", "Starting querier worker using query-scheduler and scheduler ring for addresses")
		processor, servs = newSchedulerProcessor(cfg, handler, logger, metrics)

	case cfg.SchedulerAddress != "":
		level.Info(logger).Log("msg", "Starting querier worker connected to query-scheduler", "scheduler", cfg.SchedulerAddress)

		address = cfg.SchedulerAddress
		processor, servs = newSchedulerProcessor(cfg, handler, logger, metrics)

	case cfg.FrontendAddress != "":
		level.Info(logger).Log("msg", "Starting querier worker connected to query-frontend", "frontend", cfg.FrontendAddress)

		address = cfg.FrontendAddress
		processor = newFrontendProcessor(cfg, handler, logger)
	default:
		return nil, errors.New("unable to start the querier worker, need to configure one of frontend_address, scheduler_address, or a ring config in the query_scheduler config block")
	}

	return newQuerierWorkerWithProcessor(cfg, natsCfg, metrics, logger, processor, address, rng, servs)
}

func newQuerierWorkerWithProcessor(cfg Config, natsCfg loki_nats.Config, metrics *Metrics, logger log.Logger, processor processor, address string, ring ring.ReadRing, servs []services.Service) (*querierWorker, error) {
	f := &querierWorker{
		cfg:       cfg,
		logger:    logger,
		managers:  map[string]*processorManager{},
		processor: processor,
		metrics:   metrics,
	}

	// Empty address is only used in tests, where individual targets are added manually.
	if address != "" {
		w, err := util.NewDNSWatcher(address, cfg.DNSLookupPeriod, f)
		if err != nil {
			return nil, err
		}

		servs = append(servs, w)
	}

	if ring != nil {
		w, err := lokiutil.NewRingWatcher(log.With(logger, "component", "querier-scheduler-worker"), ring, cfg.DNSLookupPeriod, f)
		if err != nil {
			return nil, err
		}
		servs = append(servs, w)
	}

	// TODO(@periklis) Make this subservice optional
	natsConn, err := loki_nats.NewConnProvider(natsCfg)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create NATS connection provider")
	}
	f.natsConnProvider = natsConn
	servs = append(servs, f.natsConnProvider)

	if len(servs) > 0 {
		subservices, err := services.NewManager(servs...)
		if err != nil {
			return nil, errors.Wrap(err, "querier worker subservices")
		}

		f.subservices = subservices
	}

	f.BasicService = services.NewBasicService(f.starting, f.running, f.stopping)
	return f, nil
}

func (w *querierWorker) starting(ctx context.Context) error {
	w.subservicesWatcher = services.NewFailureWatcher()

	if w.subservices == nil {
		return nil
	}
	w.subservicesWatcher.WatchManager(w.subservices)

	if err := services.StartManagerAndAwaitHealthy(ctx, w.subservices); err != nil {
		return errors.Wrap(err, "unable to start querier worker subservices")
	}

	conn, err := w.natsConnProvider.GetConn()
	if err != nil {
		return err
	}
	w.conn = conn

	var (
		js   jetstream.JetStream
		cons jetstream.Consumer
	)

	for {
		js, err = w.createOrUpdateStream(ctx, "query-responses", []string{"processed"})
		if err != nil {
			level.Error(w.logger).Log("msg", "failed to create or update jetstream stream", "err", err.Error())
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(5 * time.Second):
			}
			continue
		}
		conn.Flush()
		break
	}
	w.stream = js

	for {
		cons, err = w.createrOrUpdateConsumer(ctx, "query", "query.*")
		if err != nil {
			level.Error(w.logger).Log("msg", "failed to create or update jetstream consumer", "err", err.Error())
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(5 * time.Second):
			}
			continue
		}
		conn.Flush()
		break
	}
	w.consumer = cons

	level.Info(w.logger).Log("msg", "successfully created jetstream consumer")

	return nil
}

func (w *querierWorker) createOrUpdateStream(ctx context.Context, name string, subjects []string) (jetstream.JetStream, error) {
	stream, err := jetstream.New(w.conn)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get jetstream context")
	}

	streamOpts := jetstream.StreamConfig{
		Name:      name,
		Subjects:  subjects,
		Replicas:  3,
		MaxAge:    4 * time.Hour,
		Retention: jetstream.LimitsPolicy,
		Discard:   jetstream.DiscardOld,
	}

	_, err = stream.UpdateStream(ctx, streamOpts)
	if err != nil {
		if err == jetstream.ErrStreamNotFound {
			_, err = stream.CreateStream(ctx, streamOpts)
			if err != nil {
				return nil, errors.Wrap(err, "failed to create stream")
			}
			return stream, nil
		}
		return nil, errors.Wrap(err, "failed to update stream")
	}

	return stream, nil
}

func (w *querierWorker) createrOrUpdateConsumer(ctx context.Context, streamName, subject string) (jetstream.Consumer, error) {
	js, err := jetstream.New(w.conn)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get jetstream context")
	}

	cons, err := js.CreateOrUpdateConsumer(ctx, "query", jetstream.ConsumerConfig{
		AckPolicy:     jetstream.AckExplicitPolicy,
		FilterSubject: "query.*",
	})
	if err != nil {
		return nil, err
	}

	return cons, nil
}

func (w *querierWorker) running(ctx context.Context) error {
	consCtx, err := w.consumer.Consume(w.consumeHandler)
	if err != nil {
		return err
	}
	defer consCtx.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-w.subservicesWatcher.Chan():
			return errors.Wrap(err, "worker subservice failed")
		}
	}
}

func (w *querierWorker) stopping(_ error) error {
	if err := w.conn.Drain(); err != nil {
		return err
	}

	w.conn.Close()

	// Stop all goroutines fetching queries. Note that in Stopping state,
	// worker no longer creates new managers in AddressAdded method.
	w.mu.Lock()
	for _, m := range w.managers {
		m.stop()
	}
	w.mu.Unlock()

	if w.subservices == nil {
		return nil
	}

	// Stop DNS watcher and services used by processor.
	return services.StopManagerAndAwaitStopped(context.Background(), w.subservices)
}

func (w *querierWorker) consumeHandler(msg jetstream.Msg) {
	if err := msg.InProgress(); err != nil {
		level.Error(w.logger).Log("msg", "failed to notify server that this message is in progress")
		return
	}

	req := &httpgrpc.HTTPRequest{}
	err := json.Unmarshal(msg.Data(), req)
	if err != nil {
		if err := msg.Nak(); err != nil {
			level.Error(w.logger).Log("msg", "failed to set nack nats message")
		}

		return
	}

	level.Info(w.logger).Log("msg", "received nats msg", "subject", msg.Subject(), "reply", msg.Reply())

	var (
		id  = msg.Headers().Get("Nats-Msg-Id")
		sub = "processed"
	)

	w.processor.processAsyncRequest(w.stream, id, sub, req)

	if err := msg.Ack(); err != nil {
		level.Error(w.logger).Log("msg", "failed to set ack nats message")
	}
}

func (w *querierWorker) AddressAdded(address string) {
	ctx := w.ServiceContext()
	if ctx == nil || ctx.Err() != nil {
		return
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	if m := w.managers[address]; m != nil {
		return
	}

	level.Info(w.logger).Log("msg", "adding connection", "addr", address)
	conn, err := w.connect(context.Background(), address)
	if err != nil {
		level.Error(w.logger).Log("msg", "error connecting", "addr", address, "err", err)
		return
	}

	w.managers[address] = newProcessorManager(ctx, w.processor, conn, address)
	// Called with lock.
	w.resetConcurrency()
}

func (w *querierWorker) AddressRemoved(address string) {
	level.Info(w.logger).Log("msg", "removing connection", "addr", address)

	w.mu.Lock()
	p := w.managers[address]
	delete(w.managers, address)
	// Called with lock.
	w.resetConcurrency()
	w.mu.Unlock()

	if p != nil {
		p.stop()
	}
}

// Must be called with lock.
func (w *querierWorker) resetConcurrency() {
	totalConcurrency := 0
	defer func() {
		w.metrics.concurrentWorkers.Set(float64(totalConcurrency))
	}()
	index := 0

	for _, m := range w.managers {
		concurrency := 0

		if w.cfg.MatchMaxConcurrency {
			concurrency = w.cfg.MaxConcurrentRequests / len(w.managers)

			// If max concurrency does not evenly divide into our frontends a subset will be chosen
			// to receive an extra connection.  Frontend addresses were shuffled above so this will be a
			// random selection of frontends.
			if index < w.cfg.MaxConcurrentRequests%len(w.managers) {
				level.Warn(w.logger).Log("msg", "max concurrency is not evenly divisible across targets, adding an extra connection", "addr", m.address)
				concurrency++
			}
		} else {
			concurrency = w.cfg.Parallelism
		}

		// If concurrency is 0 then MaxConcurrentRequests is less than the total number of
		// frontends/schedulers. In order to prevent accidentally starving a frontend or scheduler we are just going to
		// always connect once to every target.  This is dangerous b/c we may start exceeding LogQL
		// max concurrency.
		if concurrency == 0 {
			concurrency = 1
		}

		totalConcurrency += concurrency
		m.concurrency(concurrency)
		index++
	}

	if totalConcurrency > w.cfg.MaxConcurrentRequests {
		level.Warn(w.logger).Log("msg", "total worker concurrency is greater than logql max concurrency. Queries may be queued in the querier which reduces QOS")
	}
}

func (w *querierWorker) connect(ctx context.Context, address string) (*grpc.ClientConn, error) {
	// Because we only use single long-running method, it doesn't make sense to inject user ID, send over tracing or add metrics.
	opts, err := w.cfg.GRPCClientConfig.DialOption(nil, nil)
	if err != nil {
		return nil, err
	}

	conn, err := grpc.DialContext(ctx, address, opts...)
	if err != nil {
		return nil, err
	}
	return conn, nil
}
