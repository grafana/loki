package standalone

import (
	"context"
	"fmt"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/dns"
	"github.com/grafana/dskit/kv/codec"
	"github.com/grafana/dskit/kv/memberlist"
	"github.com/grafana/dskit/netutil"
	"github.com/grafana/dskit/ring"
	ring_client "github.com/grafana/dskit/ring/client"
	"github.com/grafana/dskit/services"
	"github.com/pkg/errors"

	"github.com/grafana/loki/v3/pkg/analytics"
	"github.com/grafana/loki/v3/pkg/distributor"
	"github.com/grafana/loki/v3/pkg/distributor/clientpool"
	"github.com/grafana/loki/v3/pkg/distributor/shardstreams"
	"github.com/grafana/loki/v3/pkg/ingester"
	ingester_client "github.com/grafana/loki/v3/pkg/ingester/client"
	"github.com/grafana/loki/v3/pkg/validation"
	"github.com/prometheus/client_golang/prometheus"
)

type Service struct {
	services.Service

	rateStore *distributor.RateStoreService

	ingesterRing *ring.Ring
	memberlistKV *memberlist.KVInitService

	subservices        *services.Manager
	subservicesWatcher *services.FailureWatcher
}

func NewRateStoreService(cfg Config, reg prometheus.Registerer, logger log.Logger) (*Service, error) {
	var (
		s    = &Service{}
		srvs = []services.Service{}
		err  error
	)

	cfg.MemberlistKV.Codecs = []codec.Codec{
		ring.GetCodec(),
		analytics.JSONCodec,
		ring.GetPartitionRingCodec(),
	}

	provider := dns.NewProvider(logger, reg, dns.GolangResolverType)
	cfg.MemberlistKV.AdvertiseAddr, err = netutil.GetFirstAddressOf(cfg.LifecyclerConfig.InfNames, logger, false)
	if err != nil {
		return nil, fmt.Errorf("failed to get instance address: %w", err)
	}

	s.memberlistKV = memberlist.NewKVInitService(&cfg.MemberlistKV, logger, provider, reg)
	cfg.LifecyclerConfig.RingConfig.KVStore.MemberlistKV = s.memberlistKV.GetMemberlistKV
	srvs = append(srvs, s.memberlistKV)

	s.ingesterRing, err = ring.New(cfg.LifecyclerConfig.RingConfig, ingesterRingName, ingester.RingKey, logger, reg)
	if err != nil {
		return nil, err
	}
	srvs = append(srvs, s.ingesterRing)

	internalIngesterClientFactory := func(addr string) (ring_client.PoolClient, error) {
		return ingester_client.New(cfg.IngesterClient, addr)
	}

	cf := clientpool.NewPool(
		poolName,
		cfg.IngesterClient.PoolConfig,
		s.ingesterRing,
		ring_client.PoolAddrFunc(internalIngesterClientFactory),
		logger,
		metricsNamespace,
	)

	tenantLimits := map[string]*validation.Limits{
		"fake": {
			ShardStreams: shardstreams.Config{
				Enabled: true,
			},
		},
	}

	limits, err := validation.NewOverrides(cfg.LimitsConfig, newTenantLimits(tenantLimits))
	if err != nil {
		return nil, err
	}

	s.rateStore = distributor.NewRateStore(cfg.RateStore, s.ingesterRing, cf, limits, reg)
	srvs = append(srvs, s.rateStore)

	s.subservices, err = services.NewManager(srvs...)
	if err != nil {
		return nil, errors.Wrap(err, "services manager")
	}
	s.subservicesWatcher = services.NewFailureWatcher()
	s.subservicesWatcher.WatchManager(s.subservices)
	s.Service = services.NewBasicService(s.starting, s.running, s.stopping)

	return s, nil
}

func (s *Service) starting(ctx context.Context) error {
	return services.StartManagerAndAwaitHealthy(ctx, s.subservices)
}

func (s *Service) running(ctx context.Context) error {
	<-ctx.Done()
	return nil
}

func (s *Service) stopping(_ error) error {
	return services.StopManagerAndAwaitStopped(context.Background(), s.subservices)
}

func (s *Service) GetMemberlist() *memberlist.KVInitService {
	return s.memberlistKV
}

func (s *Service) GetIngesterRing() *ring.Ring {
	return s.ingesterRing
}
