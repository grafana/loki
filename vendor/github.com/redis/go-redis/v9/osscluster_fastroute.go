package redis

import "sync/atomic"

// fastRouteEligible reports whether the shared templates of the metadata view can
// be used without resolving the decision: the default resolver, no fallback, and
// routing policies on.
func (c *ClusterClient) fastRouteEligible() bool {
	r := c.cmdInfoResolver
	return r != nil && r.legacyDefault && r.fallBackResolver == nil && !c.opt.DisableRoutingPolicies
}

// fastRouteLookup returns the view's shared template for cmd when the default
// resolver is known to produce exactly that template for the command.
func (c *ClusterClient) fastRouteLookup(cmd Cmder) *clusterRoutingDecision {
	if !c.fastRouteEligible() {
		return nil
	}
	args := cmd.Args()
	if len(args) == 0 || !routingCommandToken(args[0]) {
		return nil
	}
	meta := c.metadataView().routingTable[cmd.Name()]
	if meta == nil || atomic.LoadUint32(&meta.defaultTemplate) == 0 {
		return nil
	}
	t := meta.template
	if t.firstKey >= len(args) {
		return nil
	}
	slot := c.cmdSlotWithPos(cmd, t.firstKey, -1)
	if slot < 0 || slot >= 16384 {
		return nil
	}
	cmd.setCachedSlot(slot)
	return t
}

// fastRouteRemember marks cmd's template as the default resolver's decision once
// the full path produced it: one key at a fixed position, not a subcommand parent.
func (c *ClusterClient) fastRouteRemember(cmd Cmder, t *clusterRoutingDecision) {
	if !c.fastRouteEligible() || t.meta == nil || len(t.meta.keySpecs) != 1 || t.meta.keySpecs[0].lastKey != 0 {
		return
	}
	view := c.metadataView()
	if t.view != view {
		return
	}
	if _, parent := view.subcommandParents[cmd.Name()]; parent {
		return
	}
	atomic.StoreUint32(&t.meta.defaultTemplate, 1)
}
