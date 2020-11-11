package transport

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"strings"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/weaveworks/common/httpgrpc"
	"github.com/weaveworks/common/httpgrpc/server"

	"github.com/cortexproject/cortex/pkg/util"
)

const (
	// StatusClientClosedRequest is the status code for when a client request cancellation of an http request
	StatusClientClosedRequest = 499
)

var (
	errCanceled              = httpgrpc.Errorf(StatusClientClosedRequest, context.Canceled.Error())
	errDeadlineExceeded      = httpgrpc.Errorf(http.StatusGatewayTimeout, context.DeadlineExceeded.Error())
	errRequestEntityTooLarge = httpgrpc.Errorf(http.StatusRequestEntityTooLarge, "http: request body too large")
)

// Config for a Handler.
type HandlerConfig struct {
	LogQueriesLongerThan time.Duration `yaml:"log_queries_longer_than"`
	MaxBodySize          int64         `yaml:"max_body_size"`
}

func (cfg *HandlerConfig) RegisterFlags(f *flag.FlagSet) {
	f.DurationVar(&cfg.LogQueriesLongerThan, "frontend.log-queries-longer-than", 0, "Log queries that are slower than the specified duration. Set to 0 to disable. Set to < 0 to enable on all queries.")
	f.Int64Var(&cfg.MaxBodySize, "frontend.max-body-size", 10*1024*1024, "Max body size for downstream prometheus.")
}

// Handler accepts queries and forwards them to RoundTripper. It can log slow queries,
// but all other logic is inside the RoundTripper.
type Handler struct {
	cfg          HandlerConfig
	log          log.Logger
	roundTripper http.RoundTripper
}

// New creates a new frontend handler.
func NewHandler(cfg HandlerConfig, roundTripper http.RoundTripper, log log.Logger) http.Handler {
	return &Handler{
		cfg:          cfg,
		log:          log,
		roundTripper: roundTripper,
	}
}

func (f *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer func() {
		_ = r.Body.Close()
	}()

	// Buffer the body for later use to track slow queries.
	var buf bytes.Buffer
	r.Body = http.MaxBytesReader(w, r.Body, f.cfg.MaxBodySize)
	r.Body = ioutil.NopCloser(io.TeeReader(r.Body, &buf))

	startTime := time.Now()
	resp, err := f.roundTripper.RoundTrip(r)
	queryResponseTime := time.Since(startTime)

	if err != nil {
		writeError(w, err)
		return
	}

	hs := w.Header()
	for h, vs := range resp.Header {
		hs[h] = vs
	}

	w.WriteHeader(resp.StatusCode)
	// we don't check for copy error as there is no much we can do at this point
	_, _ = io.Copy(w, resp.Body)

	f.reportSlowQuery(queryResponseTime, r, buf)
}

// reportSlowQuery reports slow queries if LogQueriesLongerThan is set to <0, where 0 disables logging
func (f *Handler) reportSlowQuery(queryResponseTime time.Duration, r *http.Request, bodyBuf bytes.Buffer) {
	if f.cfg.LogQueriesLongerThan == 0 || queryResponseTime <= f.cfg.LogQueriesLongerThan {
		return
	}

	logMessage := []interface{}{
		"msg", "slow query detected",
		"method", r.Method,
		"host", r.Host,
		"path", r.URL.Path,
		"time_taken", queryResponseTime.String(),
	}

	// use previously buffered body
	r.Body = ioutil.NopCloser(&bodyBuf)

	// Ensure the form has been parsed so all the parameters are present
	err := r.ParseForm()
	if err != nil {
		level.Warn(util.WithContext(r.Context(), f.log)).Log("msg", "unable to parse form for request", "err", err)
	}

	// Attempt to iterate through the Form to log any filled in values
	for k, v := range r.Form {
		logMessage = append(logMessage, fmt.Sprintf("param_%s", k), strings.Join(v, ","))
	}

	level.Info(util.WithContext(r.Context(), f.log)).Log(logMessage...)
}

func writeError(w http.ResponseWriter, err error) {
	switch err {
	case context.Canceled:
		err = errCanceled
	case context.DeadlineExceeded:
		err = errDeadlineExceeded
	default:
		if strings.Contains(err.Error(), "http: request body too large") {
			err = errRequestEntityTooLarge
		}
	}
	server.WriteError(w, err)
}
