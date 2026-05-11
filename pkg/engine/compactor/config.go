// Package compactor contains the data-object compactor service skeleton.
//
// The compactor is opt-in: it runs only when started with
// -target=dataobj-compactor AND the dataobj.enabled and
// dataobj.compaction.enabled config flags are both true. The default
// configuration leaves both gates off; nothing in this package runs in
// the default Loki binary.
//
// This file is the scaffold for the dataobj compactor. The real coordinator
// polling loop, marker management, planner integration, and physical-plan
// node types are added in subsequent changes.
package compactor

import (
	"errors"
	"flag"
)

// Config is the top-level configuration for the dataobj compactor target.
// A follow-up commit wires it into the dataobj config tree at
// pkg/dataobj/config/config.go as the `compaction` field.
type Config struct {
	// Enabled toggles the compactor. When false, the dataobj-compactor
	// module is a no-op even if the target is selected.
	Enabled bool `yaml:"enabled"`

	// MaxRunningCompactionTasks caps how many CompactionMerge tasks a
	// single workflow may run concurrently within the engine scheduler's
	// taskTypeCompaction admission lane. Currently unused; reserved for
	// the engine scheduler's compaction admission lane added in a
	// follow-up change. The semantic of zero (unlimited vs. blocked) is
	// intentionally undefined at scaffold time; the follow-up change that
	// consumes this field will define and document it.
	MaxRunningCompactionTasks int `yaml:"max_running_compaction_tasks"`

	// Scheduler holds the scheduler-side knobs: advertise_addr and
	// endpoint for the embedded engine.Scheduler instance. See
	// pkg/engine/scheduler.go for the underlying SchedulerParams.
	Scheduler SchedulerConfig `yaml:"scheduler"`
}

// SchedulerConfig holds the scheduler-side parameters that get passed
// to engine.NewScheduler when the compactor target boots.
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
		"Experimental: Enable the dataobj compactor target.")
	f.IntVar(&cfg.MaxRunningCompactionTasks, prefix+"max-running-compaction-tasks",
		defaultMaxRunningCompactionTasks,
		"Experimental: Per-workflow cap on concurrent CompactionMerge tasks. Currently unused; reserved for the engine scheduler's compaction admission lane added in a follow-up change.")
	f.StringVar(&cfg.Scheduler.AdvertiseAddr, prefix+"scheduler.advertise-addr", "",
		"Experimental: host:port the embedded compaction scheduler advertises to compaction workers. Empty string keeps the scheduler in-process-only.")
	f.StringVar(&cfg.Scheduler.Endpoint, prefix+"scheduler.endpoint", defaultEndpoint,
		"Experimental: HTTP path the embedded compaction scheduler listens on for worker frame traffic.")
}

// Validate returns nil while the compactor is disabled. When enabled it
// performs basic shape checks; deeper validation lands alongside the real
// coordinator in a follow-up change.
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
