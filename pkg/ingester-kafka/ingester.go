package ingesterkafka

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/kv"
	"github.com/grafana/dskit/modules"
	"github.com/grafana/dskit/multierror"
	"github.com/grafana/dskit/ring"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc/health/grpc_health_v1"

	"github.com/grafana/loki/v3/pkg/ingester-kafka/kafka"
	"github.com/grafana/loki/v3/pkg/ingester-kafka/partitionring"
	util_log "github.com/grafana/loki/v3/pkg/util/log"

	"github.com/grafana/loki/v3/pkg/analytics"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/util"
)

const (
	RingName          = "kafka-ingester"
	PartitionRingName = "kafka-partition"

	shutdownMarkerFilename = "shutdown-requested.txt"
)

// ErrReadOnly is returned when the ingester is shutting down and a push was
// attempted.
var (
	ErrReadOnly = errors.New("Ingester is shutting down")

	activeTenantsStats = analytics.NewInt("ingester_active_tenants")
	ingesterIDRegexp   = regexp.MustCompile("ingester(-rf1)-([0-9]+)")
)

// Config for an ingester.
type Config struct {
	Enabled bool `yaml:"enabled" doc:"description=Whether the kafka ingester is enabled."`

	LifecyclerConfig   ring.LifecyclerConfig `yaml:"lifecycler,omitempty" doc:"description=Configures how the lifecycle of the ingester will operate and where it will register for discovery."`
	ShutdownMarkerPath string                `yaml:"shutdown_marker_path"`

	// Used for the kafka ingestion path
	PartitionRingConfig partitionring.Config `yaml:"partition_ring" category:"experimental"`
	KafkaConfig         kafka.Config
}

// RegisterFlags registers the flags.
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.LifecyclerConfig.RegisterFlagsWithPrefix("kafka-ingester", f, util_log.Logger)

	f.BoolVar(&cfg.Enabled, "kafka-ingester.enabled", false, "Whether the Kafka-based ingester path is enabled")
	f.StringVar(&cfg.ShutdownMarkerPath, "kafka-ingester.shutdown-marker-path", "", "Path where the shutdown marker file is stored. If not set and common.path_prefix is set then common.path_prefix will be used.")
}

type Wrapper interface {
	Wrap(wrapped Interface) Interface
}

// Storage is the store interface we need on the ingester.
type Storage interface {
	PutObject(ctx context.Context, objectKey string, object io.Reader) error
	Stop()
}

// Interface is an interface for the Ingester
type Interface interface {
	services.Service
	http.Handler

	logproto.PusherServer

	CheckReady(ctx context.Context) error
	FlushHandler(w http.ResponseWriter, _ *http.Request)
	ShutdownHandler(w http.ResponseWriter, r *http.Request)
	PrepareShutdown(w http.ResponseWriter, r *http.Request)
}

// Ingester builds chunks for incoming log streams.
type Ingester struct {
	services.Service

	cfg    Config
	logger log.Logger

	metrics *ingesterMetrics

	// Flag for whether stopping the ingester service should also terminate the
	// loki process.
	// This is set when calling the shutdown handler.
	terminateOnShutdown bool
	readonly            bool
	shutdownMtx         sync.Mutex // Allows processes to grab a lock and prevent a shutdown

	lifecycler              *ring.Lifecycler
	lifecyclerWatcher       *services.FailureWatcher
	ingesterPartitionID     int32
	partitionRingLifecycler *ring.PartitionInstanceLifecycler
}

// New makes a new Ingester.
func New(cfg Config,
	registerer prometheus.Registerer,
	metricsNamespace string,
	logger log.Logger,
) (*Ingester, error) {
	metrics := newIngesterMetrics(registerer)

	ingesterPartitionID, err := extractIngesterPartitionID(cfg.LifecyclerConfig.ID)
	if err != nil {
		return nil, fmt.Errorf("calculating ingester partition ID: %w", err)
	}

	partitionRingKV := cfg.LifecyclerConfig.RingConfig.KVStore.Mock
	if partitionRingKV == nil {
		partitionRingKV, err = kv.NewClient(cfg.LifecyclerConfig.RingConfig.KVStore, ring.GetPartitionRingCodec(), kv.RegistererWithKVName(registerer, PartitionRingName+"-lifecycler"), logger)
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

	i.lifecyclerWatcher = services.NewFailureWatcher()
	i.lifecyclerWatcher.WatchService(i.lifecycler)
	i.lifecyclerWatcher.WatchService(i.partitionRingLifecycler)

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

func (i *Ingester) starting(ctx context.Context) error {
	// pass new context to lifecycler, so that it doesn't stop automatically when Ingester's service context is done
	err := i.lifecycler.StartAsync(context.Background())
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

	shutdownMarkerPath := path.Join(i.cfg.ShutdownMarkerPath, shutdownMarkerFilename)
	shutdownMarker, err := shutdownMarkerExists(shutdownMarkerPath)
	if err != nil {
		return fmt.Errorf("failed to check ingester shutdown marker: %w", err)
	}

	if shutdownMarker {
		level.Info(i.logger).Log("msg", "detected existing shutdown marker, setting unregister and flush on shutdown", "path", shutdownMarkerPath)
		i.setPrepareShutdown()
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
	i.stopIncomingRequests()
	var errs util.MultiError

	//if i.flushOnShutdownSwitch.Get() {
	//	i.lifecycler.SetFlushOnShutdown(true)
	//}
	errs.Add(services.StopAndAwaitTerminated(context.Background(), i.lifecycler))

	// i.streamRateCalculator.Stop()

	// In case the flag to terminate on shutdown is set or this instance is marked to release its resources,
	// we need to mark the ingester service as "failed", so Loki will shut down entirely.
	// The module manager logs the failure `modules.ErrStopProcess` in a special way.
	if i.terminateOnShutdown && errs.Err() == nil {
		i.removeShutdownMarkerFile()
		return modules.ErrStopProcess
	}
	return errs.Err()
}

// stopIncomingRequests is called when ingester is stopping
func (i *Ingester) stopIncomingRequests() {
	i.shutdownMtx.Lock()
	defer i.shutdownMtx.Unlock()

	i.readonly = true
}

// removeShutdownMarkerFile removes the shutdown marker if it exists. Any errors are logged.
func (i *Ingester) removeShutdownMarkerFile() {
	shutdownMarkerPath := path.Join(i.cfg.ShutdownMarkerPath, shutdownMarkerFilename)
	exists, err := shutdownMarkerExists(shutdownMarkerPath)
	if err != nil {
		level.Error(i.logger).Log("msg", "error checking shutdown marker file exists", "err", err)
	}
	if exists {
		err = removeShutdownMarker(shutdownMarkerPath)
		if err != nil {
			level.Error(i.logger).Log("msg", "error removing shutdown marker file", "err", err)
		}
	}
}

// PrepareShutdown will handle the /ingester/prepare_shutdown endpoint.
//
// Internally, when triggered, this handler will configure the ingester service to release their resources whenever a SIGTERM is received.
// Releasing resources meaning flushing data, deleting tokens, and removing itself from the ring.
//
// It also creates a file on disk which is used to re-apply the configuration if the
// ingester crashes and restarts before being permanently shutdown.
//
// * `GET` shows the status of this configuration
// * `POST` enables this configuration
// * `DELETE` disables this configuration
func (i *Ingester) PrepareShutdown(w http.ResponseWriter, r *http.Request) {
	if i.cfg.ShutdownMarkerPath == "" {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	shutdownMarkerPath := path.Join(i.cfg.ShutdownMarkerPath, shutdownMarkerFilename)

	switch r.Method {
	case http.MethodGet:
		exists, err := shutdownMarkerExists(shutdownMarkerPath)
		if err != nil {
			level.Error(i.logger).Log("msg", "unable to check for prepare-shutdown marker file", "path", shutdownMarkerPath, "err", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		if exists {
			util.WriteTextResponse(w, "set")
		} else {
			util.WriteTextResponse(w, "unset")
		}
	case http.MethodPost:
		if err := createShutdownMarker(shutdownMarkerPath); err != nil {
			level.Error(i.logger).Log("msg", "unable to create prepare-shutdown marker file", "path", shutdownMarkerPath, "err", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		i.setPrepareShutdown()
		level.Info(i.logger).Log("msg", "created prepare-shutdown marker file", "path", shutdownMarkerPath)

		w.WriteHeader(http.StatusNoContent)
	case http.MethodDelete:
		if err := removeShutdownMarker(shutdownMarkerPath); err != nil {
			level.Error(i.logger).Log("msg", "unable to remove prepare-shutdown marker file", "path", shutdownMarkerPath, "err", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		i.unsetPrepareShutdown()
		level.Info(i.logger).Log("msg", "removed prepare-shutdown marker file", "path", shutdownMarkerPath)

		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

// setPrepareShutdown toggles ingester lifecycler config to prepare for shutdown
func (i *Ingester) setPrepareShutdown() {
	level.Info(i.logger).Log("msg", "preparing full ingester shutdown, resources will be released on SIGTERM")
	i.lifecycler.SetFlushOnShutdown(true)
	i.lifecycler.SetUnregisterOnShutdown(true)
	i.terminateOnShutdown = true
	i.metrics.shutdownMarker.Set(1)
}

func (i *Ingester) unsetPrepareShutdown() {
	level.Info(i.logger).Log("msg", "undoing preparation for full ingester shutdown")
	i.lifecycler.SetFlushOnShutdown(true)
	i.lifecycler.SetUnregisterOnShutdown(i.cfg.LifecyclerConfig.UnregisterOnShutdown)
	i.terminateOnShutdown = false
	i.metrics.shutdownMarker.Set(0)
}

// createShutdownMarker writes a marker file to disk to indicate that an ingester is
// going to be scaled down in the future. The presence of this file means that an ingester
// should flush and upload all data when stopping.
func createShutdownMarker(p string) error {
	// Write the file, fsync it, then fsync the containing directory in order to guarantee
	// it is persisted to disk. From https://man7.org/linux/man-pages/man2/fsync.2.html
	//
	// > Calling fsync() does not necessarily ensure that the entry in the
	// > directory containing the file has also reached disk.  For that an
	// > explicit fsync() on a file descriptor for the directory is also
	// > needed.
	file, err := os.Create(p)
	if err != nil {
		return err
	}

	merr := multierror.New()
	_, err = file.WriteString(time.Now().UTC().Format(time.RFC3339))
	merr.Add(err)
	merr.Add(file.Sync())
	merr.Add(file.Close())

	if err := merr.Err(); err != nil {
		return err
	}

	dir, err := os.OpenFile(path.Dir(p), os.O_RDONLY, 0o777)
	if err != nil {
		return err
	}

	merr.Add(dir.Sync())
	merr.Add(dir.Close())
	return merr.Err()
}

// removeShutdownMarker removes the shutdown marker file if it exists.
func removeShutdownMarker(p string) error {
	err := os.Remove(p)
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	dir, err := os.OpenFile(path.Dir(p), os.O_RDONLY, 0o777)
	if err != nil {
		return err
	}

	merr := multierror.New()
	merr.Add(dir.Sync())
	merr.Add(dir.Close())
	return merr.Err()
}

// shutdownMarkerExists returns true if the shutdown marker file exists, false otherwise
func shutdownMarkerExists(p string) (bool, error) {
	s, err := os.Stat(p)
	if err != nil && os.IsNotExist(err) {
		return false, nil
	}

	if err != nil {
		return false, err
	}

	return s.Mode().IsRegular(), nil
}

// ShutdownHandler handles a graceful shutdown of the ingester service and
// termination of the Loki process.
func (i *Ingester) ShutdownHandler(w http.ResponseWriter, r *http.Request) {
	// Don't allow calling the shutdown handler multiple times
	if i.State() != services.Running {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("Ingester is stopping or already stopped."))
		return
	}
	params := r.URL.Query()
	doFlush := util.FlagFromValues(params, "flush", true)
	doDeleteRingTokens := util.FlagFromValues(params, "delete_ring_tokens", false)
	doTerminate := util.FlagFromValues(params, "terminate", true)
	err := i.handleShutdown(doTerminate, doFlush, doDeleteRingTokens)

	// Stopping the module will return the modules.ErrStopProcess error. This is
	// needed so the Loki process is shut down completely.
	if err == nil || err == modules.ErrStopProcess {
		w.WriteHeader(http.StatusNoContent)
	} else {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(err.Error()))
	}
}

// handleShutdown triggers the following operations:
//   - Change the state of ring to stop accepting writes.
//   - optional: Flush all the chunks.
//   - optional: Delete ring tokens file
//   - Unregister from KV store
//   - optional: Terminate process (handled by service manager in loki.go)
func (i *Ingester) handleShutdown(terminate, flush, del bool) error {
	i.lifecycler.SetFlushOnShutdown(flush)
	i.lifecycler.SetClearTokensOnShutdown(del)
	i.lifecycler.SetUnregisterOnShutdown(true)
	i.terminateOnShutdown = terminate
	return services.StopAndAwaitTerminated(context.Background(), i)
}

// Push implements logproto.Pusher.
func (i *Ingester) Push(_ context.Context, _ *logproto.PushRequest) (*logproto.PushResponse, error) {
	return &logproto.PushResponse{}, nil
}

// Watch implements grpc_health_v1.HealthCheck.
func (*Ingester) Watch(*grpc_health_v1.HealthCheckRequest, grpc_health_v1.Health_WatchServer) error {
	return nil
}

// ReadinessHandler is used to indicate to k8s when the ingesters are ready for
// the addition removal of another ingester. Returns 204 when the ingester is
// ready, 500 otherwise.
func (i *Ingester) CheckReady(ctx context.Context) error {
	if s := i.State(); s != services.Running && s != services.Stopping {
		return fmt.Errorf("ingester not ready: %v", s)
	}
	return i.lifecycler.CheckReady(ctx)
}

func (i *Ingester) GetDetectedFields(_ context.Context, r *logproto.DetectedFieldsRequest) (*logproto.DetectedFieldsResponse, error) {
	return &logproto.DetectedFieldsResponse{
		Fields: []*logproto.DetectedField{
			{
				Label:       "foo",
				Type:        logproto.DetectedFieldString,
				Cardinality: 1,
			},
		},
		FieldLimit: r.GetFieldLimit(),
	}, nil
}

// Flush implements ring.FlushTransferer
// Flush triggers a flush of all the chunks and closes the flush queues.
// Called from the Lifecycler as part of the ingester shutdown.
func (i *Ingester) Flush() {
}

func (i *Ingester) TransferOut(_ context.Context) error {
	return nil
}
