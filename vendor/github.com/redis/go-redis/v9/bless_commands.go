package redis

import "context"

type BlessFlag string

const (
	BlessNoEvict BlessFlag = "NO-EVICT"
)

type BlessCmdable interface {
	// BlessSet sets flag on key. It returns 1 if the flag was newly set and 0
	// if key already carried it.
	BlessSet(ctx context.Context, key string, flag BlessFlag) *IntCmd
	// BlessGet returns the flags currently set on key.
	BlessGet(ctx context.Context, key string) *StringSliceCmd
	// BlessClear removes flag from key. It returns 1 if the flag was present
	// and 0 otherwise.
	BlessClear(ctx context.Context, key string, flag BlessFlag) *IntCmd
	// BlessScan iterates the keys on a single server that carry flag, using
	// the usual SCAN cursor protocol. Pass count <= 0 to omit the COUNT hint.
	//
	// The cursor is only meaningful on the node that issued it, and each node
	// only knows about its own blessed keys. BLESS SCAN is keyless, so a
	// ClusterClient or Ring routes every call to an arbitrary node, and a
	// cursor returned by one node is useless on another: the iteration will
	// miss keys or never terminate. Do not call BlessScan on a ClusterClient
	// or Ring directly. Scan each master instead, e.g. with
	// ClusterClient.ForEachMaster or Ring.ForEachShard, and merge the pages.
	BlessScan(ctx context.Context, cursor uint64, flag BlessFlag, count int64) *ScanCmd
}

func (c cmdable) BlessSet(ctx context.Context, key string, flag BlessFlag) *IntCmd {
	args := []any{"bless", "set", key, string(flag)}
	cmd := NewIntCmd(ctx, args...)
	cmd.SetFirstKeyPos(2)
	_ = c(ctx, cmd)
	return cmd
}

func (c cmdable) BlessGet(ctx context.Context, key string) *StringSliceCmd {
	args := []any{"bless", "get", key}
	cmd := NewStringSliceCmd(ctx, args...)
	cmd.SetFirstKeyPos(2)
	_ = c(ctx, cmd)
	return cmd
}

func (c cmdable) BlessClear(ctx context.Context, key string, flag BlessFlag) *IntCmd {
	args := []any{"bless", "clear", key, string(flag)}
	cmd := NewIntCmd(ctx, args...)
	cmd.SetFirstKeyPos(2)
	_ = c(ctx, cmd)
	return cmd
}

// BlessScan iterates the blessed keys of a single server. The cursor is
// node-local, so on a ClusterClient or Ring scan each master separately; see
// BlessCmdable.BlessScan.
func (c cmdable) BlessScan(ctx context.Context, cursor uint64, flag BlessFlag, count int64) *ScanCmd {
	args := []any{"bless", "scan", cursor, string(flag)}
	if count > 0 {
		args = append(args, "count", count)
	}
	cmd := NewScanCmd(ctx, c, args...)
	_ = c(ctx, cmd)
	return cmd
}
