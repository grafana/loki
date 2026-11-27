package redis

import "context"

// BACKUP commands are only available on standalone *Client instances.
// A backup belongs to one node: STATUS, LIST, SEAL, ABORT and CLEANUP must
// reach the node that ran START. ClusterClient, Ring and UniversalClient would
// route each call on its own, so they don't expose these methods. To back up
// every primary of a cluster, run the commands on each shard's client:
//
//	err := cluster.ForEachMaster(ctx, func(ctx context.Context, node *redis.Client) error {
//	    return node.BackupStart(ctx).Err()
//	})
//
// A typical backup: BackupStart, then BackupStatus until the state is
// "incrementing", then BackupSeal. Copy the files from BackupList, and finish
// with BackupCleanup.

// BackupStart starts a backup of this node.
// Redis 8.10+. https://redis.io/docs/latest/commands/backup-start/
func (c *Client) BackupStart(ctx context.Context) *StatusCmd {
	cmd := NewStatusCmd(ctx, "backup", "start")
	_ = c.Process(ctx, cmd)
	return cmd
}

// BackupStatus returns the state of this node's backup.
// Redis 8.10+. https://redis.io/docs/latest/commands/backup-status/
func (c *Client) BackupStatus(ctx context.Context) *BackupStatusCmd {
	cmd := NewBackupStatusCmd(ctx, "backup", "status")
	_ = c.Process(ctx, cmd)
	return cmd
}

// BackupList returns the absolute paths of the files the backup has pinned so far.
// Redis 8.10+. https://redis.io/docs/latest/commands/backup-list/
func (c *Client) BackupList(ctx context.Context) *StringSliceCmd {
	cmd := NewStringSliceCmd(ctx, "backup", "list")
	_ = c.Process(ctx, cmd)
	return cmd
}

// BackupSeal ends the backup. The backup must be in the "incrementing" state.
// Redis 8.10+. https://redis.io/docs/latest/commands/backup-seal/
func (c *Client) BackupSeal(ctx context.Context) *StatusCmd {
	cmd := NewStatusCmd(ctx, "backup", "seal")
	_ = c.Process(ctx, cmd)
	return cmd
}

// BackupAbort cancels a backup that hasn't been sealed. Its state becomes "failed".
// Redis 8.10+. https://redis.io/docs/latest/commands/backup-abort/
func (c *Client) BackupAbort(ctx context.Context) *StatusCmd {
	cmd := NewStatusCmd(ctx, "backup", "abort")
	_ = c.Process(ctx, cmd)
	return cmd
}

// BackupCleanup removes the backup's files and returns the node to the "idle" state.
// Redis 8.10+. https://redis.io/docs/latest/commands/backup-cleanup/
func (c *Client) BackupCleanup(ctx context.Context) *StatusCmd {
	cmd := NewStatusCmd(ctx, "backup", "cleanup")
	_ = c.Process(ctx, cmd)
	return cmd
}
