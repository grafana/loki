package redis

import (
	"context"
	"strings"

	"github.com/redis/go-redis/v9/internal/hashtag"
)

// SCAN has request_policy:special and response_policy:special: a cursor belongs
// to the node that returned it, and the keyspace is split over the masters.
//
// ScanIterator re-sends the same *ScanCmd for every page, so the walk is kept on
// the command. The first page (cursor 0) takes a snapshot of the masters, or only
// the master of the one slot that a hash-tag MATCH can select
// (hashtag.PatternSlot). The walk starts on the node a SCAN without a walk goes
// to, so a loop that passes the cursor back by hand stays on one node. Pages
// go to one master until it returns cursor 0; the next page starts the next
// master from cursor 0. Val and Result report the server cursor; the iterator
// also asks the walk (ScanCmd.more) so it keeps fetching after a master ends.
// Each page is one ClusterClient.Process call, so process hooks run once per
// page.
//
// A SCAN that carries no walk goes to the routed node, or the node the
// ShardPicker selects: a raw command (Do) and a loop that passes a cursor back
// by hand, which cannot say which node the cursor belongs to. Keys added,
// deleted or moved during the walk can be returned twice or missed, as SCAN
// allows on one node.
type clusterScanState struct {
	masters []*clusterNode
	idx     int
	restart bool // the next page starts a new master: reset the cursor to 0
}

func (c *ClusterClient) executeScanCommand(
	ctx context.Context,
	cmd Cmder,
	node *clusterNode,
	decision clusterRoutingDecision,
) error {
	sc, ok := cmd.(*ScanCmd)
	if !ok || (sc.clusterScan == nil && !scanCursorIsZero(sc)) {
		return c.scanRoutedNode(ctx, cmd, node, &decision)
	}
	st := sc.clusterScan
	if st == nil {
		var err error
		if st, err = c.newClusterScan(ctx, sc, &decision); err != nil {
			return err
		}
		sc.clusterScan = st
	}
	if st.restart {
		sc.args[1] = uint64(0)
		st.restart = false
	}
	if err := processClusterScanOnNode(ctx, st.masters[st.idx], sc, false); err != nil {
		return err
	}
	if sc.cursor == 0 && st.idx+1 < len(st.masters) {
		st.idx++
		st.restart = true
	}
	return nil
}

// pending reports whether more masters are left to scan.
func (st *clusterScanState) pending() bool {
	return st != nil && st.restart
}

func (c *ClusterClient) newClusterScan(
	ctx context.Context,
	sc *ScanCmd,
	decision *clusterRoutingDecision,
) (*clusterScanState, error) {
	state, err := c.state.Get(ctx)
	if err != nil {
		return nil, err
	}
	// A MATCH that selects one slot scans only that slot's master. A SCAN without a
	// walk uses the same node (scanRoutedNode), so a hand-written cursor loop sends
	// every page there.
	if node, ok, err := c.scanTagMaster(state, sc); ok || err != nil {
		if err != nil {
			return nil, err
		}
		return &clusterScanState{masters: []*clusterNode{node}}, nil
	}
	// state.Masters holds only online masters: a walk over it would skip a shard.
	if err := state.requireOnline(state.declaredMasters()); err != nil {
		return nil, c.noteTopologySelectionError(err)
	}
	if len(state.Masters) == 0 {
		return nil, errClusterNoNodes
	}
	first, err := c.scanPageNode(ctx, decision)
	if err != nil {
		return nil, err
	}
	// Start on first. With ReadOnly it can be a replica: scan it in place of its
	// master, so later pages of a hand-written loop reach the node of the cursor.
	shard := state.shardMaster(first)
	start := 0
	for i, node := range state.Masters {
		if node == shard {
			start = i
			break
		}
	}
	masters := make([]*clusterNode, 0, len(state.Masters))
	masters = append(masters, state.Masters[start:]...)
	masters = append(masters, state.Masters[:start]...)
	if masters[0] == shard {
		masters[0] = first
	}
	return &clusterScanState{masters: masters}, nil
}

// shardMaster returns the master that node serves: node itself for a master,
// or the master of the slots a replica copies.
func (c *clusterState) shardMaster(node *clusterNode) *clusterNode {
	for _, slot := range c.slots {
		for _, n := range slot.nodes {
			if n == node {
				return slot.nodes[0]
			}
		}
	}
	return node
}

// scanRoutedNode runs one SCAN page on the redirected node, or the node that
// ordinary keyless routing selects.
func (c *ClusterClient) scanRoutedNode(
	ctx context.Context,
	cmd Cmder,
	node *clusterNode,
	decision *clusterRoutingDecision,
) error {
	if node == nil {
		// A MATCH that selects one slot: that slot's master, as for the walk, so every
		// page of a hand-written cursor loop reaches the node that issued the cursor.
		state, err := c.state.Get(ctx)
		if err != nil {
			return err
		}
		tagNode, ok, err := c.scanTagMaster(state, cmd)
		switch {
		case err != nil:
			return err
		case ok:
			node = tagNode
		default:
			if node, err = c.scanPageNode(ctx, decision); err != nil {
				return err
			}
		}
	}
	// A SCAN without a walk or a hash-tag-constrained MATCH is not pinned
	// to this node. If dialing fails, its retry can select another master.
	match, hasMatch := scanMatch(cmd)
	canReselect := !hasMatch || hashtag.PatternSlot(match) < 0
	return processClusterScanOnNode(ctx, node, cmd, canReselect)
}

// scanTagMaster returns the master of the one slot a SCAN's MATCH pattern can
// select (hashtag.PatternSlot), or ok=false when the pattern can match any slot.
func (c *ClusterClient) scanTagMaster(state *clusterState, cmd Cmder) (*clusterNode, bool, error) {
	match, ok := scanMatch(cmd)
	if !ok || !hashtag.Present(match) {
		return nil, false, nil
	}
	slot := hashtag.PatternSlot(match)
	if slot < 0 {
		return nil, false, nil
	}
	node, err := state.slotMasterNode(slot)
	if err != nil {
		return nil, false, c.noteTopologySelectionError(err)
	}
	return node, true, nil
}

// scanPageNode is the node that ordinary keyless routing selects for a page.
func (c *ClusterClient) scanPageNode(ctx context.Context, decision *clusterRoutingDecision) (*clusterNode, error) {
	if c.opt.ShardPicker != nil {
		return c.cmdNodeWithShardPickerAndDecision(ctx, decision.naturalSlot, c.opt.ShardPicker, decision)
	}
	slot := decision.naturalSlot
	if slot < 0 {
		slot = hashtag.RandomSlot()
	}
	return c.cmdNodeWithDecision(ctx, slot, decision)
}

// scanCursorIsZero and scanMatch read arguments as they are sent on the wire.
func scanCursorIsZero(sc *ScanCmd) bool {
	cursor, ok := routingArgText(sc, 1)
	return ok && cursor == "0"
}

func scanMatch(cmd Cmder) (string, bool) {
	for i := 2; i+1 < len(cmd.Args()); i++ {
		if name, ok := routingArgText(cmd, i); ok && strings.EqualFold(name, "match") {
			return routingArgText(cmd, i+1)
		}
	}
	return "", false
}

// clusterSpecialDialExhausted keeps the failed endpoint available to the
// cluster retry loop without altering the original dial error.
type clusterSpecialDialExhausted struct {
	err         error
	node        *clusterNode
	canReselect bool
}

func (e *clusterSpecialDialExhausted) Error() string { return e.err.Error() }
func (e *clusterSpecialDialExhausted) Unwrap() error { return e.err }

func processClusterScanOnNode(ctx context.Context, node *clusterNode, cmd Cmder, canReselect bool) error {
	ctx, exhausted := node.Client.captureFixedDialExhaustion(ctx)
	err := node.Client.Process(ctx, cmd)
	if err != nil && exhausted != nil && exhausted.Load() {
		return &clusterSpecialDialExhausted{err: err, node: node, canReselect: canReselect}
	}
	return err
}
