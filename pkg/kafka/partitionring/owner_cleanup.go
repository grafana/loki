// This file implements a single-use maintenance task, not a part of Loki's
// steady-state behaviour. It exists because partition ring owners have no
// heartbeat: an instance registers itself as an owner on startup and is
// unregistered only if it shuts down after its prepare-downscale endpoint has
// been called. An instance that disappears any other way — a deleted or
// renamed StatefulSet, an evicted pod, a lost node — leaves its owner entry in
// the ring indefinitely, where it blocks deletion of INACTIVE partitions
// (which requires zero owners) and counts towards the minimum owner count that
// promotes a PENDING partition to ACTIVE. dskit's partition ring page can
// change partition states but cannot remove owners, so the entry is otherwise
// only removable by hand-editing the KV store.
package partitionring

import (
	"context"
	"flag"
	"fmt"
	"regexp"
	"slices"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/kv"
	"github.com/grafana/dskit/modules"
	"github.com/grafana/dskit/ring"
	"github.com/grafana/dskit/services"
)

// defaultOwnerIDPattern matches the instance IDs left behind by the
// dataobj-consumer StatefulSet after it was renamed to dataobj-builder. It is
// deliberately anchored so it cannot match a dataobj-builder-N owner.
const defaultOwnerIDPattern = `^dataobj-consumer-[0-9]+$`

type OwnerCleanupConfig struct {
	OwnerIDPattern   string        `yaml:"owner_id_pattern"`
	DryRun           bool          `yaml:"dry_run"`
	PropagationDelay time.Duration `yaml:"propagation_delay"`
	IgnoreLive       bool          `yaml:"ignore_live"`
	LiveThreshold    time.Duration `yaml:"live_threshold"`

	pattern *regexp.Regexp
}

func (cfg *OwnerCleanupConfig) RegisterFlags(f *flag.FlagSet) {
	const prefix = "partition-ring-owner-cleanup."

	f.StringVar(&cfg.OwnerIDPattern, prefix+"owner-id-pattern", defaultOwnerIDPattern,
		"Regular expression matching the partition ring owner IDs to remove. Anchor it: an unanchored pattern can match owners of instances that are still running.")
	f.BoolVar(&cfg.DryRun, prefix+"dry-run", true,
		"Report the owners that match without removing them. Removal is irreversible and the entry can only be recreated by restarting the instance it belongs to, so this defaults to true and must be disabled explicitly.")
	f.DurationVar(&cfg.PropagationDelay, prefix+"propagation-delay", time.Minute,
		"How long to keep the process alive after the removal so the change can be gossiped to the other members. Exiting too early can leave the removal known only to this instance, which then loses it. Ignored for a dry run.")
	f.BoolVar(&cfg.IgnoreLive, prefix+"ignore-live", false,
		"Remove a matching owner even when the instance it belongs to is still heartbeating in the instance ring. Only set this if the instance ring itself holds stale entries.")
	f.DurationVar(&cfg.LiveThreshold, prefix+"live-threshold", 5*time.Minute,
		"An instance whose instance-ring heartbeat is newer than this is treated as running, and its owner is kept unless -partition-ring-owner-cleanup.ignore-live is set.")
}

func (cfg *OwnerCleanupConfig) Validate() error {
	if cfg.OwnerIDPattern == "" {
		return fmt.Errorf("partition ring owner cleanup: owner_id_pattern is required")
	}
	pattern, err := regexp.Compile(cfg.OwnerIDPattern)
	if err != nil {
		return fmt.Errorf("partition ring owner cleanup: invalid owner_id_pattern: %w", err)
	}
	if cfg.LiveThreshold <= 0 {
		return fmt.Errorf("partition ring owner cleanup: live_threshold must be positive")
	}
	cfg.pattern = pattern
	return nil
}

func (cfg *OwnerCleanupConfig) compiled() (*regexp.Regexp, error) {
	if cfg.pattern != nil {
		return cfg.pattern, nil
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg.pattern, nil
}

// OwnerCleaner removes the partition ring owners matching a pattern and then
// asks the process to stop. It is meant to be run as a one-off pod against a
// ring whose owners have outlived their instances; see the package comment.
type OwnerCleaner struct {
	services.Service

	cfg      OwnerCleanupConfig
	pattern  *regexp.Regexp
	ringName string

	// partitionRingKey holds the owners to remove. instanceRingKey holds the
	// ordinary (heartbeating) registrations of the same instances and is only
	// read, to tell a departed instance from a running one.
	partitionRingKey string
	partitionStore   kv.Client
	instanceRingKey  string
	instanceStore    kv.Client

	logger log.Logger
}

func NewOwnerCleaner(
	cfg OwnerCleanupConfig,
	ringName, partitionRingKey string,
	partitionStore kv.Client,
	instanceRingKey string,
	instanceStore kv.Client,
	logger log.Logger,
) (*OwnerCleaner, error) {
	pattern, err := cfg.compiled()
	if err != nil {
		return nil, err
	}

	c := &OwnerCleaner{
		cfg:              cfg,
		pattern:          pattern,
		ringName:         ringName,
		partitionRingKey: partitionRingKey,
		partitionStore:   partitionStore,
		instanceRingKey:  instanceRingKey,
		instanceStore:    instanceStore,
		logger:           log.With(logger, "component", "partition-ring-owner-cleanup", "ring", ringName),
	}
	c.Service = services.NewBasicService(nil, c.running, nil)
	return c, nil
}

func (c *OwnerCleaner) running(ctx context.Context) error {
	if err := c.run(ctx); err != nil {
		return err
	}
	// Tell Loki to shut down: this is a task, not a server. Loki treats
	// ErrStopProcess as a clean exit rather than a failed module.
	return modules.ErrStopProcess
}

func (c *OwnerCleaner) run(ctx context.Context) error {
	level.Info(c.logger).Log(
		"msg", "starting partition ring owner cleanup",
		"pattern", c.cfg.OwnerIDPattern,
		"dry_run", c.cfg.DryRun,
	)

	candidates, err := c.candidates(ctx)
	if err != nil {
		return err
	}
	if len(candidates) == 0 {
		level.Info(c.logger).Log("msg", "no owners matched the pattern, nothing to do")
		return nil
	}

	remove := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		keep := candidate.live && !c.cfg.IgnoreLive
		level.Info(c.logger).Log(
			"msg", "matched partition ring owner",
			"owner", candidate.id,
			"partition", candidate.partition,
			"registered_at", candidate.registeredAt.Format(time.RFC3339),
			"instance", candidate.instanceState(),
			"action", actionFor(keep, c.cfg.DryRun),
		)
		if !keep {
			remove = append(remove, candidate.id)
		}
	}

	if len(remove) == 0 {
		level.Warn(c.logger).Log("msg", "every matching owner belongs to a live instance, nothing to remove")
		return nil
	}
	if c.cfg.DryRun {
		level.Info(c.logger).Log(
			"msg", "dry run complete, no changes written",
			"would_remove", len(remove),
			"hint", "re-run with -partition-ring-owner-cleanup.dry-run=false to apply",
		)
		return nil
	}

	removed, err := c.removeOwners(ctx, remove)
	if err != nil {
		return fmt.Errorf("removing partition ring owners: %w", err)
	}
	level.Info(c.logger).Log("msg", "removed partition ring owners", "count", len(removed))

	// The removal is a tombstone that has to reach the other members before
	// this process goes away, otherwise a member that still holds the owner
	// re-introduces it: on merge, an absent local entry compares as older than
	// any remote one.
	level.Info(c.logger).Log("msg", "waiting for the removal to propagate", "delay", c.cfg.PropagationDelay)
	select {
	case <-time.After(c.cfg.PropagationDelay):
	case <-ctx.Done():
		level.Warn(c.logger).Log("msg", "interrupted before the propagation delay elapsed; verify the ring and re-run if needed")
		return nil
	}

	c.verify(ctx, removed)
	return nil
}

// verify re-reads the ring so the run's log carries proof of the outcome
// rather than only the intent.
func (c *OwnerCleaner) verify(ctx context.Context, removed []string) {
	desc, err := c.partitionRing(ctx)
	if err != nil {
		level.Warn(c.logger).Log("msg", "failed to re-read the ring to verify the removal", "err", err)
		return
	}
	for _, id := range removed {
		// A reader never sees tombstones, so a hit here is a live re-registration.
		if desc.HasOwner(id) {
			level.Warn(c.logger).Log("msg", "owner is present again after removal", "owner", id)
			continue
		}
		level.Info(c.logger).Log("msg", "owner is gone", "owner", id)
	}
}

type candidate struct {
	id           string
	partition    int32
	registeredAt time.Time
	live         bool
	known        bool // registered in the instance ring at all
}

func (c candidate) instanceState() string {
	switch {
	case c.live:
		return "heartbeating"
	case c.known:
		return "registered-but-stale"
	default:
		return "not-registered"
	}
}

func actionFor(keep, dryRun bool) string {
	switch {
	case keep:
		return "keep"
	case dryRun:
		return "would-remove"
	default:
		return "remove"
	}
}

func (c *OwnerCleaner) candidates(ctx context.Context) ([]candidate, error) {
	desc, err := c.partitionRing(ctx)
	if err != nil {
		return nil, fmt.Errorf("reading partition ring: %w", err)
	}

	heartbeats, err := c.instanceHeartbeats(ctx)
	if err != nil {
		return nil, fmt.Errorf("reading instance ring: %w", err)
	}

	level.Info(c.logger).Log("msg", "read rings", "owners", len(desc.Owners), "instances", len(heartbeats))

	candidates := make([]candidate, 0)
	for id, owner := range desc.Owners {
		// Tombstones are how deletions travel through memberlist; they are not
		// owners any more.
		if owner.State == ring.OwnerDeleted || !c.pattern.MatchString(id) {
			continue
		}
		heartbeat, known := heartbeats[id]
		candidates = append(candidates, candidate{
			id:           id,
			partition:    owner.OwnedPartition,
			registeredAt: time.Unix(owner.GetUpdatedTimestamp(), 0).UTC(),
			known:        known,
			live:         known && time.Since(heartbeat) <= c.cfg.LiveThreshold,
		})
	}
	slices.SortFunc(candidates, func(a, b candidate) int {
		return int(a.partition) - int(b.partition)
	})
	return candidates, nil
}

func (c *OwnerCleaner) removeOwners(ctx context.Context, ids []string) ([]string, error) {
	var removed []string

	err := c.partitionStore.CAS(ctx, c.partitionRingKey, func(in any) (any, bool, error) {
		// CAS retries on a version mismatch, so discard what a previous attempt
		// decided.
		removed = nil

		desc := ring.GetOrCreatePartitionRingDesc(in)
		for _, id := range ids {
			if desc.RemoveOwner(id) {
				removed = append(removed, id)
			}
		}
		return desc, len(removed) > 0, nil
	})
	if err != nil {
		return nil, err
	}
	return removed, nil
}

func (c *OwnerCleaner) partitionRing(ctx context.Context) (*ring.PartitionRingDesc, error) {
	in, err := c.partitionStore.Get(ctx, c.partitionRingKey)
	if err != nil {
		return nil, err
	}
	return ring.GetOrCreatePartitionRingDesc(in), nil
}

// instanceHeartbeats returns the last heartbeat of every instance registered in
// the instance ring, keyed by instance ID.
func (c *OwnerCleaner) instanceHeartbeats(ctx context.Context) (map[string]time.Time, error) {
	in, err := c.instanceStore.Get(ctx, c.instanceRingKey)
	if err != nil {
		return nil, err
	}

	desc := ring.GetOrCreateRingDesc(in)
	heartbeats := make(map[string]time.Time, len(desc.Ingesters))
	for id, instance := range desc.Ingesters {
		heartbeats[id] = time.Unix(instance.Timestamp, 0)
	}
	return heartbeats, nil
}
