package ui

import (
	"context"
	"crypto/tls"
	"database/sql"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
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
	"github.com/prometheus/client_golang/prometheus"
	"golang.org/x/net/http2"

	// This is the equivilent of the main.go of the Loki UI, hence why we allo the blank import here
	_ "github.com/go-sql-driver/mysql" //nolint:revive
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

	cfg        Config
	logger     log.Logger
	reg        prometheus.Registerer
	goldfishDB *sql.DB
}

func NewService(cfg Config, router *mux.Router, logger log.Logger, reg prometheus.Registerer) (*Service, error) {
	addr, err := ring.GetInstanceAddr(cfg.AdvertiseAddr, cfg.InfNames, logger, cfg.EnableIPv6)
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

	if !cfg.Debug {
		logger = level.NewFilter(logger, level.AllowInfo())
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
	if reg != nil {
		if err := reg.Register(node.Metrics()); err != nil {
			return nil, err
		}
	}

	svc := &Service{
		cfg:       cfg,
		logger:    logger,
		reg:       reg,
		node:      node,
		router:    router,
		client:    httpClient,
		localAddr: advertiseAddr,
	}
	svc.Service = services.NewBasicService(nil, svc.run, svc.stop)
	if err := svc.initUIFs(); err != nil {
		return nil, err
	}
	if err := svc.initGoldfishDB(); err != nil {
		return nil, err
	}
	svc.RegisterHandler()
	return svc, nil
}

func (s *Service) run(ctx context.Context) error {
	var joinOnce sync.Once

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
	newPeers := make(map[string]struct{})
	for _, p := range peers {
		newPeers[p] = struct{}{}
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
					peers, err := s.discoverNewPeers(newPeers)
					if err != nil {
						level.Warn(s.logger).Log("msg", "failed to get peers to join; will try again", "err", err)
						continue
					}
					if len(peers) > 0 {
						level.Info(s.logger).Log("msg", "rejoining cluster", "peers_count", len(newPeers))
						if err := s.node.Start(peers); err != nil {
							level.Warn(s.logger).Log("msg", "failed to connect to peers; will try again", "err", err)
							continue
						}
					}

					// Only change state to participant after we've had a chance to join
					// the cluster. This is an optional small optimization to reduce the
					// total amount of network traffic required to synchronize with the
					// cluster state; see [ckit.Node.ChangeState] for more details.
					joinOnce.Do(func() {
						if err := s.node.ChangeState(ctx, peer.StateParticipant); err != nil {
							level.Error(s.logger).Log("msg", "failed to change state to participant", "err", err)

							// ChangeState only fails when making an invalid state
							// transition. We can log the error but otherwise safely avoid
							// it, since the error means that either:
							//
							// 1. We're already in the participant state, or
							// 2. We're immediately terminating and shouldn't change state.
						}
					})
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
	if s.reg != nil {
		s.reg.Unregister(s.node.Metrics())
	}
	if s.goldfishDB != nil {
		s.goldfishDB.Close()
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

// initGoldfishDB initializes the database connection for goldfish features
func (s *Service) initGoldfishDB() error {
	if !s.cfg.Goldfish.Enable {
		level.Info(s.logger).Log("msg", "goldfish feature disabled, skipping database initialization")
		return nil
	}

	password := os.Getenv("GOLDFISH_DB_PASSWORD")
	if password == "" {
		return fmt.Errorf("CloudSQL password must be provided via GOLDFISH_DB_PASSWORD environment variable")
	}

	// Build DSN for CloudSQL proxy connection
	// MySQL DSN format: username:password@tcp(host:port)/dbname?params
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?parseTime=true&charset=utf8mb4&collation=utf8mb4_unicode_ci",
		s.cfg.Goldfish.CloudSQLUser,
		password,
		s.cfg.Goldfish.CloudSQLHost,
		s.cfg.Goldfish.CloudSQLPort,
		s.cfg.Goldfish.CloudSQLDatabase,
	)

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}

	// Configure connection pool
	db.SetMaxOpenConns(s.cfg.Goldfish.MaxConnections)
	db.SetMaxIdleConns(s.cfg.Goldfish.MaxConnections / 2)
	db.SetConnMaxIdleTime(time.Duration(s.cfg.Goldfish.MaxIdleTime) * time.Second)

	// Verify connection
	if err := db.Ping(); err != nil {
		db.Close()
		return fmt.Errorf("failed to ping database: %w", err)
	}

	// Verify tables exist (panic if they don't)
	if err := s.verifyGoldfishTables(db); err != nil {
		db.Close()
		panic(fmt.Sprintf("goldfish tables not found: %v", err))
	}

	s.goldfishDB = db
	level.Info(s.logger).Log("msg", "goldfish database connection initialized successfully")
	return nil
}

// verifyGoldfishTables checks if the required goldfish tables exist
func (s *Service) verifyGoldfishTables(db *sql.DB) error {
	tables := []string{"sampled_queries", "comparison_outcomes"}

	for _, table := range tables {
		query := fmt.Sprintf("SELECT 1 FROM %s LIMIT 1", table)
		_, err := db.Exec(query)
		if err != nil {
			return fmt.Errorf("table %s not found or not accessible: %w", table, err)
		}
	}

	return nil
}
