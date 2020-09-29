package frontend

import (
	"context"
	"fmt"
	"io"
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
	server     *server.Server
	connection io.Closer
	client     FrontendClient
	clientCfg  grpcclient.ConfigWithTLS
	querierID  string

	log log.Logger

	workerCancels     []context.CancelFunc
	serverCtx         context.Context
	wg                sync.WaitGroup
	currentProcessors *atomic.Int32
}

func newFrontendManager(serverCtx context.Context, log log.Logger, server *server.Server, connection io.Closer, client FrontendClient, clientCfg grpcclient.ConfigWithTLS, querierID string) *frontendManager {
	f := &frontendManager{
		log:               log,
		connection:        connection,
		client:            client,
		clientCfg:         clientCfg,
		server:            server,
		serverCtx:         serverCtx,
		currentProcessors: atomic.NewInt32(0),
		querierID:         querierID,
	}

	return f
}

func (f *frontendManager) stop() {
	f.concurrentRequests(0)
	f.wg.Wait()
	_ = f.connection.Close()
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

		if err := f.process(c); err != nil {
			level.Error(f.log).Log("msg", "error processing requests", "err", err)
			backoff.Wait()
			continue
		}

		backoff.Reset()
	}
}

// process loops processing requests on an established stream.
func (f *frontendManager) process(c Frontend_ProcessClient) error {
	// Build a child context so we can cancel a query when the stream is closed.
	ctx, cancel := context.WithCancel(c.Context())
	defer cancel()

	for {
		request, err := c.Recv()
		if err != nil {
			return err
		}

		switch request.Type {
		case HTTP_REQUEST:
			// Handle the request on a "background" goroutine, so we go back to
			// blocking on c.Recv().  This allows us to detect the stream closing
			// and cancel the query.  We don't actually handle queries in parallel
			// here, as we're running in lock step with the server - each Recv is
			// paired with a Send.
			go f.runRequest(ctx, request.HttpRequest, func(response *httpgrpc.HTTPResponse) error {
				return c.Send(&ClientToFrontend{HttpResponse: response})
			})

		case GET_ID:
			err := c.Send(&ClientToFrontend{ClientID: f.querierID})
			if err != nil {
				return err
			}

		default:
			return fmt.Errorf("unknown request type: %v", request.Type)
		}
	}
}

func (f *frontendManager) runRequest(ctx context.Context, request *httpgrpc.HTTPRequest, sendHTTPResponse func(response *httpgrpc.HTTPResponse) error) {
	response, err := f.server.Handle(ctx, request)
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

	if err := sendHTTPResponse(response); err != nil {
		level.Error(f.log).Log("msg", "error processing requests", "err", err)
	}
}
