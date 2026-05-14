package compactor

import (
	"context"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"
)

// validWorkerCfg returns a minimal valid WorkerConfig for tests. The
// SchedulerLookupAddress is a syntactically valid host:port that no
// scheduler is listening on, so DNS resolution succeeds but no
// scheduler is discovered. AdvertiseAddr uses 127.0.0.1:0 so the
// listener picks a random free port.
func validWorkerCfg() WorkerConfig {
	return WorkerConfig{
		WorkerThreads:           1,
		SchedulerLookupAddress:  "127.0.0.1:65535",
		SchedulerLookupInterval: time.Second,
		AdvertiseAddr:           "127.0.0.1:0",
		Endpoint:                defaultEndpoint,
	}
}

// TestWorker_BootShutdown is the spec-required smoke test: worker boots,
// attempts DNS-SRV lookup, finds no scheduler, and shuts down cleanly.
func TestWorker_BootShutdown(t *testing.T) {
	w, err := NewWorker(WorkerParams{
		Config: validWorkerCfg(),
		Bucket: objstore.NewInMemBucket(),
		Logger: log.NewNopLogger(),
		// Metastore left nil: no tasks arrive (no scheduler), so the
		// metastore is never dereferenced.
		Registerer: prometheus.NewRegistry(),
	})
	require.NoError(t, err)
	require.NotNil(t, w)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	svc := w.Service()
	require.NoError(t, services.StartAndAwaitRunning(ctx, svc))
	require.Equal(t, services.Running, svc.State())

	// Sleep one lookup interval + a bit so the SRV poll loop runs at
	// least once with no scheduler in sight. The worker must stay
	// Running through that poll.
	time.Sleep(1500 * time.Millisecond)
	require.Equal(t, services.Running, svc.State(),
		"worker must stay running while no scheduler is discoverable")

	require.NoError(t, services.StopAndAwaitTerminated(ctx, svc))
	require.Equal(t, services.Terminated, svc.State())
}

// TestNewWorker_RequiresBucket pins the constructor invariant: the
// worker is useless without a bucket.
func TestNewWorker_RequiresBucket(t *testing.T) {
	_, err := NewWorker(WorkerParams{
		Config:     validWorkerCfg(),
		Bucket:     nil,
		Logger:     log.NewNopLogger(),
		Registerer: prometheus.NewRegistry(),
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "bucket")
}

// TestNewWorker_RequiresSchedulerLookupAddress pins the worker-side
// precondition that is intentionally NOT enforced in Config.Validate
// (so planner-only deployments stay valid).
func TestNewWorker_RequiresSchedulerLookupAddress(t *testing.T) {
	cfg := validWorkerCfg()
	cfg.SchedulerLookupAddress = ""
	_, err := NewWorker(WorkerParams{
		Config:     cfg,
		Bucket:     objstore.NewInMemBucket(),
		Logger:     log.NewNopLogger(),
		Registerer: prometheus.NewRegistry(),
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "scheduler_lookup_address",
		"error must name the missing field for operator clarity, got: %v", err)
}

// TestNewWorker_RequiresAdvertiseAddr pins the worker-side precondition
// that AdvertiseAddr must be non-empty. The compaction worker has no
// LocalScheduler, and engine.NewWorker rejects both-nil with a generic
// error; we want the operator-facing error to name the actual field.
func TestNewWorker_RequiresAdvertiseAddr(t *testing.T) {
	cfg := validWorkerCfg()
	cfg.AdvertiseAddr = ""
	_, err := NewWorker(WorkerParams{
		Config:     cfg,
		Bucket:     objstore.NewInMemBucket(),
		Logger:     log.NewNopLogger(),
		Registerer: prometheus.NewRegistry(),
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "advertise_addr",
		"error must name the missing field for operator clarity, got: %v", err)
}

// TestNewWorker_RequiresEndpoint pins the worker-side precondition that
// Endpoint must be non-empty. Empty Endpoint would otherwise silently
// fall through to engine.NewWorker's default ("/api/v2/frame") which
// collides with the query-engine worker's path.
func TestNewWorker_RequiresEndpoint(t *testing.T) {
	cfg := validWorkerCfg()
	cfg.Endpoint = ""
	_, err := NewWorker(WorkerParams{
		Config:     cfg,
		Bucket:     objstore.NewInMemBucket(),
		Logger:     log.NewNopLogger(),
		Registerer: prometheus.NewRegistry(),
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "endpoint",
		"error must name the missing field for operator clarity, got: %v", err)
}

// TestNewWorker_InvalidAdvertiseAddr exercises the constructor error
// path when the advertise address can't be parsed. Pins the error
// wrapping around the shared resolveAdvertiseAddr helper.
func TestNewWorker_InvalidAdvertiseAddr(t *testing.T) {
	cfg := validWorkerCfg()
	cfg.AdvertiseAddr = "not-a-valid-host:port:::"
	_, err := NewWorker(WorkerParams{
		Config:     cfg,
		Bucket:     objstore.NewInMemBucket(),
		Logger:     log.NewNopLogger(),
		Registerer: prometheus.NewRegistry(),
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "resolve worker advertise address",
		"error must mention the resolution step for operator clarity, got: %v", err)
}
