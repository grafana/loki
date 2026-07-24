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
	"fmt"
	"time"

	"github.com/grafana/dskit/flagext"

	"github.com/grafana/loki/v3/pkg/dataobj/consumer/logsobj"
)

// Config is the top-level configuration for dataobj compaction.
type Config struct {
	// Enabled gates both the dataobj-compaction-planner and the
	// dataobj-compaction-worker targets. When false, both modules are no-ops
	// even if their target is selected.
	Enabled bool `yaml:"enabled"`

	// MaxRunningCompactionTasks caps how many IndexMerge tasks the coordinator
	// runs concurrently per tenant. Zero means unlimited (one goroutine per task
	// with no admission throttle). Negative values are rejected at config
	// validation.
	MaxRunningCompactionTasks int `yaml:"max_running_compaction_tasks"`

	// LogMaxRunningCompactionTasks caps how many LogMerge tasks the coordinator
	// runs concurrently per tenant. Zero means unlimited. Negative values are
	// rejected at config validation.
	LogMaxRunningCompactionTasks int `yaml:"logs_max_running_compaction_tasks"`

	// PollingInterval is the cadence of the coordinator's main loop. Each
	// tick reads the most-recent ToC and plans compaction per tenant.
	PollingInterval time.Duration `yaml:"polling_interval"`

	// MaxRunsPerTask (K in the K-way merge) is the maximum number of runs a
	// single IndexMerge task may consume. Memory grows linearly with K.
	MaxRunsPerTask int `yaml:"max_runs_per_task"`

	// LogMaxRunsPerTask (K for log compaction) is the maximum number of runs a
	// single LogMerge task may consume. Kept separate from index max runs
	LogMaxRunsPerTask int `yaml:"logs_max_runs_per_task"`

	// LogMinCompactionSize is the minimum total compactable data (sum of all
	// runs' uncompressed size) that justifies log compaction. Converged
	// windows below this floor are skipped.
	LogMinCompactionSize flagext.Bytes `yaml:"logs_min_compaction_size"`

	// ToCConsolidateTimeout bounds the coordinator's inline ReplaceIndexPointers
	// call. NOT a task TTL — applied as context.WithTimeout around the metastore
	// RPC; on expiry the cycle aborts inline and the next polling tick re-plans.
	ToCConsolidateTimeout time.Duration `yaml:"toc_consolidate_timeout"`

	// DryRun, when true, skips the post-compaction ReplaceIndexPointers ToC
	// swap. Planning and IndexMerge task execution still run and the
	// per-output audit log is still emitted, but the ToC is never mutated.
	// Intended for testing index compaction.
	DryRun bool `yaml:"dry_run"`

	// PlanVersion is hashed into IndexMerge output paths so a planner
	// change invalidates previously-written outputs. Bump on any
	// breaking change to the planner or merge semantics. Stored as uint
	// because the standard library's flag package does not provide
	// Uint32Var.
	PlanVersion uint `yaml:"plan_version"`

	// Scheduler holds the scheduler-side knobs: advertise_addr and
	// endpoint for the embedded engine.Scheduler instance. See
	// pkg/engine/scheduler.go for the underlying SchedulerParams.
	Scheduler SchedulerConfig `yaml:"scheduler"`

	// Worker holds the worker-side knobs for the dataobj-compaction-worker
	// target. Independent of Scheduler: a process can be a planner-only
	// (scheduler+coordinator) or worker-only deployment, selected via
	// -target.
	Worker WorkerConfig `yaml:"worker"`

	// IndexobjBuilder controls index object construction parameters (page sizes,
	// target object/section sizes, etc.) used by the compactor worker when
	// merging postings + stats sections into a new index object.
	IndexobjBuilder logsobj.BuilderBaseConfig `yaml:"indexobj_builder" category:"experimental"`

	// LogsobjBuilder controls the construction of the compacted *data* (log)
	// objects that LogMerge writes. It is deliberately separate from
	// IndexobjBuilder: index objects are small and finely sectioned, while the
	// merged log objects must use data-object-scale sections (like the ingester's
	// ~128MB) — reusing the small index-object section size over-sections the
	// merged objects and balloons the index rebuilt over them.
	LogsobjBuilder logsobj.BuilderBaseConfig `yaml:"logsobj_builder" category:"experimental"`
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
	defaultMaxRunningCompactionTasks    = 16
	defaultLogMaxRunningCompactionTasks = 16
	defaultEndpoint                     = "/api/v2/compaction-frame"

	defaultPollingInterval       = 5 * time.Minute
	defaultMaxRunsPerTask        = 8
	defaultLogMaxRunsPerTask     = 3
	defaultToCConsolidateTimeout = 30 * time.Second
	defaultPlanVersion           = uint(1)
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
		"Experimental: Per-tenant-cycle cap on concurrent IndexMerge tasks dispatched by the coordinator. 0 means unlimited (one goroutine per task with no admission throttle).")
	f.IntVar(&cfg.LogMaxRunningCompactionTasks, prefix+"logs.max-running-compaction-tasks",
		defaultLogMaxRunningCompactionTasks,
		"Experimental: Per-tenant-cycle cap on concurrent LogMerge tasks dispatched by the coordinator. 0 means unlimited.")
	f.DurationVar(&cfg.PollingInterval, prefix+"polling-interval", defaultPollingInterval,
		"Experimental: Coordinator main-loop cadence.")
	f.IntVar(&cfg.MaxRunsPerTask, prefix+"max-runs-per-task", defaultMaxRunsPerTask,
		"Experimental: Maximum runs per IndexMerge task (K). Memory grows linearly with K.")
	f.IntVar(&cfg.LogMaxRunsPerTask, prefix+"logs.max-runs-per-task", defaultLogMaxRunsPerTask,
		"Experimental: Maximum runs per LogMerge task (K for log compaction). Separate from max-runs-per-task to scale independently")
	_ = cfg.LogMinCompactionSize.Set("4MB")
	f.Var(&cfg.LogMinCompactionSize, prefix+"logs.min-compaction-size",
		"Experimental: Minimum total compactable data (sum of all runs' uncompressed size) that justifies log compaction. Converged windows below this floor are skipped.")
	f.DurationVar(&cfg.ToCConsolidateTimeout, prefix+"toc-consolidate-timeout", defaultToCConsolidateTimeout,
		"Experimental: Coordinator-side timeout around the inline ToC ReplaceIndexPointers call. Not a task TTL.")
	f.BoolVar(&cfg.DryRun, prefix+"dry-run", false,
		"Experimental: Skip the post-compaction ToC ReplaceIndexPointers swap. Planning, IndexMerge task execution, and per-output audit logging still run, but the ToC is never mutated.")
	f.UintVar(&cfg.PlanVersion, prefix+"plan-version", defaultPlanVersion,
		"Experimental: Plan version hashed into IndexMerge output paths. Bump to invalidate previously-written outputs after a planner-algorithm change.")
	f.StringVar(&cfg.Scheduler.AdvertiseAddr, prefix+"scheduler.advertise-addr", "",
		"Experimental: host:port the embedded compaction scheduler advertises to compaction workers. Empty string keeps the scheduler in-process-only.")
	f.StringVar(&cfg.Scheduler.Endpoint, prefix+"scheduler.endpoint", defaultEndpoint,
		"Experimental: HTTP path the embedded compaction scheduler listens on for worker frame traffic.")
	cfg.Worker.RegisterFlagsWithPrefix(prefix+"worker.", f)

	_ = cfg.IndexobjBuilder.TargetPageSize.Set("2KB")
	_ = cfg.IndexobjBuilder.TargetObjectSize.Set("4MB")
	_ = cfg.IndexobjBuilder.TargetSectionSize.Set("2MB")
	_ = cfg.IndexobjBuilder.BufferSize.Set("16KB")
	cfg.IndexobjBuilder.RegisterFlagsWithPrefix(prefix+"indexobj-builder.", f)

	// The compacted log objects are data objects; default them to the ingester's
	// data-object sizes (not the small index-object sizes above) so LogMerge does
	// not over-section them and inflate the index built over them.
	_ = cfg.LogsobjBuilder.TargetPageSize.Set("2MB")
	_ = cfg.LogsobjBuilder.TargetObjectSize.Set("1GB")
	_ = cfg.LogsobjBuilder.TargetSectionSize.Set("128MB")
	_ = cfg.LogsobjBuilder.BufferSize.Set("16MB")
	cfg.LogsobjBuilder.RegisterFlagsWithPrefix(prefix+"logsobj-builder.", f)
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
	if cfg.LogMaxRunningCompactionTasks < 0 {
		return errInvalidLogMaxRunningCompactionTasks
	}
	if cfg.Scheduler.Endpoint == "" {
		return errEmptySchedulerEndpoint
	}
	if cfg.PollingInterval <= 0 {
		return errInvalidPollingInterval
	}
	if cfg.ToCConsolidateTimeout <= 0 {
		return errInvalidToCConsolidateTimeout
	}
	if cfg.MaxRunsPerTask <= 0 {
		return errInvalidMaxRunsPerTask
	}
	if cfg.LogMaxRunsPerTask <= 0 {
		return errInvalidLogMaxRunsPerTask
	}
	if cfg.LogMinCompactionSize == 0 {
		return errInvalidLogMinCompactionSize
	}

	if err := cfg.IndexobjBuilder.Validate(); err != nil {
		return fmt.Errorf("invalid indexobj builder config: %w", err)
	}
	if err := cfg.LogsobjBuilder.Validate(); err != nil {
		return fmt.Errorf("invalid logsobj builder config: %w", err)
	}
	return nil
}

// Sentinel validation errors. Kept at package scope so tests can match
// them with errors.Is.
var (
	errInvalidMaxRunningCompactionTasks    = errors.New("dataobj.compaction.max_running_compaction_tasks must be >= 0")
	errInvalidLogMaxRunningCompactionTasks = errors.New("dataobj.compaction.logs.max_running_compaction_tasks must be >= 0")
	errEmptySchedulerEndpoint              = errors.New("dataobj.compaction.scheduler.endpoint must not be empty when compaction is enabled")
	errInvalidPollingInterval              = errors.New("dataobj.compaction.polling_interval must be > 0 when compaction is enabled")
	errInvalidToCConsolidateTimeout        = errors.New("dataobj.compaction.toc_consolidate_timeout must be > 0 when compaction is enabled")
	errInvalidMaxRunsPerTask               = errors.New("dataobj.compaction.max_runs_per_task must be > 0 when compaction is enabled")
	errInvalidLogMaxRunsPerTask            = errors.New("dataobj.compaction.logs.max_runs_per_task must be > 0 when compaction is enabled")
	errInvalidLogMinCompactionSize         = errors.New("dataobj.compaction.logs.min_compaction_size must be > 0 when compaction is enabled")
)
