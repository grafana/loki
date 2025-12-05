// SPDX-License-Identifier: MPL-2.0

package memberlist

// NodeSelectionDelegate is an optional delegate that can be used to filter and prioritize
// nodes for gossip, and push/pull operations. This allows implementing custom routing logic,
// such as zone-aware or rack-aware gossiping.
//
// This delegate is not used for probes (health checks). When implementing zone-aware gossiping,
// probes can bypass this delegate.
type NodeSelectionDelegate interface {
	// SelectNode is called for each node to determine:
	// - selected: whether the node should be included in the selection pool
	// - preferred: indicates whether the node should be prioritized. During a gossip cycle,
	//              at least one preferred node is always selected. If no preferred nodes exist,
	//              all gossip targets are chosen randomly from the nodes that have been marked
	//              as "selected".
	SelectNode(Node) (selected, preferred bool)
}
