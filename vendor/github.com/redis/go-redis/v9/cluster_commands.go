package redis

import (
	"context"
	"fmt"
)

type ClusterCmdable interface {
	ClusterMyShardID(ctx context.Context) *StringCmd
	ClusterMyID(ctx context.Context) *StringCmd
	ClusterSlots(ctx context.Context) *ClusterSlotsCmd
	ClusterShards(ctx context.Context) *ClusterShardsCmd
	ClusterLinks(ctx context.Context) *ClusterLinksCmd
	ClusterSlotStats(ctx context.Context, startSlot, endSlot int) *ClusterSlotStatsCmd
	ClusterSlotStatsOrderBy(ctx context.Context, a *ClusterSlotStatsOrderByArgs) *ClusterSlotStatsCmd
	ClusterNodes(ctx context.Context) *StringCmd
	ClusterMeet(ctx context.Context, host, port string) *StatusCmd
	ClusterForget(ctx context.Context, nodeID string) *StatusCmd
	ClusterReplicate(ctx context.Context, nodeID string) *StatusCmd
	ClusterResetSoft(ctx context.Context) *StatusCmd
	ClusterResetHard(ctx context.Context) *StatusCmd
	ClusterInfo(ctx context.Context) *StringCmd
	ClusterKeySlot(ctx context.Context, key string) *IntCmd
	ClusterGetKeysInSlot(ctx context.Context, slot int, count int) *StringSliceCmd
	ClusterCountFailureReports(ctx context.Context, nodeID string) *IntCmd
	ClusterCountKeysInSlot(ctx context.Context, slot int) *IntCmd
	ClusterDelSlots(ctx context.Context, slots ...int) *StatusCmd
	ClusterDelSlotsRange(ctx context.Context, min, max int) *StatusCmd
	ClusterSaveConfig(ctx context.Context) *StatusCmd
	ClusterSlaves(ctx context.Context, nodeID string) *StringSliceCmd
	ClusterFailover(ctx context.Context) *StatusCmd
	ClusterAddSlots(ctx context.Context, slots ...int) *StatusCmd
	ClusterAddSlotsRange(ctx context.Context, min, max int) *StatusCmd
	ReadOnly(ctx context.Context) *StatusCmd
	ReadWrite(ctx context.Context) *StatusCmd
}

func (c cmdable) ClusterMyShardID(ctx context.Context) *StringCmd {
	cmd := NewStringCmd(ctx, "cluster", "myshardid")
	_ = c(ctx, cmd)
	return cmd
}

func (c cmdable) ClusterMyID(ctx context.Context) *StringCmd {
	cmd := NewStringCmd(ctx, "cluster", "myid")
	_ = c(ctx, cmd)
	return cmd
}

// ClusterSlots returns the mapping of cluster slots to nodes.
//
// Deprecated: Use ClusterShards instead as of Redis 7.0.0.
func (c cmdable) ClusterSlots(ctx context.Context) *ClusterSlotsCmd {
	cmd := NewClusterSlotsCmd(ctx, "cluster", "slots")
	_ = c(ctx, cmd)
	return cmd
}

func (c cmdable) ClusterShards(ctx context.Context) *ClusterShardsCmd {
	cmd := NewClusterShardsCmd(ctx, "cluster", "shards")
	_ = c(ctx, cmd)
	return cmd
}

func (c cmdable) ClusterLinks(ctx context.Context) *ClusterLinksCmd {
	cmd := NewClusterLinksCmd(ctx, "cluster", "links")
	_ = c(ctx, cmd)
	return cmd
}

// ClusterSlotStats returns the usage statistics of the slots in the inclusive
// range [startSlot, endSlot], ordered by slot. A node reports only the slots it
// owns, so a ClusterClient asks every master and merges the replies. In a
// pipeline the command is still sent to one node and returns only the slots
// of that node.
//
// Only KeyCount is reported unless the server runs with
// cluster-slot-stats-enabled yes. Requires Redis 8.2 or newer.
func (c cmdable) ClusterSlotStats(ctx context.Context, startSlot, endSlot int) *ClusterSlotStatsCmd {
	cmd := NewClusterSlotStatsCmd(ctx, "cluster", "slot-stats", "slotsrange", startSlot, endSlot)
	_ = c(ctx, cmd)
	return cmd
}

// ClusterSlotStatsOrderByArgs are the options of CLUSTER SLOT-STATS ORDERBY.
type ClusterSlotStatsOrderByArgs struct {
	// Metric to order by: "key-count", "memory-bytes", "cpu-usec",
	// "network-bytes-in" or "network-bytes-out".
	Metric string
	// Limit is the maximum number of slots to return. Zero returns all of them.
	Limit int
	// Asc orders from the lowest value. The default is from the highest.
	Asc bool
}

// ClusterSlotStatsOrderBy returns the usage statistics of the slots ordered by
// a metric; slots with the same value are ordered by slot. A node reports only
// the slots it owns, so a ClusterClient asks every master, merges the replies
// and applies the order and the limit again. In a pipeline the command is
// still sent to one node and returns only the slots of that node.
//
// Only KeyCount is reported unless the server runs with
// cluster-slot-stats-enabled yes. Requires Redis 8.2 or newer.
func (c cmdable) ClusterSlotStatsOrderBy(ctx context.Context, a *ClusterSlotStatsOrderByArgs) *ClusterSlotStatsCmd {
	cmd := NewClusterSlotStatsCmd(ctx, clusterSlotStatsOrderByArgs(a)...)
	_ = c(ctx, cmd)
	return cmd
}

func clusterSlotStatsOrderByArgs(a *ClusterSlotStatsOrderByArgs) []interface{} {
	args := []interface{}{"cluster", "slot-stats", "orderby", a.Metric}
	if a.Limit != 0 {
		args = append(args, "limit", a.Limit)
	}
	if a.Asc {
		args = append(args, "asc")
	}
	return args
}

func (c cmdable) ClusterNodes(ctx context.Context) *StringCmd {
	cmd := NewStringCmd(ctx, "cluster", "nodes")
	_ = c(ctx, cmd)
	return cmd
}

func (c cmdable) ClusterMeet(ctx context.Context, host, port string) *StatusCmd {
	cmd := NewStatusCmd(ctx, "cluster", "meet", host, port)
	_ = c(ctx, cmd)
	return cmd
}

func (c cmdable) ClusterForget(ctx context.Context, nodeID string) *StatusCmd {
	cmd := NewStatusCmd(ctx, "cluster", "forget", nodeID)
	_ = c(ctx, cmd)
	return cmd
}

func (c cmdable) ClusterReplicate(ctx context.Context, nodeID string) *StatusCmd {
	cmd := NewStatusCmd(ctx, "cluster", "replicate", nodeID)
	_ = c(ctx, cmd)
	return cmd
}

func (c cmdable) ClusterResetSoft(ctx context.Context) *StatusCmd {
	cmd := NewStatusCmd(ctx, "cluster", "reset", "soft")
	_ = c(ctx, cmd)
	return cmd
}

func (c cmdable) ClusterResetHard(ctx context.Context) *StatusCmd {
	cmd := NewStatusCmd(ctx, "cluster", "reset", "hard")
	_ = c(ctx, cmd)
	return cmd
}

func (c cmdable) ClusterInfo(ctx context.Context) *StringCmd {
	cmd := NewStringCmd(ctx, "cluster", "info")
	_ = c(ctx, cmd)
	return cmd
}

func (c cmdable) ClusterKeySlot(ctx context.Context, key string) *IntCmd {
	cmd := NewIntCmd(ctx, "cluster", "keyslot", key)
	_ = c(ctx, cmd)
	return cmd
}

func (c cmdable) ClusterGetKeysInSlot(ctx context.Context, slot int, count int) *StringSliceCmd {
	cmd := NewStringSliceCmd(ctx, "cluster", "getkeysinslot", slot, count)
	_ = c(ctx, cmd)
	return cmd
}

func (c cmdable) ClusterCountFailureReports(ctx context.Context, nodeID string) *IntCmd {
	cmd := NewIntCmd(ctx, "cluster", "count-failure-reports", nodeID)
	_ = c(ctx, cmd)
	return cmd
}

func (c cmdable) ClusterCountKeysInSlot(ctx context.Context, slot int) *IntCmd {
	cmd := NewIntCmd(ctx, "cluster", "countkeysinslot", slot)
	_ = c(ctx, cmd)
	return cmd
}

func (c cmdable) ClusterDelSlots(ctx context.Context, slots ...int) *StatusCmd {
	args := make([]interface{}, 2+len(slots))
	args[0] = "cluster"
	args[1] = "delslots"
	for i, slot := range slots {
		args[2+i] = slot
	}
	cmd := NewStatusCmd(ctx, args...)
	_ = c(ctx, cmd)
	return cmd
}

// slotsRange expands the inclusive range [min, max] into the slot list the
// CLUSTER ADDSLOTS/DELSLOTS commands take. A range whose length comes out
// negative or overflows would panic in make(), so it is reported as an error.
func slotsRange(min, max int) ([]int, error) {
	if max < min {
		return nil, fmt.Errorf("redis: invalid slot range %d..%d: max is below min", min, max)
	}
	size := max - min + 1
	if size <= 0 {
		return nil, fmt.Errorf("redis: invalid slot range %d..%d: range is too large", min, max)
	}
	slots := make([]int, size)
	for i := range slots {
		slots[i] = min + i
	}
	return slots, nil
}

func (c cmdable) ClusterDelSlotsRange(ctx context.Context, min, max int) *StatusCmd {
	slots, err := slotsRange(min, max)
	if err != nil {
		cmd := NewStatusCmd(ctx)
		cmd.SetErr(err)
		return cmd
	}
	return c.ClusterDelSlots(ctx, slots...)
}

func (c cmdable) ClusterSaveConfig(ctx context.Context) *StatusCmd {
	cmd := NewStatusCmd(ctx, "cluster", "saveconfig")
	_ = c(ctx, cmd)
	return cmd
}

// ClusterSlaves lists the replica nodes of a master node.
//
// Deprecated: Use ClusterReplicas instead as of Redis 5.0.0.
func (c cmdable) ClusterSlaves(ctx context.Context, nodeID string) *StringSliceCmd {
	cmd := NewStringSliceCmd(ctx, "cluster", "slaves", nodeID)
	_ = c(ctx, cmd)
	return cmd
}

func (c cmdable) ClusterFailover(ctx context.Context) *StatusCmd {
	cmd := NewStatusCmd(ctx, "cluster", "failover")
	_ = c(ctx, cmd)
	return cmd
}

func (c cmdable) ClusterAddSlots(ctx context.Context, slots ...int) *StatusCmd {
	args := make([]interface{}, 2+len(slots))
	args[0] = "cluster"
	args[1] = "addslots"
	for i, num := range slots {
		args[2+i] = num
	}
	cmd := NewStatusCmd(ctx, args...)
	_ = c(ctx, cmd)
	return cmd
}

func (c cmdable) ClusterAddSlotsRange(ctx context.Context, min, max int) *StatusCmd {
	slots, err := slotsRange(min, max)
	if err != nil {
		cmd := NewStatusCmd(ctx)
		cmd.SetErr(err)
		return cmd
	}
	return c.ClusterAddSlots(ctx, slots...)
}

func (c cmdable) ReadOnly(ctx context.Context) *StatusCmd {
	cmd := NewStatusCmd(ctx, "readonly")
	_ = c(ctx, cmd)
	return cmd
}

func (c cmdable) ReadWrite(ctx context.Context) *StatusCmd {
	cmd := NewStatusCmd(ctx, "readwrite")
	_ = c(ctx, cmd)
	return cmd
}
