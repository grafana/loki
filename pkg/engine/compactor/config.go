// Package compactor contains the dataobj compaction planner and worker service
// skeletons. The planner and worker are two roles in the broader dataobj
// compaction system.
//
// The compaction planner and compaction worker are both opt-in via Loki targets
// and share the same dataobj.compaction.enabled gate.
package compactor

import (
	"errors"
	"flag"
	"time"
)

// Config is the top-level configuration for dataobj compaction.
type Config struct {
	// Enabled gates both the dataobj-compaction-planner and the
	// dataobj-compaction-worker targets. When false, both modules are no-ops
	// even if their target is selected.
	Enabled bool `yaml:"enabled"`

	// MaxRunningCompactionTasks caps how many compaction tasks (IndexMerge
	// in v1.0; LogMerge in v2.0) a single workflow may run concurrently
	// within the engine scheduler's taskTypeCompaction admission lane.
	// Currently unused; reserved for the engine scheduler's compaction
	// admission lane added in a follow-up change. The semantic of zero
	// (unlimited vs. blocked) is intentionally undefined at scaffold
	// time; the follow-up change that consumes this field will define
	// and document it.
	MaxRunningCompactionTasks int `yaml:"max_running_compaction_tasks"`

	// Scheduler holds the scheduler-side knobs: advertise_addr and
	// endpoint for the embedded engine.Scheduler instance. See
	// pkg/engine/scheduler.go for the underlying SchedulerParams.
	Scheduler SchedulerConfig `yaml:"scheduler"`

	// Worker holds the worker-side knobs for the dataobj-compaction-worker
	// target. Independent of Scheduler: a process can be a planner-only
	// (scheduler+coordinator) or worker-only deployment, selected via
	// -target.
	Worker WorkerConfig `yaml:"worker"`
}

// SchedulerConfig holds the scheduler-side parameters that get passed
// to engine.NewScheduler when the compaction planner target boots.
type SchedulerConfig struct {
	// AdvertiseAddr is the host:port the embedded scheduler advertises to
	// remote workers. Empty string keeps the scheduler in-process-only
	// (no HTTP listener registered).
	AdvertiseAddr string `yaml:"advertise_addr"`

	// Endpoint is the absolute path on the Loki HTTP router where the
	// embedded scheduler listens for worker frame traffic. Defaults to
	// "/api/v2/compaction-frame" so it never collides with the
	// query-engine scheduler at "/api/v2/frame".
	Endpoint string `yaml:"endpoint"`
}

// WorkerConfig holds the worker-side parameters that get passed to
// engine.NewWorker when the dataobj-compaction-worker target boots.
// The compaction worker runs in remote-transport mode only; it discovers
// the compaction scheduler via DNS-SRV lookup.
//
// The worker module is gated by Loki target -target=dataobj-compaction-worker
// AND the same dataobj.compaction.enabled flag the planner uses. There is
// no separate worker enable flag — the role separation comes from the
// Loki target.
type WorkerConfig struct {
	// WorkerThreads is the per-pod task concurrency. Zero means use
	// GOMAXPROCS (matches the engine worker's default).
	WorkerThreads int `yaml:"worker_threads"`

	// SchedulerLookupAddress is the DNS-SRV address used to discover the
	// compaction scheduler(s). Required when the worker module actually
	// initializes (enforced in NewWorker, not in Config.Validate, so
	// planner-only deployments validate cleanly with the default empty
	// value).
	// Example: dnssrv+_compaction-frame._tcp.compactor-scheduler.svc.cluster.local
	SchedulerLookupAddress string `yaml:"scheduler_lookup_address"`

	// SchedulerLookupInterval is how often the worker re-runs the DNS-SRV
	// lookup to discover scheduler changes. Defaults to 10s.
	SchedulerLookupInterval time.Duration `yaml:"scheduler_lookup_interval"`

	// AdvertiseAddr is the host:port the embedded worker advertises to
	// schedulers and other workers. Required when the worker module
	// actually initializes (enforced in NewWorker): the compaction worker
	// uses no LocalScheduler, and engine.NewWorker requires one of
	// AdvertiseAddr or LocalScheduler to be set.
	AdvertiseAddr string `yaml:"advertise_addr"`

	// Endpoint is the absolute path on the worker's HTTP router where the
	// wire frame handler is registered. Defaults to
	// "/api/v2/compaction-frame" so it never collides with the
	// query-engine worker at "/api/v2/frame".
	Endpoint string `yaml:"endpoint"`
}

// Default values intentionally chosen conservative for the scaffold; the
// real values get tuned alongside the coordinator in a follow-up change.
const (
	defaultMaxRunningCompactionTasks = 16
	defaultEndpoint                  = "/api/v2/compaction-frame"
)

// RegisterFlags registers the compaction config flags under the given
// prefix.
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.RegisterFlagsWithPrefix("dataobj.compaction.", f)
}

// RegisterFlagsWithPrefix registers the compaction config flags using
// prefix as the flag-name prefix.
func (cfg *Config) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.BoolVar(&cfg.Enabled, prefix+"enabled", false,
		"Experimental: Enable dataobj compaction modules (planner and worker targets when selected via -target).")
	f.IntVar(&cfg.MaxRunningCompactionTasks, prefix+"max-running-compaction-tasks",
		defaultMaxRunningCompactionTasks,
		"Experimental: Per-workflow cap on concurrent compaction tasks (IndexMerge / LogMerge). Currently unused; reserved for the engine scheduler's compaction admission lane added in a follow-up change.")
	f.StringVar(&cfg.Scheduler.AdvertiseAddr, prefix+"scheduler.advertise-addr", "",
		"Experimental: host:port the embedded compaction scheduler advertises to compaction workers. Empty string keeps the scheduler in-process-only.")
	f.StringVar(&cfg.Scheduler.Endpoint, prefix+"scheduler.endpoint", defaultEndpoint,
		"Experimental: HTTP path the embedded compaction scheduler listens on for worker frame traffic.")
	cfg.Worker.RegisterFlagsWithPrefix(prefix+"worker.", f)
}

// RegisterFlagsWithPrefix registers the worker config flags using prefix
// as the flag-name prefix. Typically called via Config.RegisterFlagsWithPrefix
// with prefix = "<parent>worker.".
func (cfg *WorkerConfig) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.IntVar(&cfg.WorkerThreads, prefix+"worker-threads", 0,
		"Experimental: Number of task-execution threads. 0 uses GOMAXPROCS.")
	f.StringVar(&cfg.SchedulerLookupAddress, prefix+"scheduler-lookup-address", "",
		"Experimental: DNS-SRV address used to discover compaction schedulers. Required when -target=dataobj-compaction-worker. Example: dnssrv+_compaction-frame._tcp.compactor-scheduler.svc.cluster.local")
	f.DurationVar(&cfg.SchedulerLookupInterval, prefix+"scheduler-lookup-interval", 10*time.Second,
		"Experimental: Interval at which to re-run the DNS-SRV lookup.")
	f.StringVar(&cfg.AdvertiseAddr, prefix+"advertise-addr", "",
		"Experimental: host:port the embedded compaction worker advertises to schedulers. Required when -target=dataobj-compaction-worker.")
	f.StringVar(&cfg.Endpoint, prefix+"endpoint", defaultEndpoint,
		"Experimental: HTTP path the embedded compaction worker registers its frame handler on.")
}

// Validate returns nil while the compaction planner is disabled.
func (cfg *Config) Validate() error {
	if !cfg.Enabled {
		return nil
	}
	if cfg.MaxRunningCompactionTasks < 0 {
		return errInvalidMaxRunningCompactionTasks
	}
	if cfg.Scheduler.Endpoint == "" {
		return errEmptySchedulerEndpoint
	}
	return nil
}

// Sentinel validation errors. Kept at package scope so tests can match
// them with errors.Is.
var (
	errInvalidMaxRunningCompactionTasks = errors.New("dataobj.compaction.max_running_compaction_tasks must be >= 0")
	errEmptySchedulerEndpoint           = errors.New("dataobj.compaction.scheduler.endpoint must not be empty when compaction is enabled")
)
