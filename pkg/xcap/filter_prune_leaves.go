package xcap

import "time"

// pruneLeavesStrategy implements DropStrategy by only dropping leaf regions
// that are below the duration threshold. After dropping a leaf, if its parent
// becomes a leaf and is also below threshold, it will be dropped too (cascading upward).
type pruneLeavesStrategy struct{}

func (s *pruneLeavesStrategy) Name() string {
	return string(DropStrategyPruneLeaves)
}

func (s *pruneLeavesStrategy) Filter(c *Capture, minDuration time.Duration) {
	if c == nil || minDuration <= 0 {
		return
	}

	ctx := newFilterContext(c, minDuration)

	// Initially mark all regions as kept
	for _, r := range c.regions {
		ctx.toKeep[r.id] = true
	}

	// Iteratively prune leaves until no more can be pruned.
	// A region can be pruned if:
	// 1. It is a leaf (no kept children)
	// 2. Its duration is below the threshold
	changed := true
	for changed {
		changed = false
		for _, r := range c.regions {
			if !ctx.toKeep[r.id] {
				continue
			}

			// Check if this is now a leaf (all children dropped)
			if ctx.hasKeptChildren(r) {
				continue
			}

			// It's a leaf - check if it should be dropped
			if ctx.duration(r) < minDuration {
				ctx.toKeep[r.id] = false
				changed = true
			}
		}
	}

	// Roll up observations from dropped regions to their nearest kept ancestor
	for _, r := range c.regions {
		if ctx.toKeep[r.id] {
			continue
		}

		ancestorID := ctx.findNearestKeptAncestor(r.parentID)
		if ancestor := ctx.regionByID[ancestorID]; ancestor != nil {
			rollUpObservations(ancestor, r)
			ancestor.addDroppedRegions(1)
		}
	}

	applyFilterResult(c, ctx)
}
