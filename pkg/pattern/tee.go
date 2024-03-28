package pattern

import (
	"context"
	"errors"
	"fmt"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/ring"
	ring_client "github.com/grafana/dskit/ring/client"
	"github.com/grafana/dskit/services"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/grafana/loki/pkg/distributor"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/pattern/clientpool"
)

type Tee struct {
	cfg    Config
	logger log.Logger

	services.Service
	subservices        *services.Manager
	subservicesWatcher *services.FailureWatcher
	ring               *ring.Ring
	pool               *ring_client.Pool

	ingesterAppends *prometheus.CounterVec
}

func NewTee(
	cfg Config,
	metricsNamespace string,
	registerer prometheus.Registerer,
	logger log.Logger,
) (*Tee, error) {
	var err error
	registerer = prometheus.WrapRegistererWithPrefix(metricsNamespace+"_", registerer)

	t := &Tee{
		logger: log.With(logger, "component", "pattern-tee"),
		ingesterAppends: promauto.With(registerer).NewCounterVec(prometheus.CounterOpts{
			Name: "pattern_ingester_appends_total",
			Help: "The total number of batch appends sent to pattern ingesters.",
		}, []string{"ingester", "status"}),
		cfg: cfg,
	}
	t.ring, err = ring.New(cfg.LifecyclerConfig.RingConfig, "pattern-ingester", "pattern-ring", t.logger, registerer)
	if err != nil {
		return nil, err
	}
	factory := cfg.factory
	if factory == nil {
		factory = ring_client.PoolAddrFunc(func(addr string) (ring_client.PoolClient, error) {
			return clientpool.NewClient(cfg.ClientConfig, addr)
		})
	}
	t.pool = clientpool.NewPool("pattern-ingester", cfg.ClientConfig.PoolConfig, t.ring, factory, logger, metricsNamespace)

	t.subservices, err = services.NewManager(t.pool, t.ring)
	if err != nil {
		return nil, fmt.Errorf("services manager: %w", err)
	}
	t.subservicesWatcher = services.NewFailureWatcher()
	t.subservicesWatcher.WatchManager(t.subservices)
	t.Service = services.NewBasicService(t.starting, t.running, t.stopping)

	return t, nil
}

func (t *Tee) starting(ctx context.Context) error {
	return services.StartManagerAndAwaitHealthy(ctx, t.subservices)
}

func (t *Tee) running(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return nil
	case err := <-t.subservicesWatcher.Chan():
		return fmt.Errorf("pattern tee subservices failed: %w", err)
	}
}

func (t *Tee) stopping(_ error) error {
	return services.StopManagerAndAwaitStopped(context.Background(), t.subservices)
}

// Duplicate Implements distributor.Tee which is used to tee distributor requests to pattern ingesters.
func (t *Tee) Duplicate(tenant string, streams []distributor.KeyedStream) {
	for idx := range streams {
		go func(stream distributor.KeyedStream) {
			if err := t.sendStream(tenant, stream); err != nil {
				level.Error(t.logger).Log("msg", "failed to send stream to pattern ingester", "err", err)
			}
		}(streams[idx])
	}
}

func (t *Tee) sendStream(tenant string, stream distributor.KeyedStream) error {
	var descs [1]ring.InstanceDesc
	replicationSet, err := t.ring.Get(stream.HashKey, ring.WriteNoExtend, descs[:0], nil, nil)
	if err != nil {
		return err
	}
	if replicationSet.Instances == nil {
		return errors.New("no instances found")
	}
	addr := replicationSet.Instances[0].Addr
	client, err := t.pool.GetClientFor(addr)
	if err != nil {
		return err
	}
	req := &logproto.PushRequest{
		Streams: []logproto.Stream{
			stream.Stream,
		},
	}

	ctx, cancel := context.WithTimeout(user.InjectOrgID(context.Background(), tenant), t.cfg.ClientConfig.RemoteTimeout)
	defer cancel()
	_, err = client.(logproto.PatternClient).Push(ctx, req)
	if err != nil {
		t.ingesterAppends.WithLabelValues(addr, "fail").Inc()
		return err
	}
	t.ingesterAppends.WithLabelValues(addr, "success").Inc()
	return nil
}
