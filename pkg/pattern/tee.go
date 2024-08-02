package pattern

import (
	"context"
	"errors"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/ring"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/grafana/loki/v3/pkg/distributor"
	"github.com/grafana/loki/v3/pkg/logproto"
)

type Tee struct {
	cfg        Config
	logger     log.Logger
	ringClient RingClient

	ingesterAppends       *prometheus.CounterVec
	ingesterMetricAppends *prometheus.CounterVec

	teedRequests *prometheus.CounterVec

	requestCh chan request

	// shutdown channel
	quit chan struct{}
}

type request struct {
	tenant string
	stream distributor.KeyedStream
}

func NewTee(
	cfg Config,
	ringClient RingClient,
	metricsNamespace string,
	registerer prometheus.Registerer,
	logger log.Logger,
) (*Tee, error) {
	registerer = prometheus.WrapRegistererWithPrefix(metricsNamespace+"_", registerer)

	t := &Tee{
		logger: log.With(logger, "component", "pattern-tee"),
		ingesterAppends: promauto.With(registerer).NewCounterVec(prometheus.CounterOpts{
			Name: "pattern_ingester_appends_total",
			Help: "The total number of batch appends sent to pattern ingesters.",
		}, []string{"ingester", "status"}),
		ingesterMetricAppends: promauto.With(registerer).NewCounterVec(prometheus.CounterOpts{
			Name: "pattern_ingester_metric_appends_total",
			Help: "The total number of metric only batch appends sent to pattern ingesters. These requests will not be processed for patterns.",
		}, []string{"ingester", "status"}),
		teedRequests: promauto.With(registerer).NewCounterVec(prometheus.CounterOpts{
			Name: "pattern_ingester_teed_requests_total",
			Help: "The total number of batch appends sent to fallback pattern ingesters, for not owned streams.",
		}, []string{"tenant", "status"}),
		cfg:        cfg,
		ringClient: ringClient,
		requestCh:  make(chan request, cfg.TeeBufferSize),
		quit:       make(chan struct{}),
	}

	for i := 0; i < cfg.TeeParallelism; i++ {
		go t.run()
	}

	return t, nil
}

func (t *Tee) run() {
	for {
		select {
		case <-t.quit:
			return
		case req := <-t.requestCh:
			ctx, cancel := context.WithTimeout(
				user.InjectOrgID(context.Background(), req.tenant),
				t.cfg.ClientConfig.RemoteTimeout,
			)
			defer cancel()

			if err := t.sendStream(ctx, req.stream); err != nil {
				level.Error(t.logger).Log("msg", "failed to send stream to pattern ingester", "err", err)
			}
		}
	}
}

func (t *Tee) sendStream(ctx context.Context, stream distributor.KeyedStream) error {
	err := t.sendOwnedStream(ctx, stream)
	if err == nil {
		// Success, return early
		return nil
	}

	// Pattern ingesters serve 2 functions, processing patterns and aggregating metrics.
	// Only owned streams are processed for patterns, however any pattern ingester can
	// aggregate metrics for any stream. Therefore, if we can't send the owned stream,
	// try to forward request to any pattern ingester so we at least capture the metrics.
	replicationSet, err := t.ringClient.Ring().GetReplicationSetForOperation(ring.WriteNoExtend)
	if replicationSet.Instances == nil {
		t.ingesterMetricAppends.WithLabelValues("none", "fail").Inc()
		return errors.New("no instances found for fallback")
	}

	for _, instance := range replicationSet.Instances {
		addr := instance.Addr
		client, err := t.ringClient.Pool().GetClientFor(addr)
		if err != nil {
			req := &logproto.PushRequest{
				Streams: []logproto.Stream{
					stream.Stream,
				},
			}

			_, err = client.(logproto.PatternClient).Push(ctx, req)
			if err != nil {
				t.ingesterMetricAppends.WithLabelValues(addr, "fail").Inc()
				continue
			}
			t.ingesterMetricAppends.WithLabelValues(addr, "success").Inc()
			// bail after any success to prevent sending more than one
			return nil
		}
	}

	return err
}

func (t *Tee) sendOwnedStream(ctx context.Context, stream distributor.KeyedStream) error {
	var descs [1]ring.InstanceDesc
	replicationSet, err := t.ringClient.Ring().Get(stream.HashKey, ring.WriteNoExtend, descs[:0], nil, nil)
	if err != nil {
		return err
	}
	if replicationSet.Instances == nil {
		return errors.New("no instances found")
	}
	addr := replicationSet.Instances[0].Addr
	client, err := t.ringClient.Pool().GetClientFor(addr)
	if err != nil {
		return err
	}
	req := &logproto.PushRequest{
		Streams: []logproto.Stream{
			stream.Stream,
		},
	}

	_, err = client.(logproto.PatternClient).Push(ctx, req)
	if err != nil {
		t.ingesterAppends.WithLabelValues(addr, "fail").Inc()
		return err
	}
	// Success here means the stream will be processed for both metrics and patterns
	t.ingesterAppends.WithLabelValues(addr, "success").Inc()
	t.ingesterMetricAppends.WithLabelValues(addr, "success").Inc()
	return nil
}

// Duplicate Implements distributor.Tee which is used to tee distributor requests to pattern ingesters.
func (t *Tee) Duplicate(tenant string, streams []distributor.KeyedStream) {
	for idx := range streams {
		go func(stream distributor.KeyedStream) {
			req := request{
				tenant: tenant,
				stream: stream,
			}

			// We need to prioritize protecting distributors to prevent bigger problems to the system, so
			// we respond to backpressure by dropping requests if the channel is full
			select {
			case t.requestCh <- req:
				t.teedRequests.WithLabelValues(tenant, "queued").Inc()
				return
			default:
				t.teedRequests.WithLabelValues(tenant, "dropped").Inc()
				return
			}
		}(streams[idx])
	}
}

// Stop will cancel any ongoing requests and stop the goroutine listening for requests
func (t *Tee) Stop() {
	close(t.quit)
	t.requestCh = make(chan request, t.cfg.TeeBufferSize)
}
