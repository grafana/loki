package redis

// flushCSCDifference clears each node cache in from that is absent from retained.
// Comparing node identities preserves caches across slot and role changes.
func flushCSCDifference(from, retained *clusterState) {
	if from == nil || retained == nil {
		return
	}
	routed := make(map[*clusterNode]struct{}, len(retained.Masters)+len(retained.Slaves))
	for _, slot := range retained.slots {
		for _, node := range slot.nodes {
			routed[node] = struct{}{}
		}
	}
	for _, slot := range from.slots {
		for _, node := range slot.nodes {
			if _, ok := routed[node]; ok {
				continue
			}
			routed[node] = struct{}{} // A node may appear in multiple slot ranges.
			if node.Client.csc != nil {
				node.Client.csc.Flush()
			}
		}
	}
}

// CSCStats sums statistics of node clients currently in the registry. Removed
// nodes are not retained for accounting; their counters disappear with them.
//
// Experimental: this API may change in a minor release.
func (c *ClusterClient) CSCStats() CSCStats {
	var total CSCStats
	nodes, _ := c.nodes.All()
	for _, node := range nodes {
		s := node.Client.CSCStats()
		total.Hits += s.Hits
		total.Misses += s.Misses
		total.Entries += s.Entries
		total.MemoryUsageBytes += s.MemoryUsageBytes
	}
	return total
}

// CSCRefreshStats sums refresh and invalidation counters of current node clients.
//
// Experimental: this API may change in a minor release.
func (c *ClusterClient) CSCRefreshStats() CSCRefreshStats {
	var total CSCRefreshStats
	nodes, _ := c.nodes.All()
	for _, node := range nodes {
		s := node.Client.CSCRefreshStats()
		total.Enqueued += s.Enqueued
		total.Dropped += s.Dropped
		total.Refreshed += s.Refreshed
		total.RefreshFailed += s.RefreshFailed
		total.DemandFlushes += s.DemandFlushes
		total.Invalidations += s.Invalidations
		total.Deletions += s.Deletions
		total.DeletionsNoop += s.DeletionsNoop
	}
	return total
}

func (c *ClusterClient) autopipelineCSCActive() bool {
	if c.opt.ClientSideCacheConfig != nil {
		return true
	}
	// A custom factory can enable node-local CSC without Cluster cache config.
	// Inspect existing clients only; never discover nodes or retain a second registry.
	if !c.opt.cscCustomFactory {
		return false
	}
	c.nodes.mu.RLock()
	defer c.nodes.mu.RUnlock()
	for _, node := range c.nodes.nodes {
		if node.Client.autopipelineCSCActive() {
			return true
		}
	}
	return false
}

// A solo command may use any node's cache, including one configured by a custom
// factory. Leave metadata eligibility to the selected node's Process path;
// admission must not discover nodes or assume they share one metadata view.
func (c *ClusterClient) autopipelineCSCEligible(cmd Cmder) bool {
	return !cmd.NoRetry() && commandArgsRepeatable(cmd) &&
		clusterCSCCommandPolicy(cmd.Name()) != clusterCSCReplyBypass
}
