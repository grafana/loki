package xcap

import "time"

// dropSubtreeStrategy implements DropStrategy by dropping regions below the
// duration threshold along with their entire subtree of descendants, regardless
// of descendant durations. This is the most aggressive pruning strategy.
type dropSubtreeStrategy struct{}

func (s *dropSubtreeStrategy) Name() string {
	return string(DropStrategyDropSubtree)
}

func (s *dropSubtreeStrategy) Filter(c *Capture, minDuration time.Duration) {
	if c == nil || minDuration <= 0 {
		return
	}

	ctx := newFilterContext(c, minDuration)

	// Initially mark based on duration
	for _, r := range c.regions {
		ctx.toKeep[r.id] = ctx.duration(r) >= minDuration
	}

	// Mark descendants of dropped regions as dropped too.
	// We iterate until no changes to handle cascading (any ordering of regions).
	s.markSubtreesForDrop(c.regions, ctx)

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

// markSubtreesForDrop marks all descendants of dropped regions as dropped.
func (s *dropSubtreeStrategy) markSubtreesForDrop(regions []*Region, ctx *filterContext) {
	// Iterate until no changes (to handle any ordering of regions)
	changed := true
	for changed {
		changed = false
		for _, r := range regions {
			if !ctx.toKeep[r.id] {
				continue // already marked for drop
			}

			// If parent is dropped, this region should be dropped too
			if !r.parentID.IsZero() && !ctx.toKeep[r.parentID] {
				ctx.toKeep[r.id] = false
				changed = true
			}
		}
	}
}
