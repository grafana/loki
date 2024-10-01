package metastore

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/coder/quartz"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/flagext"
	"github.com/grafana/dskit/services"
	"github.com/hashicorp/raft"
	raftwal "github.com/hashicorp/raft-wal"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/v3/pkg/ingester-rf1/metastore/health"
	"github.com/grafana/loki/v3/pkg/ingester-rf1/metastore/metastorepb"
	"github.com/grafana/loki/v3/pkg/ingester-rf1/metastore/raftleader"
)

const metastoreRaftLeaderHealthServiceName = "metastorepb.MetastoreService.RaftLeader"

type Config struct {
	DataDir string     `yaml:"data_dir"`
	Raft    RaftConfig `yaml:"raft"`
}

type RaftConfig struct {
	Dir string `yaml:"dir"`

	BootstrapPeers   []string `yaml:"bootstrap_peers"`
	ServerID         string   `yaml:"server_id"`
	BindAddress      string   `yaml:"bind_address"`
	AdvertiseAddress string   `yaml:"advertise_address"`

	ApplyTimeout time.Duration `yaml:"apply_timeout" doc:"hidden"`
}

func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	const prefix = "metastore."
	f.StringVar(&cfg.DataDir, prefix+"data-dir", "./data-metastore/data", "")
	cfg.Raft.RegisterFlags(f)
}

func (cfg *RaftConfig) RegisterFlags(f *flag.FlagSet) {
	const prefix = "metastore.raft."
	f.StringVar(&cfg.Dir, prefix+"dir", "./data-metastore/raft", "")
	f.Var((*flagext.StringSlice)(&cfg.BootstrapPeers), prefix+"bootstrap-peers", "")
	f.StringVar(&cfg.BindAddress, prefix+"bind-address", "localhost:9099", "")
	f.StringVar(&cfg.ServerID, prefix+"server-id", "localhost:9099", "")
	f.StringVar(&cfg.AdvertiseAddress, prefix+"advertise-address", "localhost:9099", "")
	f.DurationVar(&cfg.ApplyTimeout, prefix+"apply-timeout", 5*time.Second, "")
}

type Metastore struct {
	services.Service
	metastorepb.MetastoreServiceServer

	config Config
	logger log.Logger
	reg    prometheus.Registerer

	// In-memory state.
	state *metastoreState

	// Persistent state.
	db *boltdb

	// Raft module.
	wal          *raftwal.WAL
	snapshots    *raft.FileSnapshotStore
	transport    *raft.NetworkTransport
	raft         *raft.Raft
	leaderhealth *raftleader.HealthObserver

	logStore      raft.LogStore
	stableStore   raft.StableStore
	snapshotStore raft.SnapshotStore

	walDir string

	done chan struct{}
	wg   sync.WaitGroup

	// Used in tests.
	clock quartz.Clock
}

func New(config Config, logger log.Logger, reg prometheus.Registerer, hs health.Service) (*Metastore, error) {
	m := &Metastore{
		config: config,
		logger: logger,
		reg:    reg,
		db:     newDB(config, logger),
		done:   make(chan struct{}),
		clock:  quartz.NewReal(),
	}
	m.leaderhealth = raftleader.NewRaftLeaderHealthObserver(hs, logger)
	m.state = newMetastoreState(logger, m.db)
	m.Service = services.NewBasicService(m.starting, m.running, m.stopping)
	return m, nil
}

func (m *Metastore) Shutdown() error {
	m.shutdownRaft()
	m.db.shutdown()
	return nil
}

func (m *Metastore) starting(_ context.Context) error {
	if err := m.db.open(false); err != nil {
		return fmt.Errorf("failed to initialize database: %w", err)
	}
	if err := m.initRaft(); err != nil {
		return fmt.Errorf("failed to initialize raft: %w", err)
	}
	m.wg.Add(1)
	go m.cleanupLoop()
	return nil
}

func (m *Metastore) stopping(_ error) error {
	close(m.done)
	m.wg.Wait()
	return m.Shutdown()
}

func (m *Metastore) running(ctx context.Context) error {
	<-ctx.Done()
	return nil
}

const (
	snapshotsRetain       = 3
	walCacheEntries       = 512
	transportConnPoolSize = 10
	transportTimeout      = 10 * time.Second

	raftTrailingLogs      = 32 << 10
	raftSnapshotInterval  = 300 * time.Second
	raftSnapshotThreshold = 16 << 10
)

func (m *Metastore) initRaft() (err error) {
	defer func() {
		if err != nil {
			// If the initialization fails, initialized components
			// should be de-initialized gracefully.
			m.shutdownRaft()
		}
	}()

	hasState, err := m.openRaftStore()
	if err != nil {
		return err
	}

	addr, err := net.ResolveTCPAddr("tcp", m.config.Raft.AdvertiseAddress)
	if err != nil {
		return err
	}
	m.transport, err = raft.NewTCPTransport(m.config.Raft.BindAddress, addr, transportConnPoolSize, transportTimeout, os.Stderr)
	if err != nil {
		return err
	}

	config := raft.DefaultConfig()
	// TODO: Wrap gokit
	//	config.Logger
	config.LogLevel = "debug"
	config.TrailingLogs = raftTrailingLogs
	config.SnapshotThreshold = raftSnapshotThreshold
	config.SnapshotInterval = raftSnapshotInterval
	// TODO: We don't need to restore the latest snapshot
	//  on start, because the FSM is already disk-based.
	//	config.NoSnapshotRestoreOnStart = true
	config.LocalID = raft.ServerID(m.config.Raft.ServerID)

	fsm := newFSM(m.logger, m.db, m.state)
	m.raft, err = raft.NewRaft(config, fsm, m.logStore, m.stableStore, m.snapshotStore, m.transport)
	if err != nil {
		return fmt.Errorf("starting raft node: %w", err)
	}

	if !hasState {
		_ = level.Warn(m.logger).Log("msg", "no existing state found, bootstrapping cluster")
		if err = m.bootstrap(); err != nil {
			return fmt.Errorf("failed to bootstrap cluster: %w", err)
		}
	}

	m.leaderhealth.Register(m.raft, metastoreRaftLeaderHealthServiceName)
	return nil
}

func (m *Metastore) openRaftStore() (hasState bool, err error) {
	if err = m.createRaftDirs(); err != nil {
		return false, err
	}
	m.wal, err = raftwal.Open(m.walDir)
	if err != nil {
		return false, fmt.Errorf("failed to open WAL: %w", err)
	}
	m.snapshots, err = raft.NewFileSnapshotStore(m.config.Raft.Dir, snapshotsRetain, os.Stderr)
	if err != nil {
		return false, fmt.Errorf("failed to open shapshot store: %w", err)
	}
	m.logStore = m.wal
	m.logStore, _ = raft.NewLogCache(walCacheEntries, m.logStore)
	m.stableStore = m.wal
	m.snapshotStore = m.snapshots
	if hasState, err = raft.HasExistingState(m.logStore, m.stableStore, m.snapshotStore); err != nil {
		return hasState, fmt.Errorf("failed to check for existing state: %w", err)
	}
	return hasState, nil
}

func (m *Metastore) createRaftDirs() (err error) {
	m.walDir = filepath.Join(m.config.Raft.Dir, "wal")
	if err = os.MkdirAll(m.walDir, 0o755); err != nil {
		return fmt.Errorf("WAL dir: %w", err)
	}
	if err = os.MkdirAll(m.config.Raft.Dir, 0o755); err != nil {
		return fmt.Errorf("snapshot directory: %w", err)
	}
	return nil
}

func (m *Metastore) shutdownRaft() {
	if m.raft != nil {
		// If raft has been initialized, try to transfer leadership.
		// Only after this we remove the leader health observer and
		// shutdown the raft.
		// There is a chance that client will still be trying to connect
		// to this instance, therefore retrying is still required.
		if err := m.raft.LeadershipTransfer().Error(); err != nil {
			switch {
			case errors.Is(err, raft.ErrNotLeader):
				// Not a leader, nothing to do.
			case strings.Contains(err.Error(), "cannot find peer"):
				// It's likely that there's just one node in the cluster.
			default:
				_ = level.Error(m.logger).Log("msg", "failed to transfer leadership", "err", err)
			}
		}
		m.leaderhealth.Deregister(m.raft, metastoreRaftLeaderHealthServiceName)
		if err := m.raft.Shutdown().Error(); err != nil {
			_ = level.Error(m.logger).Log("msg", "failed to shutdown raft", "err", err)
		}
	}
	if m.transport != nil {
		if err := m.transport.Close(); err != nil {
			_ = level.Error(m.logger).Log("msg", "failed to close transport", "err", err)
		}
	}
	if m.wal != nil {
		if err := m.wal.Close(); err != nil {
			_ = level.Error(m.logger).Log("msg", "failed to close WAL", "err", err)
		}
	}
}
