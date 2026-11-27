package redis

import (
	"context"
	"sync"
	"sync/atomic"
)

// Preserve the existing cluster-wide helpers unless a resolver selects a policy
// for this invocation. A resolver for an unrelated command must not change them.
func (c *ClusterClient) usesLegacyClusterHelpers(ctx context.Context, cmd Cmder) bool {
	if c.opt.DisableRoutingPolicies || c.cmdInfoResolver == nil ||
		(c.cmdInfoResolver.legacyDefault && c.cmdInfoResolver.fallBackResolver == nil) {
		return true
	}
	decision := c.resolveCommandRoutingDecision(ctx, cmd)
	if decision.policy == nil && !decision.policyFromMetadata {
		return true
	}
	// Process consumes this decision without invoking a custom resolver twice.
	cmd.setClusterRoutingDecision(&decision)
	return false
}

func (c *ClusterClient) DBSize(ctx context.Context) *IntCmd {
	cmd := NewIntCmd(ctx, "dbsize")
	c.processClusterHelper(ctx, cmd, func(ctx context.Context, _ Cmder) error {
		var size atomic.Int64
		err := c.ForEachMaster(ctx, func(ctx context.Context, master *Client) error {
			n, err := master.DBSize(ctx).Result()
			if err != nil {
				return err
			}
			size.Add(n)
			return nil
		})
		if err == nil {
			cmd.val = size.Load()
		}
		return err
	})
	return cmd
}

func (c *ClusterClient) ScriptLoad(ctx context.Context, script string) *StringCmd {
	cmd := NewStringCmd(ctx, "script", "load", script)
	c.processClusterHelper(ctx, cmd, func(ctx context.Context, _ Cmder) error {
		var mu sync.Mutex
		err := c.ForEachShard(ctx, func(ctx context.Context, shard *Client) error {
			val, err := shard.ScriptLoad(ctx, script).Result()
			if err != nil {
				return err
			}

			mu.Lock()
			if cmd.Val() == "" {
				cmd.val = val
			}
			mu.Unlock()

			return nil
		})
		return err
	})
	return cmd
}

func (c *ClusterClient) ScriptFlush(ctx context.Context) *StatusCmd {
	cmd := NewStatusCmd(ctx, "script", "flush")
	c.processClusterHelper(ctx, cmd, func(ctx context.Context, _ Cmder) error {
		var mu sync.Mutex
		var status string
		err := c.ForEachShard(ctx, func(ctx context.Context, shard *Client) error {
			val, err := shard.ScriptFlush(ctx).Result()
			if err != nil {
				return err
			}

			mu.Lock()
			status = val
			mu.Unlock()

			return nil
		})
		if err == nil {
			cmd.val = status
		}
		return err
	})
	return cmd
}

func (c *ClusterClient) ScriptExists(ctx context.Context, hashes ...string) *BoolSliceCmd {
	args := make([]interface{}, 2+len(hashes))
	args[0] = "script"
	args[1] = "exists"
	for i, hash := range hashes {
		args[2+i] = hash
	}
	cmd := NewBoolSliceCmd(ctx, args...)
	c.processClusterHelper(ctx, cmd, func(ctx context.Context, _ Cmder) error {
		result := make([]bool, len(hashes))
		for i := range result {
			result[i] = true
		}
		var mu sync.Mutex
		err := c.ForEachShard(ctx, func(ctx context.Context, shard *Client) error {
			val, err := shard.ScriptExists(ctx, hashes...).Result()
			if err != nil {
				return err
			}

			mu.Lock()
			for i, v := range val {
				result[i] = result[i] && v
			}
			mu.Unlock()

			return nil
		})
		if err == nil {
			cmd.val = result
		}
		return err
	})
	return cmd
}

func (c *ClusterClient) ClusterSlotStats(ctx context.Context, startSlot, endSlot int) *ClusterSlotStatsCmd {
	cmd := NewClusterSlotStatsCmd(ctx, "cluster", "slot-stats", "slotsrange", startSlot, endSlot)
	c.clusterSlotStats(ctx, cmd)
	return cmd
}

func (c *ClusterClient) ClusterSlotStatsOrderBy(ctx context.Context, a *ClusterSlotStatsOrderByArgs) *ClusterSlotStatsCmd {
	cmd := NewClusterSlotStatsCmd(ctx, clusterSlotStatsOrderByArgs(a)...)
	c.clusterSlotStats(ctx, cmd)
	return cmd
}

// clusterSlotStats asks every master, since a node reports only its own slots.
func (c *ClusterClient) clusterSlotStats(ctx context.Context, cmd *ClusterSlotStatsCmd) {
	c.processClusterHelper(ctx, cmd, func(ctx context.Context, _ Cmder) error {
		var mu sync.Mutex
		var cmds []Cmder
		err := c.ForEachMaster(ctx, func(ctx context.Context, master *Client) error {
			shardCmd := NewClusterSlotStatsCmd(ctx, cmd.Args()...)
			if err := master.Process(ctx, shardCmd); err != nil {
				return err
			}

			mu.Lock()
			cmds = append(cmds, shardCmd)
			mu.Unlock()

			return nil
		})
		if err != nil {
			return err
		}
		val, err := aggregateClusterSlotStats(cmd, cmds)
		if err != nil {
			return err
		}
		cmd.val = val.([]ClusterSlotStat)
		return nil
	})
}

// processClusterHelper resolves the helper policy only after process hooks
// continue. Legacy helper fan-out and policy routing share the same hook call.
func (c *ClusterClient) processClusterHelper(ctx context.Context, cmd Cmder, legacy ProcessHook) {
	ctx = cscUnmarkedContext(ctx)
	err := c.withProcessHook(ctx, cmd, func(ctx context.Context, cmd Cmder) error {
		if c.usesLegacyClusterHelpers(ctx, cmd) {
			return legacy(ctx, cmd)
		}
		return c.process(ctx, cmd)
	})
	cmd.SetErr(err)
	cmd.setClusterRoutingDecision(nil)
}
