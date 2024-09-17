package ingester

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/kv"
	"github.com/grafana/dskit/ring"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc/health/grpc_health_v1"

	"github.com/grafana/loki/v3/pkg/kafka"
	"github.com/grafana/loki/v3/pkg/kafka/ingester/shutdownmarker"
	"github.com/grafana/loki/v3/pkg/kafka/partitionring"
	util_log "github.com/grafana/loki/v3/pkg/util/log"

	"github.com/grafana/loki/v3/pkg/util"
)

const (
	RingName          = "kafka-ingester"
	PartitionRingName = "kafka-partition"
)

var (
	ingesterIDRegexp           = regexp.MustCompile("-([0-9]+)$")
	defaultFlushInterval       = 15 * time.Second
	defaultFlushSize     int64 = 300 << 20 // 300 MB
)

// Config for an ingester.
type Config struct {
	Enabled             bool                  `yaml:"enabled" doc:"description=Whether the kafka ingester is enabled."`
	LifecyclerConfig    ring.LifecyclerConfig `yaml:"lifecycler,omitempty" doc:"description=Configures how the lifecycle of the ingester will operate and where it will register for discovery."`
	ShutdownMarkerPath  string                `yaml:"shutdown_marker_path"`
	FlushInterval       time.Duration         `yaml:"flush_interval" doc:"description=The interval at which the ingester will flush and commit offsets to Kafka. If not set, the default flush interval will be used."`
	FlushSize           int64                 `yaml:"flush_size" doc:"description=The size at which the ingester will flush and commit offsets to Kafka. If not set, the default flush size will be used."`
	PartitionRingConfig partitionring.Config  `yaml:"partition_ring" category:"experimental"`
	KafkaConfig         kafka.Config          `yaml:"-"`
}

// RegisterFlags registers the flags.
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.LifecyclerConfig.RegisterFlagsWithPrefix("kafka-ingester", f, util_log.Logger)
	cfg.PartitionRingConfig.RegisterFlags(f)
	f.StringVar(&cfg.ShutdownMarkerPath, "kafka-ingester.shutdown-marker-path", "", "Path where the shutdown marker file is stored. If not set and common.path_prefix is set then common.path_prefix will be used.")
	f.BoolVar(&cfg.Enabled, "kafka-ingester.enabled", false, "Whether the Kafka-based ingester path is enabled")
	f.DurationVar(&cfg.FlushInterval, "kafka-ingester.flush-interval", defaultFlushInterval, "The interval at which the ingester will flush and commit offsets to Kafka. If not set, the default flush interval will be used.")
	f.Int64Var(&cfg.FlushSize, "kafka-ingester.flush-size", defaultFlushSize, "The size at which the ingester will flush and commit offsets to Kafka. If not set, the default flush size will be used.")
}

func (cfg *Config) Validate() error {
	if !cfg.Enabled {
		return nil
	}
	if cfg.FlushInterval <= 0 {
		return errors.New("kafka-ingester.flush-interval must be greater than 0")
	}
	if cfg.LifecyclerConfig.RingConfig.ReplicationFactor != 1 {
		cfg.LifecyclerConfig.RingConfig.ReplicationFactor = 1
		level.Warn(util_log.Logger).Log("msg", "kafka-ingester.lifecycler.replication-factor has been set to 1. This is the only supported replication factor for the kafka-ingester.")
	}
	return nil
}

type Wrapper interface {
	Wrap(wrapped Interface) Interface
}

// Interface is an interface for the Ingester
type Interface interface {
	services.Service
	http.Handler
	CheckReady(ctx context.Context) error
	FlushHandler(w http.ResponseWriter, _ *http.Request)
}

// Ingester builds chunks for incoming log streams.
type Ingester struct {
	services.Service

	cfg    Config
	logger log.Logger

	metrics *ingesterMetrics

	lifecycler              *ring.Lifecycler
	lifecyclerWatcher       *services.FailureWatcher
	ingesterPartitionID     int32
	partitionRingLifecycler *ring.PartitionInstanceLifecycler
	partitionReader         *PartitionReader
}

// New makes a new Ingester.
func New(cfg Config,
	consumerFactory ConsumerFactory,
	logger log.Logger,
	metricsNamespace string,
	registerer prometheus.Registerer,
) (*Ingester, error) {
	metrics := newIngesterMetrics(registerer)

	ingesterPartitionID, err := extractIngesterPartitionID(cfg.LifecyclerConfig.ID)
	if err != nil {
		return nil, fmt.Errorf("calculating ingester partition ID: %w", err)
	}

	partitionRingKV := cfg.PartitionRingConfig.KVStore.Mock
	if partitionRingKV == nil {
		partitionRingKV, err = kv.NewClient(cfg.PartitionRingConfig.KVStore, ring.GetPartitionRingCodec(), kv.RegistererWithKVName(registerer, PartitionRingName+"-lifecycler"), logger)
		if err != nil {
			return nil, fmt.Errorf("creating KV store for ingester partition ring: %w", err)
		}
	}

	partitionRingLifecycler := ring.NewPartitionInstanceLifecycler(
		cfg.PartitionRingConfig.ToLifecyclerConfig(ingesterPartitionID, cfg.LifecyclerConfig.ID),
		PartitionRingName,
		PartitionRingName+"-key",
		partitionRingKV,
		logger,
		prometheus.WrapRegistererWithPrefix("loki_", registerer))
	i := &Ingester{
		cfg:                     cfg,
		logger:                  logger,
		ingesterPartitionID:     ingesterPartitionID,
		partitionRingLifecycler: partitionRingLifecycler,
		metrics:                 metrics,
	}

	i.lifecycler, err = ring.NewLifecycler(cfg.LifecyclerConfig, i, RingName, RingName+"-ring", true, logger, prometheus.WrapRegistererWithPrefix(metricsNamespace+"_", registerer))
	if err != nil {
		return nil, err
	}
	i.partitionReader, err = NewPartitionReader(cfg.KafkaConfig, ingesterPartitionID, cfg.LifecyclerConfig.ID, consumerFactory, logger, registerer)
	if err != nil {
		return nil, err
	}

	i.lifecyclerWatcher = services.NewFailureWatcher()
	i.lifecyclerWatcher.WatchService(i.lifecycler)
	i.lifecyclerWatcher.WatchService(i.partitionRingLifecycler)
	i.lifecyclerWatcher.WatchService(i.partitionReader)

	i.Service = services.NewBasicService(i.starting, i.running, i.stopping)

	return i, nil
}

// ingesterPartitionID returns the partition ID owner the the given ingester.
func extractIngesterPartitionID(ingesterID string) (int32, error) {
	if strings.Contains(ingesterID, "local") {
		return 0, nil
	}

	match := ingesterIDRegexp.FindStringSubmatch(ingesterID)
	if len(match) == 0 {
		return 0, fmt.Errorf("ingester ID %s doesn't match regular expression %q", ingesterID, ingesterIDRegexp.String())
	}
	// Parse the ingester sequence number.
	ingesterSeq, err := strconv.Atoi(match[1])
	if err != nil {
		return 0, fmt.Errorf("no ingester sequence number in ingester ID %s", ingesterID)
	}

	return int32(ingesterSeq), nil
}

// ServeHTTP implements the pattern ring status page.
func (i *Ingester) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	i.lifecycler.ServeHTTP(w, r)
}

func (i *Ingester) starting(ctx context.Context) (err error) {
	defer func() {
		if err != nil {
			// if starting() fails for any reason (e.g., context canceled),
			// the lifecycler must be stopped.
			_ = services.StopAndAwaitTerminated(context.Background(), i.lifecycler)
		}
	}()

	// First of all we have to check if the shutdown marker is set. This needs to be done
	// as first thing because, if found, it may change the behaviour of the ingester startup.
	if exists, err := shutdownmarker.Exists(shutdownmarker.GetPath(i.cfg.ShutdownMarkerPath)); err != nil {
		return fmt.Errorf("failed to check ingester shutdown marker: %w", err)
	} else if exists {
		level.Info(i.logger).Log("msg", "detected existing shutdown marker, setting unregister and flush on shutdown", "path", shutdownmarker.GetPath(i.cfg.ShutdownMarkerPath))
		i.setPrepareShutdown()
	}

	// pass new context to lifecycler, so that it doesn't stop automatically when Ingester's service context is done
	err = i.lifecycler.StartAsync(context.Background())
	if err != nil {
		return err
	}

	err = i.lifecycler.AwaitRunning(ctx)
	if err != nil {
		return err
	}

	err = i.partitionRingLifecycler.StartAsync(context.Background())
	if err != nil {
		return err
	}
	err = i.partitionRingLifecycler.AwaitRunning(ctx)
	if err != nil {
		return err
	}
	err = i.partitionReader.StartAsync(context.Background())
	if err != nil {
		return err
	}
	err = i.partitionReader.AwaitRunning(ctx)
	if err != nil {
		return err
	}

	return nil
}

func (i *Ingester) running(ctx context.Context) error {
	var serviceError error
	select {
	// wait until service is asked to stop
	case <-ctx.Done():
	// stop
	case err := <-i.lifecyclerWatcher.Chan():
		serviceError = fmt.Errorf("lifecycler failed: %w", err)
	}

	return serviceError
}

// stopping is called when Ingester transitions to Stopping state.
//
// At this point, loop no longer runs, but flushers are still running.
func (i *Ingester) stopping(_ error) error {
	var errs util.MultiError

	errs.Add(services.StopAndAwaitTerminated(context.Background(), i.partitionReader))
	errs.Add(services.StopAndAwaitTerminated(context.Background(), i.lifecycler))
	errs.Add(services.StopAndAwaitTerminated(context.Background(), i.partitionRingLifecycler))
	// Remove the shutdown marker if it exists since we are shutting down
	shutdownMarkerPath := shutdownmarker.GetPath(i.cfg.ShutdownMarkerPath)
	exist, err := shutdownmarker.Exists(shutdownMarkerPath)
	if err != nil {
		level.Warn(i.logger).Log("msg", "failed to check for prepare-shutdown marker file", "path", shutdownMarkerPath, "err", err)
	} else if exist {
		if err := shutdownmarker.Remove(shutdownMarkerPath); err != nil {
			level.Warn(i.logger).Log("msg", "failed to remove shutdown marker", "path", shutdownMarkerPath, "err", err)
		}
	}
	return errs.Err()
}

// Watch implements grpc_health_v1.HealthCheck.
func (*Ingester) Watch(*grpc_health_v1.HealthCheckRequest, grpc_health_v1.Health_WatchServer) error {
	return nil
}

func (i *Ingester) PreparePartitionDownscaleHandler(w http.ResponseWriter, r *http.Request) {
	logger := log.With(i.logger, "partition", i.ingesterPartitionID)
	// Don't allow callers to change the shutdown configuration while we're in the middle
	// of starting or shutting down.
	if i.State() != services.Running {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}

	shutdownMarkerPath := shutdownmarker.GetPath(i.cfg.ShutdownMarkerPath)
	exists, err := shutdownmarker.Exists(shutdownMarkerPath)
	if err != nil {
		level.Error(i.logger).Log("msg", "unable to check for prepare-shutdown marker file", "path", shutdownMarkerPath, "err", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	switch r.Method {
	case http.MethodPost:
		// It's not allowed to prepare the downscale while in PENDING state. Why? Because if the downscale
		// will be later cancelled, we don't know if it was requested in PENDING or ACTIVE state, so we
		// don't know to which state reverting back. Given a partition is expected to stay in PENDING state
		// for a short period, we simply don't allow this case.
		state, _, err := i.partitionRingLifecycler.GetPartitionState(r.Context())
		if err != nil {
			level.Error(logger).Log("msg", "failed to check partition state in the ring", "err", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		if state == ring.PartitionPending {
			level.Warn(logger).Log("msg", "received a request to prepare partition for shutdown, but the request can't be satisfied because the partition is in PENDING state")
			w.WriteHeader(http.StatusConflict)
			return
		}

		if err := i.partitionRingLifecycler.ChangePartitionState(r.Context(), ring.PartitionInactive); err != nil {
			level.Error(logger).Log("msg", "failed to change partition state to inactive", "err", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if !exists {
			if err := shutdownmarker.Create(shutdownMarkerPath); err != nil {
				level.Error(i.logger).Log("msg", "unable to create prepare-shutdown marker file", "path", shutdownMarkerPath, "err", err)
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
		}
		i.setPrepareShutdown()

	case http.MethodDelete:
		state, _, err := i.partitionRingLifecycler.GetPartitionState(r.Context())
		if err != nil {
			level.Error(logger).Log("msg", "failed to check partition state in the ring", "err", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		// If partition is inactive, make it active. We ignore other states Active and especially Pending.
		if state == ring.PartitionInactive {

			// We don't switch it back to PENDING state if there are not enough owners because we want to guarantee consistency
			// in the read path. If the partition is within the lookback period we need to guarantee that partition will be queried.
			// Moving back to PENDING will cause us loosing consistency, because PENDING partitions are not queried by design.
			// We could move back to PENDING if there are not enough owners and the partition moved to INACTIVE more than
			// "lookback period" ago, but since we delete inactive partitions with no owners that moved to inactive since longer
			// than "lookback period" ago, it looks to be an edge case not worth to address.
			if err := i.partitionRingLifecycler.ChangePartitionState(r.Context(), ring.PartitionActive); err != nil {
				level.Error(logger).Log("msg", "failed to change partition state to active", "err", err)
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			if exists {
				if err := shutdownmarker.Remove(shutdownMarkerPath); err != nil {
					level.Error(i.logger).Log("msg", "unable to remove prepare-shutdown marker file", "path", shutdownMarkerPath, "err", err)
					w.WriteHeader(http.StatusInternalServerError)
					return
				}
			}
			i.unsetPrepareShutdown()
		}
	}

	state, stateTimestamp, err := i.partitionRingLifecycler.GetPartitionState(r.Context())
	if err != nil {
		level.Error(logger).Log("msg", "failed to check partition state in the ring", "err", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	if state == ring.PartitionInactive {
		util.WriteJSONResponse(w, map[string]any{"timestamp": stateTimestamp.Unix()})
	} else {
		util.WriteJSONResponse(w, map[string]any{"timestamp": 0})
	}
}

// setPrepareShutdown toggles ingester lifecycler config to prepare for shutdown
func (i *Ingester) setPrepareShutdown() {
	i.lifecycler.SetUnregisterOnShutdown(true)
	i.lifecycler.SetFlushOnShutdown(true)
	i.partitionRingLifecycler.SetCreatePartitionOnStartup(false)
	i.partitionRingLifecycler.SetRemoveOwnerOnShutdown(true)
	i.metrics.shutdownMarker.Set(1)
}

func (i *Ingester) unsetPrepareShutdown() {
	i.lifecycler.SetUnregisterOnShutdown(i.cfg.LifecyclerConfig.UnregisterOnShutdown)
	i.lifecycler.SetFlushOnShutdown(true)
	i.partitionRingLifecycler.SetCreatePartitionOnStartup(true)
	i.partitionRingLifecycler.SetRemoveOwnerOnShutdown(false)
	i.metrics.shutdownMarker.Set(0)
}

// ReadinessHandler is used to indicate to k8s when the ingesters are ready for
// the addition removal of another ingester. Returns 204 when the ingester is
// ready, 500 otherwise.
func (i *Ingester) CheckReady(ctx context.Context) error {
	// todo.
	if s := i.State(); s != services.Running && s != services.Stopping {
		return fmt.Errorf("ingester not ready: %v", s)
	}
	return i.lifecycler.CheckReady(ctx)
}

// Flush implements ring.FlushTransferer
// Flush triggers a flush of all the chunks and closes the flush queues.
// Called from the Lifecycler as part of the ingester shutdown.
func (i *Ingester) Flush() {
}

func (i *Ingester) TransferOut(_ context.Context) error {
	return nil
}
