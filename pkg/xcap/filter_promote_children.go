package xcap

import "time"

// promoteChildrenStrategy implements DropStrategy by dropping regions below
// the duration threshold and re-parenting their children to the nearest kept
// ancestor. This preserves all regions that meet the duration threshold
// regardless of their position in the tree.
type promoteChildrenStrategy struct{}

func (s *promoteChildrenStrategy) Name() string {
	return string(DropStrategyPromoteChildren)
}

func (s *promoteChildrenStrategy) Filter(c *Capture, minDuration time.Duration) {
	if c == nil || minDuration <= 0 {
		return
	}

	ctx := newFilterContext(c, minDuration)

	// Mark regions to keep based on duration
	for _, r := range c.regions {
		ctx.toKeep[r.id] = ctx.duration(r) >= minDuration
	}

	// Process dropped regions: roll up observations to nearest kept ancestor
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

	// Re-parent children of dropped regions (modify in-place)
	for _, r := range c.regions {
		if !ctx.toKeep[r.id] {
			continue
		}
		if !r.parentID.IsZero() && !ctx.toKeep[r.parentID] {
			r.parentID = ctx.findNearestKeptAncestor(r.parentID)
		}
	}

	applyFilterResult(c, ctx)
}
