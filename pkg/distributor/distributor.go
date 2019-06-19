package distributor

import (
	"context"
	"flag"
	"sync/atomic"
	"time"

	cortex_client "github.com/cortexproject/cortex/pkg/ingester/client"
	"github.com/cortexproject/cortex/pkg/ring"
	cortex_util "github.com/cortexproject/cortex/pkg/util"
	"github.com/cortexproject/cortex/pkg/util/validation"
	opentracing "github.com/opentracing/opentracing-go"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/weaveworks/common/user"
	"google.golang.org/grpc/health/grpc_health_v1"

	"github.com/grafana/loki/pkg/ingester/client"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/util"
)

const metricName = "logs"

var (
	ingesterAppends = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "loki",
		Name:      "distributor_ingester_appends_total",
		Help:      "The total number of batch appends sent to ingesters.",
	}, []string{"ingester"})
	ingesterAppendFailures = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "loki",
		Name:      "distributor_ingester_append_failures_total",
		Help:      "The total number of failed batch appends sent to ingesters.",
	}, []string{"ingester"})

	bytesIngested = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "loki",
		Name:      "distributor_bytes_received_total",
		Help:      "The total number of uncompressed bytes received per tenant",
	}, []string{"tenant"})
	linesIngested = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "loki",
		Name:      "distributor_lines_received_total",
		Help:      "The total number of lines received per tenant",
	}, []string{"tenant"})
)

// Config for a Distributor.
type Config struct {
	// For testing.
	factory func(addr string) (grpc_health_v1.HealthClient, error)
}

// RegisterFlags registers the flags.
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
}

// Distributor coordinates replicates and distribution of log streams.
type Distributor struct {
	cfg       Config
	clientCfg client.Config
	ring      ring.ReadRing
	overrides *validation.Overrides
	pool      *cortex_client.Pool
}

// New a distributor creates.
func New(cfg Config, clientCfg client.Config, ring ring.ReadRing, overrides *validation.Overrides) (*Distributor, error) {
	factory := cfg.factory
	if factory == nil {
		factory = func(addr string) (grpc_health_v1.HealthClient, error) {
			return client.New(clientCfg, addr)
		}
	}

	return &Distributor{
		cfg:       cfg,
		clientCfg: clientCfg,
		ring:      ring,
		overrides: overrides,
		pool:      cortex_client.NewPool(clientCfg.PoolConfig, ring, factory, cortex_util.Logger),
	}, nil
}

// TODO taken from Cortex, see if we can refactor out an usable interface.
type streamTracker struct {
	stream      *logproto.Stream
	minSuccess  int
	maxFailures int
	succeeded   int32
	failed      int32
}

// TODO taken from Cortex, see if we can refactor out an usable interface.
type pushTracker struct {
	samplesPending int32
	samplesFailed  int32
	done           chan struct{}
	err            chan error
}

// Push a set of streams.
func (d *Distributor) Push(ctx context.Context, req *logproto.PushRequest) (*logproto.PushResponse, error) {
	userID, err := user.ExtractOrgID(ctx)
	if err != nil {
		return nil, err
	}

	// Track metrics.
	bytesCount := 0
	lineCount := 0
	for _, stream := range req.Streams {
		for _, entry := range stream.Entries {
			bytesCount += len(entry.Line)
			lineCount++
		}
	}
	bytesIngested.WithLabelValues(userID).Add(float64(bytesCount))
	linesIngested.WithLabelValues(userID).Add(float64(lineCount))

	// First we flatten out the request into a list of samples.
	// We use the heuristic of 1 sample per TS to size the array.
	// We also work out the hash value at the same time.
	streams := make([]streamTracker, 0, len(req.Streams))
	keys := make([]uint32, 0, len(req.Streams))
	var validationErr error
	for _, stream := range req.Streams {
		if err := d.validateLabels(userID, stream.Labels); err != nil {
			validationErr = err
			continue
		}

		entries := make([]logproto.Entry, 0, len(stream.Entries))
		for _, entry := range stream.Entries {
			if err := d.overrides.ValidateSample(userID, metricName, cortex_client.Sample{
				TimestampMs: entry.Timestamp.UnixNano() / int64(time.Millisecond),
			}); err != nil {
				validationErr = err
				continue
			}
			entries = append(entries, entry)
		}

		if len(entries) == 0 {
			continue
		}
		stream.Entries = entries
		keys = append(keys, util.TokenFor(userID, stream.Labels))
		streams = append(streams, streamTracker{
			stream: stream,
		})
	}

	if len(streams) == 0 {
		return &logproto.PushResponse{}, validationErr
	}

	replicationSets, err := d.ring.BatchGet(keys, ring.Write)
	if err != nil {
		return nil, err
	}

	samplesByIngester := map[string][]*streamTracker{}
	ingesterDescs := map[string]ring.IngesterDesc{}
	for i, replicationSet := range replicationSets {
		streams[i].minSuccess = len(replicationSet.Ingesters) - replicationSet.MaxErrors
		streams[i].maxFailures = replicationSet.MaxErrors
		for _, ingester := range replicationSet.Ingesters {
			samplesByIngester[ingester.Addr] = append(samplesByIngester[ingester.Addr], &streams[i])
			ingesterDescs[ingester.Addr] = ingester
		}
	}

	tracker := pushTracker{
		samplesPending: int32(len(streams)),
		done:           make(chan struct{}),
		err:            make(chan error),
	}
	for ingester, samples := range samplesByIngester {
		go func(ingester ring.IngesterDesc, samples []*streamTracker) {
			// Use a background context to make sure all ingesters get samples even if we return early
			localCtx, cancel := context.WithTimeout(context.Background(), d.clientCfg.RemoteTimeout)
			defer cancel()
			localCtx = user.InjectOrgID(localCtx, userID)
			if sp := opentracing.SpanFromContext(ctx); sp != nil {
				localCtx = opentracing.ContextWithSpan(localCtx, sp)
			}
			d.sendSamples(localCtx, ingester, samples, &tracker)
		}(ingesterDescs[ingester], samples)
	}
	select {
	case err := <-tracker.err:
		return nil, err
	case <-tracker.done:
		return &logproto.PushResponse{}, validationErr
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (d *Distributor) validateLabels(userID, labels string) error {
	ls, err := util.ToClientLabels(labels)
	if err != nil {
		return err
	}

	return d.overrides.ValidateLabels(userID, ls)
}

// TODO taken from Cortex, see if we can refactor out an usable interface.
func (d *Distributor) sendSamples(ctx context.Context, ingester ring.IngesterDesc, streamTrackers []*streamTracker, pushTracker *pushTracker) {
	err := d.sendSamplesErr(ctx, ingester, streamTrackers)

	// If we succeed, decrement each sample's pending count by one.  If we reach
	// the required number of successful puts on this sample, then decrement the
	// number of pending samples by one.  If we successfully push all samples to
	// min success ingesters, wake up the waiting rpc so it can return early.
	// Similarly, track the number of errors, and if it exceeds maxFailures
	// shortcut the waiting rpc.
	//
	// The use of atomic increments here guarantees only a single sendSamples
	// goroutine will write to either channel.
	for i := range streamTrackers {
		if err != nil {
			if atomic.AddInt32(&streamTrackers[i].failed, 1) <= int32(streamTrackers[i].maxFailures) {
				continue
			}
			if atomic.AddInt32(&pushTracker.samplesFailed, 1) == 1 {
				pushTracker.err <- err
			}
		} else {
			if atomic.AddInt32(&streamTrackers[i].succeeded, 1) != int32(streamTrackers[i].minSuccess) {
				continue
			}
			if atomic.AddInt32(&pushTracker.samplesPending, -1) == 0 {
				pushTracker.done <- struct{}{}
			}
		}
	}
}

// TODO taken from Cortex, see if we can refactor out an usable interface.
func (d *Distributor) sendSamplesErr(ctx context.Context, ingester ring.IngesterDesc, streams []*streamTracker) error {
	c, err := d.pool.GetClientFor(ingester.Addr)
	if err != nil {
		return err
	}

	req := &logproto.PushRequest{
		Streams: make([]*logproto.Stream, len(streams)),
	}
	for i, s := range streams {
		req.Streams[i] = s.stream
	}

	_, err = c.(logproto.PusherClient).Push(ctx, req)
	ingesterAppends.WithLabelValues(ingester.Addr).Inc()
	if err != nil {
		ingesterAppendFailures.WithLabelValues(ingester.Addr).Inc()
	}
	return err
}

// Check implements the grpc healthcheck
func (*Distributor) Check(_ context.Context, _ *grpc_health_v1.HealthCheckRequest) (*grpc_health_v1.HealthCheckResponse, error) {
	return &grpc_health_v1.HealthCheckResponse{Status: grpc_health_v1.HealthCheckResponse_SERVING}, nil
}
