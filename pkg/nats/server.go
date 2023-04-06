package nats

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"time"

	"github.com/grafana/dskit/kv"
	"github.com/grafana/dskit/ring"
	"github.com/grafana/dskit/services"
	nats_server "github.com/nats-io/nats-server/v2/server"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/automaxprocs/maxprocs"

	util_log "github.com/grafana/loki/pkg/util/log"
)

type Server struct {
	services.Service
	subservices        *services.Manager
	subservicesWatcher *services.FailureWatcher
	ring               *ring.Ring

	server *nats_server.Server
	opts   *nats_server.Options
}

func NewServer(cfg Config) (services.Service, error) {
	cfg.InstancePort = cfg.ClusterPort
	ringStore, err := kv.NewClient(
		cfg.KVStore,
		ring.GetCodec(),
		kv.RegistererWithKVName(prometheus.WrapRegistererWithPrefix("loki_", prometheus.DefaultRegisterer), "nats"),
		util_log.Logger,
	)
	if err != nil {
		return nil, err
	}
	lcCfg, err := cfg.RingConfig.ToLifecyclerConfig(1, util_log.Logger)
	if err != nil {
		return nil, err
	}
	var delegate ring.BasicLifecyclerDelegate
	delegate = ring.NewInstanceRegisterDelegate(ring.ACTIVE, lcCfg.NumTokens)
	delegate = ring.NewLeaveOnStoppingDelegate(delegate, util_log.Logger)
	delegate = ring.NewAutoForgetDelegate(4*lcCfg.HeartbeatTimeout, delegate, util_log.Logger)
	lc, err := ring.NewBasicLifecycler(lcCfg, "nats", "nats", ringStore, delegate, util_log.Logger, prometheus.WrapRegistererWithPrefix("nats_", prometheus.DefaultRegisterer))
	if err != nil {
		return nil, err
	}
	ringCfg := cfg.ToRingConfig(1)
	ringCfg.SubringCacheDisabled = true
	ringClient, err := ring.New(ringCfg, "nats", "nats", util_log.Logger, prometheus.DefaultRegisterer)
	if err != nil {
		return nil, err
	}
	opts := &nats_server.Options{
		Port:       cfg.ClientPort,
		ServerName: cfg.InstanceID,
		Accounts:   []*nats_server.Account{nats_server.NewAccount("$SYS")},
		Users: []*nats_server.User{
			{Username: "loki", Password: "loki", Account: nats_server.NewAccount("$SYS")},
		},
	}
	if cfg.Cluster {
		opts.Cluster = nats_server.ClusterOpts{
			Name:      "loki-nats-cluster",
			Advertise: cfg.InstanceAddr + ":" + strconv.Itoa(cfg.InstancePort),
			Port:      cfg.InstancePort,
		}
	}
	if cfg.DataPath != "" {
		opts.StoreDir = cfg.DataPath
		opts.JetStream = true
	}
	natsServer := nats_server.New(opts)
	if natsServer == nil {
		return nil, fmt.Errorf("failed to create nats server")
	}
	natsServer.SetLoggerV2(NewNATSLogger(util_log.Logger), true, cfg.TraceLogging, true)
	subSvc, err := services.NewManager(lc, ringClient)
	if err != nil {
		return nil, err
	}
	s := &Server{
		server:             natsServer,
		subservicesWatcher: services.NewFailureWatcher(),
		subservices:        subSvc,
		opts:               opts,
		ring:               ringClient,
	}

	s.Service = services.NewBasicService(s.starting, s.running, s.stopping)
	return s, nil
}

func (s *Server) starting(ctx context.Context) error {
	s.subservicesWatcher.WatchManager(s.subservices)

	if err := services.StartManagerAndAwaitHealthy(ctx, s.subservices); err != nil {
		return fmt.Errorf("failed to start subservices: %w", err)
	}
	all, _ := s.ring.GetAllHealthy(ring.NewOp([]ring.InstanceState{ring.ACTIVE}, nil))

	routes := make([]*url.URL, 0, len(all.Instances))
	for _, instance := range all.Instances {
		// Skip ourselves.
		if instance.Addr == s.opts.Cluster.Advertise {
			continue
		}
		u, err := url.Parse("nats://" + instance.Addr)
		if err != nil {
			continue
		}
		routes = append(routes, u)
	}
	s.opts.Routes = routes
	return nil
}

func (s *Server) running(ctx context.Context) error {
	go func() {
		<-ctx.Done()
		s.server.Shutdown()
	}()

	// Reload routes if none are configured.
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(5 * time.Second):
				if len(s.opts.Routes) > 0 {
					continue
				}
				// todo we should compare and remove dead routes.
				all, _ := s.ring.GetAllHealthy(ring.NewOp([]ring.InstanceState{ring.ACTIVE}, nil))
				routes := make([]*url.URL, 0, len(all.Instances))
				for _, instance := range all.Instances {
					// Skip ourselves.
					if instance.Addr == s.opts.Cluster.Advertise {
						continue
					}
					u, err := url.Parse("nats://" + instance.Addr)
					if err != nil {
						continue
					}
					routes = append(routes, u)
				}
				if len(routes) == 0 {
					continue
				}
				s.opts.Routes = routes
				s.server.ReloadOptions(s.opts)
			}
		}
	}()
	if err := nats_server.Run(s.server); err != nil {
		return err
	}
	// Adjust MAXPROCS if running under linux/cgroups quotas.
	undo, err := maxprocs.Set(maxprocs.Logger(s.server.Debugf))
	if err != nil {
		s.server.Warnf("Failed to set GOMAXPROCS: %v", err)
	} else {
		defer undo()
	}

	s.server.WaitForShutdown()
	return nil
}

func (s *Server) stopping(_ error) error {
	return services.StopManagerAndAwaitStopped(context.Background(), s.subservices)
}
