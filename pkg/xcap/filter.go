package xcap

import (
	"time"

	"github.com/go-kit/log/level"

	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

// filterer holds the global filtering configuration and strategy.
type filterer struct {
	config   FilteringConfig
	strategy DropStrategy
}

func (f *filterer) applyFiltering(c *Capture) {
	if c == nil || f == nil || f.strategy == nil || f.config.MinRegionDuration <= 0 {
		return
	}
	f.strategy.Filter(c, f.config.MinRegionDuration)
}

// nopFilterer is a no-op filterer used as the default.
var nopFilterer = &filterer{}

var (
	globalFilterer = nopFilterer
)

// ConfigureGlobal configures the global xcap filtering settings.
// This should be called once at application startup.
// Returns an error if the configuration is invalid (e.g., unknown strategy name).
//
// Example:
//
//	err := xcap.ConfigureGlobal(xcap.Config{
//	    Filtering: xcap.FilteringConfig{
//	        MinRegionDuration: 1 * time.Millisecond,
//	        DropStrategyName:  xcap.DropStrategyPromoteChildren,
//	    },
//	})
//	if err != nil {
//	    log.Fatal(err)
//	}
func ConfigureGlobal(cfg Config) error {
	if err := cfg.Filtering.Validate(); err != nil {
		return err
	}

	strategy, err := cfg.Filtering.resolveStrategy()
	if err != nil {
		return err
	}

	globalFilterer = &filterer{
		config:   cfg.Filtering,
		strategy: strategy,
	}

	level.Info(util_log.Logger).Log(
		"msg", "xcap region filtering enabled",
		"threshold", cfg.Filtering.MinRegionDuration,
		"strategy", strategy.Name(),
	)

	return nil
}

// DropStrategy defines the interface for region filtering strategies.
// Implementations determine which regions to keep and how to handle
// the tree structure when regions are dropped.
type DropStrategy interface {
	// Name returns the strategy identifier for logging/debugging.
	Name() string

	// Filter applies the strategy to the capture in-place, modifying
	// its regions according to the strategy rules. The minDuration
	// parameter specifies the threshold below which regions may be dropped.
	//
	// Implementations must:
	// - Roll up observations from dropped regions to kept ancestors
	// - Update droppedRegions counts on kept regions
	// - Maintain valid parent-child relationships
	// - Modify the capture's regions slice in-place
	Filter(capture *Capture, minDuration time.Duration)
}

// filterContext holds shared state during filtering operations.
type filterContext struct {
	regionByID       map[identifier]*Region
	childrenByParent map[identifier][]*Region
	toKeep           map[identifier]bool
	minDuration      time.Duration
}

// newFilterContext creates a new filter context from a capture.
// The capture's mutex must be held by the caller.
func newFilterContext(c *Capture, minDuration time.Duration) *filterContext {
	ctx := &filterContext{
		regionByID:       make(map[identifier]*Region, len(c.regions)),
		childrenByParent: make(map[identifier][]*Region),
		toKeep:           make(map[identifier]bool, len(c.regions)),
		minDuration:      minDuration,
	}

	for _, r := range c.regions {
		ctx.regionByID[r.id] = r
		ctx.childrenByParent[r.parentID] = append(ctx.childrenByParent[r.parentID], r)
	}

	return ctx
}

// duration returns the duration of a region.
func (ctx *filterContext) duration(r *Region) time.Duration {
	return r.endTime.Sub(r.startTime)
}

// children returns the children of a region.
func (ctx *filterContext) children(r *Region) []*Region {
	return ctx.childrenByParent[r.id]
}

// hasKeptChildren returns true if any child of the region is marked to keep.
func (ctx *filterContext) hasKeptChildren(r *Region) bool {
	for _, child := range ctx.children(r) {
		if ctx.toKeep[child.id] {
			return true
		}
	}
	return false
}

// findNearestKeptAncestor walks up the tree to find the closest kept region.
// Returns zeroID if no kept ancestor exists.
func (ctx *filterContext) findNearestKeptAncestor(startID identifier) identifier {
	current := startID
	for !current.IsZero() {
		if ctx.toKeep[current] {
			return current
		}
		if parent, ok := ctx.regionByID[current]; ok {
			current = parent.parentID
		} else {
			break
		}
	}
	return zeroID
}

// rollUpObservations merges observations from source into target.
func rollUpObservations(target, source *Region) {
	if target == nil || source == nil {
		return
	}

	for key, obs := range source.observations {
		if existing, ok := target.observations[key]; ok {
			existing.Merge(obs)
		} else {
			if target.observations == nil {
				target.observations = make(map[StatisticKey]*AggregatedObservation)
			}
			target.observations[key] = &AggregatedObservation{
				Statistic: obs.Statistic,
				Value:     obs.Value,
				Count:     obs.Count,
			}
		}
	}
}

// applyFilterResult modifies the capture in-place, keeping only the regions marked to keep.
func applyFilterResult(c *Capture, ctx *filterContext) {
	n := 0
	for _, r := range c.regions {
		if ctx.toKeep[r.id] {
			c.regions[n] = r
			n++
		}
	}
	c.regions = c.regions[:n]
}
