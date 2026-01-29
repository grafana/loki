package xcap

import (
	"fmt"
	"time"
)

// DropStrategyName identifies a built-in drop strategy by name.
type DropStrategyName string

const (
	// DropStrategyPruneLeaves only drops leaf regions that are below the
	// duration threshold. After dropping a leaf, if its parent becomes a
	// leaf and is also below threshold, it will be dropped too (cascading upward).
	DropStrategyPruneLeaves DropStrategyName = "prune_leaves"

	// DropStrategyPromoteChildren drops regions below the duration threshold
	// and re-parents their children to the nearest kept ancestor. This preserves
	// all regions that meet the duration threshold regardless of tree position.
	DropStrategyPromoteChildren DropStrategyName = "promote_children"

	// DropStrategyDropSubtree drops regions below the duration threshold along
	// with their entire subtree of descendants, regardless of descendant durations.
	DropStrategyDropSubtree DropStrategyName = "drop_subtree"
)

// FilteringConfig holds configuration for region filtering.
// Filtering is DISABLED when this struct is zero-valued.
// To enable filtering, set MinRegionDuration > 0.
type FilteringConfig struct {
	// MinRegionDuration is the minimum duration a region must have to be
	// retained. Regions shorter than this threshold are dropped according
	// to the configured strategy.
	//
	// A value of 0 or negative disables filtering entirely.
	MinRegionDuration time.Duration `yaml:"min_region_duration"`

	// DropStrategyName selects a built-in drop strategy by name.
	// Valid values: "prune_leaves", "promote_children", "drop_subtree".
	//
	// If an unknown name is provided, NewProvider will return an error.
	DropStrategyName DropStrategyName `yaml:"drop_strategy"`
}

// Config holds configuration for the xcap Provider.
type Config struct {
	// Filtering configures region filtering behavior.
	// When zero-valued, filtering is disabled.
	Filtering FilteringConfig `yaml:"filtering"`
}

// Validate checks the configuration for errors.
// Returns an error if DropStrategyName is set to an unknown value.
// When no strategy is specified, prune_leaves is used as default.
func (c *FilteringConfig) Validate() error {
	// Validate strategy name (empty string defaults to prune_leaves)
	switch c.DropStrategyName {
	case DropStrategyPruneLeaves, DropStrategyPromoteChildren, DropStrategyDropSubtree, "":
		return nil
	default:
		return fmt.Errorf("xcap: unknown drop_strategy %q; valid options are: prune_leaves, promote_children, drop_subtree", c.DropStrategyName)
	}
}

// noopStrategy is a no-op drop strategy that keeps all regions unchanged.
type noopStrategy struct{}

func (s *noopStrategy) Name() string { return "noop" }

func (s *noopStrategy) Filter(_ *Capture, _ time.Duration) {}

// resolveStrategy returns the DropStrategy to use based on configuration.
// Returns noopStrategy if filtering is disabled.
// Defaults to prune_leaves if no strategy is specified.
// Returns an error if validation would fail.
func (c *FilteringConfig) resolveStrategy() (DropStrategy, error) {
	if c.MinRegionDuration <= 0 {
		return &noopStrategy{}, nil
	}

	switch c.DropStrategyName {
	case DropStrategyPruneLeaves, "": // Default to prune_leaves
		return &pruneLeavesStrategy{}, nil
	case DropStrategyPromoteChildren:
		return &promoteChildrenStrategy{}, nil
	case DropStrategyDropSubtree:
		return &dropSubtreeStrategy{}, nil
	default:
		// Should not happen if Validate() was called
		return nil, fmt.Errorf("xcap: unvalidated strategy name %q", c.DropStrategyName)
	}
}
