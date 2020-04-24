package frontend

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/pkg/errors"
	"github.com/weaveworks/common/httpgrpc"
	"github.com/weaveworks/common/httpgrpc/server"
	"github.com/weaveworks/common/middleware"
	"google.golang.org/grpc"
	"google.golang.org/grpc/naming"

	"github.com/cortexproject/cortex/pkg/util"
	"github.com/cortexproject/cortex/pkg/util/grpcclient"
	"github.com/cortexproject/cortex/pkg/util/services"
)

var (
	backoffConfig = util.BackoffConfig{
		MinBackoff: 50 * time.Millisecond,
		MaxBackoff: 1 * time.Second,
	}
)

// WorkerConfig is config for a worker.
type WorkerConfig struct {
	Address           string        `yaml:"frontend_address"`
	Parallelism       int           `yaml:"parallelism"`
	DNSLookupDuration time.Duration `yaml:"dns_lookup_duration"`

	GRPCClientConfig grpcclient.Config `yaml:"grpc_client_config"`
}

// RegisterFlags adds the flags required to config this to the given FlagSet.
func (cfg *WorkerConfig) RegisterFlags(f *flag.FlagSet) {
	f.StringVar(&cfg.Address, "querier.frontend-address", "", "Address of query frontend service, in host:port format.")
	f.IntVar(&cfg.Parallelism, "querier.worker-parallelism", 10, "Number of simultaneous queries to process.")
	f.DurationVar(&cfg.DNSLookupDuration, "querier.dns-lookup-period", 10*time.Second, "How often to query DNS.")

	cfg.GRPCClientConfig.RegisterFlagsWithPrefix("querier.frontend-client", f)
}

// Worker is the counter-part to the frontend, actually processing requests.
type worker struct {
	cfg    WorkerConfig
	log    log.Logger
	server *server.Server

	watcher naming.Watcher //nolint:staticcheck //Skipping for now. If you still see this more than likely issue https://github.com/cortexproject/cortex/issues/2015 has not yet been addressed.
	wg      sync.WaitGroup
}

// NewWorker creates a new worker and returns a service that is wrapping it.
// If no address is specified, it returns nil service (and no error).
func NewWorker(cfg WorkerConfig, server *server.Server, log log.Logger) (services.Service, error) {
	if cfg.Address == "" {
		level.Info(log).Log("msg", "no address specified, not starting worker")
		return nil, nil
	}

	resolver, err := naming.NewDNSResolverWithFreq(cfg.DNSLookupDuration)
	if err != nil {
		return nil, err
	}

	watcher, err := resolver.Resolve(cfg.Address)
	if err != nil {
		return nil, err
	}

	w := &worker{
		cfg:     cfg,
		log:     log,
		server:  server,
		watcher: watcher,
	}
	return services.NewBasicService(nil, w.watchDNSLoop, w.stopping), nil
}

func (w *worker) stopping(_ error) error {
	// wait until all per-address workers are done. This is only called after watchDNSLoop exits.
	w.wg.Wait()
	return nil
}

// watchDNSLoop watches for changes in DNS and starts or stops workers.
func (w *worker) watchDNSLoop(servCtx context.Context) error {
	go func() {
		// Close the watcher, when this service is asked to stop.
		// Closing the watcher makes watchDNSLoop exit, since it only iterates on watcher updates, and has no other
		// way to stop. We cannot close the watcher in `stopping` method, because it is only called *after*
		// watchDNSLoop exits.
		<-servCtx.Done()
		w.watcher.Close()
	}()

	cancels := map[string]context.CancelFunc{}

	for {
		updates, err := w.watcher.Next()
		if err != nil {
			// watcher.Next returns error when Close is called, but we call Close when our context is done.
			// we don't want to report error in that case.
			if servCtx.Err() != nil {
				return nil
			}
			return errors.Wrapf(err, "error from DNS watcher")
		}

		for _, update := range updates {
			switch update.Op {
			case naming.Add:
				level.Debug(w.log).Log("msg", "adding connection", "addr", update.Addr)
				ctx, cancel := context.WithCancel(servCtx)
				cancels[update.Addr] = cancel
				w.runMany(ctx, update.Addr)

			case naming.Delete:
				level.Debug(w.log).Log("msg", "removing connection", "addr", update.Addr)
				if cancel, ok := cancels[update.Addr]; ok {
					cancel()
				}

			default:
				return fmt.Errorf("unknown op: %v", update.Op)
			}
		}
	}
}

// runMany starts N runOne loops for a given address.
func (w *worker) runMany(ctx context.Context, address string) {
	client, err := w.connect(address)
	if err != nil {
		level.Error(w.log).Log("msg", "error connecting", "addr", address, "err", err)
		return
	}

	w.wg.Add(w.cfg.Parallelism)
	for i := 0; i < w.cfg.Parallelism; i++ {
		go w.runOne(ctx, client)
	}
}

// runOne loops, trying to establish a stream to the frontend to begin
// request processing.
func (w *worker) runOne(ctx context.Context, client FrontendClient) {
	defer w.wg.Done()

	backoff := util.NewBackoff(ctx, backoffConfig)
	for backoff.Ongoing() {
		c, err := client.Process(ctx)
		if err != nil {
			level.Error(w.log).Log("msg", "error contacting frontend", "err", err)
			backoff.Wait()
			continue
		}

		if err := w.process(c); err != nil {
			level.Error(w.log).Log("msg", "error processing requests", "err", err)
			backoff.Wait()
			continue
		}

		backoff.Reset()
	}
}

// process loops processing requests on an established stream.
func (w *worker) process(c Frontend_ProcessClient) error {
	// Build a child context so we can cancel querie when the stream is closed.
	ctx, cancel := context.WithCancel(c.Context())
	defer cancel()

	for {
		request, err := c.Recv()
		if err != nil {
			return err
		}

		// Handle the request on a "background" goroutine, so we go back to
		// blocking on c.Recv().  This allows us to detect the stream closing
		// and cancel the query.  We don't actally handle queries in parallel
		// here, as we're running in lock step with the server - each Recv is
		// paired with a Send.
		go func() {
			response, err := w.server.Handle(ctx, request.HttpRequest)
			if err != nil {
				var ok bool
				response, ok = httpgrpc.HTTPResponseFromError(err)
				if !ok {
					response = &httpgrpc.HTTPResponse{
						Code: http.StatusInternalServerError,
						Body: []byte(err.Error()),
					}
				}
			}

			// Ensure responses that are too big are not retried.
			if len(response.Body) >= w.cfg.GRPCClientConfig.MaxSendMsgSize {
				errMsg := fmt.Sprintf("response larger than the max (%d vs %d)", len(response.Body), w.cfg.GRPCClientConfig.MaxSendMsgSize)
				response = &httpgrpc.HTTPResponse{
					Code: http.StatusRequestEntityTooLarge,
					Body: []byte(errMsg),
				}
				level.Error(w.log).Log("msg", "error processing query", "err", errMsg)
			}

			if err := c.Send(&ProcessResponse{
				HttpResponse: response,
			}); err != nil {
				level.Error(w.log).Log("msg", "error processing requests", "err", err)
			}
		}()
	}
}

func (w *worker) connect(address string) (FrontendClient, error) {
	opts := []grpc.DialOption{grpc.WithInsecure()}
	opts = append(opts, w.cfg.GRPCClientConfig.DialOption([]grpc.UnaryClientInterceptor{middleware.ClientUserHeaderInterceptor}, nil)...)
	conn, err := grpc.Dial(address, opts...)
	if err != nil {
		return nil, err
	}
	return NewFrontendClient(conn), nil
}
