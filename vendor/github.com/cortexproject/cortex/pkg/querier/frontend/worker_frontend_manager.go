package frontend

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/weaveworks/common/httpgrpc"
	"github.com/weaveworks/common/httpgrpc/server"
	"go.uber.org/atomic"

	"github.com/cortexproject/cortex/pkg/util"
	"github.com/cortexproject/cortex/pkg/util/grpcclient"
)

var (
	backoffConfig = util.BackoffConfig{
		MinBackoff: 50 * time.Millisecond,
		MaxBackoff: 1 * time.Second,
	}
)

type frontendManager struct {
	server    *server.Server
	client    FrontendClient
	clientCfg grpcclient.ConfigWithTLS

	log log.Logger

	workerCancels     []context.CancelFunc
	serverCtx         context.Context
	wg                sync.WaitGroup
	currentProcessors *atomic.Int32
}

func newFrontendManager(serverCtx context.Context, log log.Logger, server *server.Server, client FrontendClient, clientCfg grpcclient.ConfigWithTLS) *frontendManager {
	f := &frontendManager{
		log:               log,
		client:            client,
		clientCfg:         clientCfg,
		server:            server,
		serverCtx:         serverCtx,
		currentProcessors: atomic.NewInt32(0),
	}

	return f
}

func (f *frontendManager) stop() {
	f.concurrentRequests(0)
	f.wg.Wait()
}

func (f *frontendManager) concurrentRequests(n int) {
	if n < 0 {
		n = 0
	}

	for len(f.workerCancels) < n {
		ctx, cancel := context.WithCancel(f.serverCtx)
		f.workerCancels = append(f.workerCancels, cancel)

		go f.runOne(ctx)
	}

	for len(f.workerCancels) > n {
		var cancel context.CancelFunc
		cancel, f.workerCancels = f.workerCancels[0], f.workerCancels[1:]
		cancel()
	}
}

// runOne loops, trying to establish a stream to the frontend to begin
// request processing.
func (f *frontendManager) runOne(ctx context.Context) {
	f.wg.Add(1)
	defer f.wg.Done()

	f.currentProcessors.Inc()
	defer f.currentProcessors.Dec()

	backoff := util.NewBackoff(ctx, backoffConfig)
	for backoff.Ongoing() {
		c, err := f.client.Process(ctx)
		if err != nil {
			level.Error(f.log).Log("msg", "error contacting frontend", "err", err)
			backoff.Wait()
			continue
		}

		if err := f.process(ctx, c); err != nil {
			level.Error(f.log).Log("msg", "error processing requests", "err", err)
			backoff.Wait()
			continue
		}

		backoff.Reset()
	}
}

// process loops processing requests on an established stream.
func (f *frontendManager) process(ctx context.Context, c Frontend_ProcessClient) error {
	// Build a child context so we can cancel a query when the stream is closed.
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
			response, err := f.server.Handle(ctx, request.HttpRequest)
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
			if len(response.Body) >= f.clientCfg.GRPC.MaxSendMsgSize {
				errMsg := fmt.Sprintf("response larger than the max (%d vs %d)", len(response.Body), f.clientCfg.GRPC.MaxSendMsgSize)
				response = &httpgrpc.HTTPResponse{
					Code: http.StatusRequestEntityTooLarge,
					Body: []byte(errMsg),
				}
				level.Error(f.log).Log("msg", "error processing query", "err", errMsg)
			}

			if err := c.Send(&ProcessResponse{
				HttpResponse: response,
			}); err != nil {
				level.Error(f.log).Log("msg", "error processing requests", "err", err)
			}
		}()
	}
}
