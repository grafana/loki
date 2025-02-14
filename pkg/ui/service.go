package ui

import (
	"context"
	"crypto/tls"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/gorilla/mux"
	"github.com/grafana/ckit"
	"github.com/grafana/ckit/peer"
	"github.com/grafana/dskit/ring"
	"github.com/grafana/dskit/services"
	"golang.org/x/net/http2"

	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

// This allows to rate limit the number of updates when the cluster is frequently changing (e.g. during rollout).
const stateUpdateMinInterval = 5 * time.Second

type Service struct {
	services.Service
	node   *ckit.Node
	router *mux.Router
	uiFS   fs.FS

	client    *http.Client
	localAddr string

	cfg    Config
	logger log.Logger
}

func NewService(cfg Config, router *mux.Router, logger log.Logger) (*Service, error) {
	addr, err := ring.GetInstanceAddr(cfg.AdvertiseAddr, cfg.InfNames, util_log.Logger, cfg.EnableIPv6)
	if err != nil {
		return nil, err
	}
	cfg.AdvertiseAddr = addr

	httpClient := &http.Client{
		Transport: &http2.Transport{
			AllowHTTP: true,
			DialTLSContext: func(ctx context.Context, network, addr string, _ *tls.Config) (net.Conn, error) {
				return net.DialTimeout(network, addr, calcTimeout(ctx))
			},
		},
	}
	advertiseAddr := fmt.Sprintf("%s:%d", cfg.AdvertiseAddr, cfg.AdvertisePort)
	node, err := ckit.NewNode(httpClient, ckit.Config{
		Name:          cfg.NodeName,
		Log:           logger,
		AdvertiseAddr: advertiseAddr,
		Label:         cfg.ClusterName,
	})
	if err != nil {
		return nil, err
	}

	svc := &Service{
		cfg:       cfg,
		logger:    logger,
		node:      node,
		router:    router,
		client:    httpClient,
		localAddr: advertiseAddr,
	}
	svc.Service = services.NewBasicService(nil, svc.run, svc.stop)
	if err := svc.initUIFs(); err != nil {
		return nil, err
	}
	svc.RegisterHandler()
	return svc, nil
}

func (s *Service) run(ctx context.Context) error {
	if err := s.node.ChangeState(ctx, peer.StateParticipant); err != nil {
		level.Error(s.logger).Log("msg", "failed to change state to participant", "err", err)
		return err
	}
	peers, err := s.getBootstrapPeers()
	if err != nil {
		// Warn when failed to get peers on startup as it can result in a split brain. We do not fail hard here
		// because it would complicate the process of bootstrapping a new cluster.
		level.Warn(s.logger).Log("msg", "failed to get peers to join at startup; will create a new cluster", "err", err)
	}
	level.Info(s.logger).Log("msg", "starting cluster node", "peers_count", len(peers))
	if err := s.node.Start(peers); err != nil {
		level.Warn(s.logger).Log("msg", "failed to connect to peers; bootstrapping a new cluster", "err", err)

		err := s.node.Start(nil)
		if err != nil {
			level.Error(s.logger).Log("msg", "failed to bootstrap a fresh cluster with no peers", "err", err)
		}
	}

	var wg sync.WaitGroup
	if s.cfg.RejoinInterval > 0 {
		ticker := time.NewTicker(s.cfg.RejoinInterval)
		defer ticker.Stop()

		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					peers, err := s.getBootstrapPeers()
					if err != nil {
						level.Warn(s.logger).Log("msg", "failed to get peers to join; will try again", "err", err)
						continue
					}
					level.Info(s.logger).Log("msg", "rejoining cluster", "peers_count", len(peers))
					if err := s.node.Start(peers); err != nil {
						level.Warn(s.logger).Log("msg", "failed to connect to peers; will try again", "err", err)
						continue
					}
				}
			}
		}()
	}

	<-ctx.Done()
	wg.Wait()
	return nil
}

func (s *Service) stop(_ error) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := s.node.ChangeState(ctx, peer.StateTerminating); err != nil {
		level.Error(s.logger).Log("msg", "failed to change state to terminating", "err", err)
	}
	return s.node.Stop()
}

// findPeerByName returns the peer with the given name visible to the node.
func (s *Service) findPeerByName(name string) (peer.Peer, error) {
	for _, p := range s.node.Peers() {
		if strings.EqualFold(p.Name, name) {
			return p, nil
		}
	}
	return peer.Peer{}, fmt.Errorf("peer not found: %s", name)
}

// TODO(rfratto): consider making the max timeout configurable.
// Set a maximum timeout for establishing the connection. If our
// context has a deadline earlier than our timeout, we shrink the
// timeout to it.
func calcTimeout(ctx context.Context) time.Duration {
	timeout := 30 * time.Second
	if dur, ok := deadlineDuration(ctx); ok && dur < timeout {
		timeout = dur
	}
	return timeout
}

func deadlineDuration(ctx context.Context) (d time.Duration, ok bool) {
	if t, ok := ctx.Deadline(); ok {
		return time.Until(t), true
	}
	return 0, false
}
