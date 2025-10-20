package ui

import (
	"context"
	"crypto/tls"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/gorilla/mux"
	"github.com/grafana/dskit/ring"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"
	"golang.org/x/net/http2"

	// This is equivalent to a main.go for the Loki UI, so the blank import is allowed
	_ "github.com/go-sql-driver/mysql" //nolint:revive

	"github.com/grafana/loki/v3/pkg/goldfish"
)

// This allows to rate limit the number of updates when the cluster is frequently changing (e.g. during rollout).
const stateUpdateMinInterval = 5 * time.Second

type Service struct {
	services.Service
	ring          *ring.Ring
	localNodeName string
	router        *mux.Router
	uiFS          fs.FS

	client    *http.Client
	localAddr string

	cfg             Config
	logger          log.Logger
	reg             prometheus.Registerer
	goldfishStorage goldfish.Storage
	goldfishMetrics *GoldfishMetrics

	now func() time.Time
}

func NewService(cfg Config, router *mux.Router, ring *ring.Ring, localAddr string, logger log.Logger, reg prometheus.Registerer) (*Service, error) {
	httpClient := &http.Client{
		Transport: &http2.Transport{
			AllowHTTP: true,
			DialTLSContext: func(ctx context.Context, network, addr string, _ *tls.Config) (net.Conn, error) {
				return net.DialTimeout(network, addr, calcTimeout(ctx))
			},
		},
	}

	// Use the instance ID from ring config as the local node name
	localNodeName := cfg.Ring.InstanceID

	if !cfg.Debug {
		logger = level.NewFilter(logger, level.AllowInfo())
	}

	svc := &Service{
		cfg:           cfg,
		logger:        logger,
		reg:           reg,
		ring:          ring,
		localNodeName: localNodeName,
		router:        router,
		client:        httpClient,
		localAddr:     localAddr,
		now:           time.Now,
	}

	// Initialize metrics if goldfish is enabled
	if cfg.Goldfish.Enable {
		svc.goldfishMetrics = NewGoldfishMetrics(reg)
	}

	svc.Service = services.NewBasicService(nil, svc.run, svc.stop)
	if err := svc.initGoldfishDB(); err != nil {
		return nil, err
	}
	svc.RegisterHandler()
	return svc, nil
}

func (s *Service) run(ctx context.Context) error {
	level.Info(s.logger).Log("msg", "UI service running", "node", s.localNodeName, "addr", s.localAddr)

	<-ctx.Done()
	return nil
}

func (s *Service) stop(_ error) error {
	level.Info(s.logger).Log("msg", "stopping UI service")
	if s.goldfishStorage != nil {
		s.goldfishStorage.Close()
	}
	return nil
}

// findNodeAddressByName returns the HTTP address of the UI instance with the given ID.
// It queries the UI ring to find the instance address.
func (s *Service) findNodeAddressByName(instanceID string) (string, error) {
	// Query all rings to find the instance
	replicationSet, err := s.ring.GetAllHealthy(ring.Read)
	if err != nil {
		return "", fmt.Errorf("failed to get instances from ring: %w", err)
	}

	// Look for the instance by ID
	for _, instance := range replicationSet.Instances {
		if strings.EqualFold(instance.Id, instanceID) {
			return instance.Addr, nil
		}
	}

	return "", fmt.Errorf("instance not found in ring: %s", instanceID)
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
		level.Info(s.logger).Log("msg", "goldfish feature disabled, using noop storage")
		s.goldfishStorage = goldfish.NewNoopStorage()
		return nil
	}

	// Create storage configuration
	storageConfig := goldfish.StorageConfig{
		Type:             "cloudsql",
		CloudSQLHost:     s.cfg.Goldfish.CloudSQLHost,
		CloudSQLPort:     s.cfg.Goldfish.CloudSQLPort,
		CloudSQLDatabase: s.cfg.Goldfish.CloudSQLDatabase,
		CloudSQLUser:     s.cfg.Goldfish.CloudSQLUser,
		MaxConnections:   s.cfg.Goldfish.MaxConnections,
		MaxIdleTime:      s.cfg.Goldfish.MaxIdleTime,
	}

	storage, err := goldfish.NewMySQLStorage(storageConfig, s.logger)
	if err != nil {
		level.Warn(s.logger).Log("msg", "failed to create goldfish storage, goldfish features will be disabled", "err", err)
		s.cfg.Goldfish.Enable = false
		s.goldfishStorage = goldfish.NewNoopStorage()
		return nil
	}

	s.goldfishStorage = storage
	level.Info(s.logger).Log("msg", "goldfish storage initialized successfully")
	return nil
}
