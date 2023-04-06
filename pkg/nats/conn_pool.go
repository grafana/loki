package nats

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/ring"
	"github.com/grafana/dskit/services"
	util_log "github.com/grafana/loki/pkg/util/log"
	"github.com/nats-io/nats.go"
	"github.com/prometheus/client_golang/prometheus"
)

type ConnProvider struct {
	cfg Config

	ring               *ring.Ring
	subservicesWatcher *services.FailureWatcher
	subservices        *services.Manager
	services.Service
}

func NewConnProvider(cfg Config) (*ConnProvider, error) {
	ringCfg := cfg.ToRingConfig(1)
	ringCfg.SubringCacheDisabled = true
	ringClient, err := ring.New(ringCfg, "nats", "nats", util_log.Logger, prometheus.WrapRegistererWithPrefix("nats_factory", prometheus.DefaultRegisterer))
	if err != nil {
		return nil, err
	}
	subSvc, err := services.NewManager(ringClient)
	if err != nil {
		return nil, err
	}
	s := &ConnProvider{
		cfg:                cfg,
		ring:               ringClient,
		subservicesWatcher: services.NewFailureWatcher(),
		subservices:        subSvc,
	}
	s.Service = services.NewBasicService(s.starting, s.running, s.stopping)

	return s, nil
}

func (f *ConnProvider) starting(ctx context.Context) error {
	f.subservicesWatcher.WatchManager(f.subservices)

	if err := services.StartManagerAndAwaitHealthy(ctx, f.subservices); err != nil {
		return fmt.Errorf("failed to start subservices: %w", err)
	}

	for {
		_, err := f.GetConn()
		if err != nil {
			level.Warn(util_log.Logger).Log("msg", "waiting for connection to NATS to be valid, will retry ...", "err", err)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(5 * time.Second):
			}
			continue
		}
		break
	}
	return nil
}

func (f *ConnProvider) running(ctx context.Context) error {
	<-ctx.Done()
	return nil
}

func (f *ConnProvider) stopping(_ error) error {
	return services.StopManagerAndAwaitStopped(context.Background(), f.subservices)
}

func (f *ConnProvider) GetConn() (*nats.Conn, error) {
	// todo better discovery and connection pooling
	all, _ := f.ring.GetAllHealthy(ring.NewOp([]ring.InstanceState{ring.ACTIVE}, nil))
	if len(all.Instances) == 0 {
		return nil, fmt.Errorf("no nats instances found")
	}
	var servers string
	for _, instance := range all.Instances {
		// remove port from Addr and use client port from NATS Config
		servers += fmt.Sprintf("nats://%s:%d,", strings.Split(instance.Addr, ":")[0], f.cfg.ClientPort)
	}

	// // Optionally set ReconnectWait and MaxReconnect attempts.
	// // This example means 10 seconds total per backend.
	// nc, err = nats.Connect(servers, nats.MaxReconnects(5), nats.ReconnectWait(2 * time.Second))

	return nats.Connect(servers)
}
