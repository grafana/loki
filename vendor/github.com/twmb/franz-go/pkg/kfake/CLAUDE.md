# kfake Development Context

## Overview

kfake is a fake Kafka broker implementation for testing the kgo client library. Supports transactions, consumer groups, fetch sessions, ACLs, and full produce/fetch lifecycle.

## Running

Standalone server (has `//go:build none` tag):
```bash
go run -tags none main.go -l debug    # -l/--log-level: none, error, warn, info, debug
```

Test suite:
```bash
./run_tests.sh [options]
  -t, --test PATTERN     Test pattern (Txn, Group, Txn/range, Group/sticky)
  -n, --iterations NUM   Max iterations (default: 50)
  -r, --records NUM      Number of records (default: 500000)
  --race                 Enable race detector (uses 5min timeout)
  -l, --log-level LEVEL  Log level for both client and server (debug, info)
  --client-log LEVEL     Log level for kgo test client only
  --server-log LEVEL     Log level for kfake server only
  --clean                Kill servers and remove /tmp/kfake_test_logs
  -k, --kill             Kill processes on ports 9092-9094 and exit
```

Logs go to `/tmp/kfake_test_logs/` (server.log, client.log). On test failure, the server stays alive for debugging.

Run specific tests with standard Go:
```bash
go test -v -run TestACL ./pkg/kfake/   # ACL tests
go test -v -run TestGroup ./pkg/kfake/ # Group tests
```

Probe a live kfake server with rpk:
```bash
rpk topic list -X brokers=localhost:9092
rpk topic consume foo -X brokers=localhost:9092
rpk group describe <group> -X brokers=localhost:9092
```

Analyze test logs:
```bash
python3 tools/analyze_logs.py dual-assign                     # 848 dual-assignment failure
python3 tools/analyze_logs.py txn-timeout                     # transaction timeout root cause
python3 tools/analyze_logs.py client-trace client.log -p 3    # client debug log tracing
rpk cluster txn list -X brokers=localhost:9092                # live transaction state
```

## Key Files

- `00_produce.go` - Produce handler with transaction support
- `01_fetch.go` - Fetch handler with read_committed support and fetch sessions (KIP-227)
- `txns.go` - Transaction coordinator, EndTxn, TxnOffsetCommit
- `data.go` - Partition data, LSO calculation, batch storage, config types
- `groups.go` - Consumer group management, JoinGroup/SyncGroup/Heartbeat
- `acl.go` - ACL storage, checking logic, and authorized operations
- `config.go` - Cluster configuration options
- `cluster.go` - Main cluster struct, request routing, state initialization

## Broker Config Handling

Broker configs (`bcfgs`) are stored as `atomic.Pointer[map[string]*string]` on the cluster for race-safe access. Writes use copy-on-write via `storeBcfgs`. `validateBrokerConfig` (standalone function) validates types (Int/Long must be numeric) before writes.

Key config-driven values:
- `consumerHeartbeatIntervalMs()` reads `group.consumer.heartbeat.interval.ms` from bcfgs, default `defHeartbeatInterval` (5000 normally, 100 in test binaries)
- `maxMessageBytes()` reads from topic config then bcfgs, default `defMaxMessageBytes` (1048588)

Config maps in `data.go`: `validBrokerConfigs`, `validTopicConfigs`, `configDefaults`, `configTypes`

## Consumer Group State Machine (Classic)

States: `Empty` -> `PreparingRebalance` -> `CompletingRebalance` -> `Stable`

Rebalance triggers in `handleJoin`:
- **Stable**: Rebalance if leader rejoins OR any member's metadata changed
- **CompletingRebalance**: If member has different metadata, trigger new rebalance

## Consumer Group (Next-Gen / KIP-848)

kfake supports the next-gen consumer group protocol (ConsumerGroupHeartbeat API). The kgo client gates this behind a context opt-in: `should848()` checks for context value `"opt_in_kafka_next_gen_balancer_beta"`.

Key files:
- `groups.go` - kfake handler for ConsumerGroupHeartbeat, member/epoch management, assignment computation
- `pkg/kgo/consumer_group_848.go` - kgo client-side 848 lifecycle

Test helpers (`newClient` in behavior_test.go and helpers_test.go) automatically set the opt-in context. Direct `kgo.NewClient` calls with `kgo.ConsumerGroup` must add `kgo.WithContext(context.WithValue(ctx, "opt_in_kafka_next_gen_balancer_beta", true))` manually. The context.WithValue line needs `//nolint:revive,staticcheck` to pass lint.

KIP-848 assignors: uniform (default, maps to kgo sticky balancer) and range.

The heartbeat interval defaults to 5000ms but is lowered when testing to support faster partition movement reconciliation.

### 848 Reconciliation

The reconciliation follows Kafka's cooperative assignment builder pattern. Per-member state:
- `lastReconciledSent` - what the server told the member to own
- `partitionsPendingRevocation` - partitions told to revoke, awaiting client confirmation
- `targetAssignment` - what the server wants the member to own (from `computeTargetAssignment`)

Both `lastReconciledSent` and `partitionsPendingRevocation` contribute to the partition epoch map. A partition with epoch != -1 is owned and can't be claimed by another member.

Member states: `cmStable`, `cmUnrevokedPartitions`, `cmUnreleasedPartitions`. Dual epochs: `groupEpoch` (bumped on membership/subscription changes) and `targetAssignmentEpoch` (set after target computation).

`maybeReconcile` dispatches based on member state to either `computeNextAssignment` (full reconciliation) or `updateCurrentAssignment` (fast path for subscription changes). `computeNextAssignment` has 4 branches:
- a) Revocations AND client owns them - stay at current epoch, wait
- b) New partitions to assign - advance epoch, assign them
- c) Unreleased partitions only - advance epoch, wait
- d) Stable - advance epoch, no change

Key helpers: `updateMemberSubscriptions` (shared by rejoin + regular heartbeat), `evictConsumerMember` (shared by session + rebalance timeout), `clearConfirmedRevocations` (removes partitions client confirmed releasing).

Revocation lifecycle:
1. Server computes reconciled without partition P (target changed)
2. P added to `partitionsPendingRevocation`, epoch stays alive
3. Client's stale heartbeat Topics still has P - pendingRevoke kept
4. Client receives response without P, revokes, sends Topics without P
5. Server clears P from pendingRevoke, epoch removed
6. P is free for another member

Keepalive heartbeats (Topics=nil) conservatively retain all pending revocations.

## Transaction Flow

1. `InitProducerID` - Get producer ID and epoch
2. `AddPartitionsToTxn` - Register partitions (pre-KIP-890 only)
3. `Produce` - Send transactional batches (`inTx=true`)
4. `AddOffsetsToTxn` - Register consumer group for offset commit
5. `TxnOffsetCommit` - Stage offset commits in `pidinf.txOffsets`
6. `EndTxn` - Commit/abort: marks batches, writes control batch, applies offsets, calls `recalculateLSO()`

**KIP-890**: Produce v12+ implicitly adds partitions via `pids.get()` with non-nil `pd` parameter. EndTxn v5+ bumps epoch after each transaction. EndTxn retries are handled idempotently: if `epoch == serverEpoch-1` and `!inTx`, return success with current epoch.

**Transaction state**: kfake uses `inTx` (bool) + `lastWasCommit` (bool) instead of the full state machine. Synchronous completion means no PREPARE/COMPLETE states needed. All txn coordinator epoch errors use PRODUCER_FENCED; only the produce path uses INVALID_PRODUCER_EPOCH.

**InitProducerID recovery**: accepts epoch <= server epoch (not just ==). Stale epochs from timeout bumps trigger recovery, not infinite retry. Idempotent (nil txid) always gets a fresh PID.

**Concurrency**: all pids state runs directly in Cluster.run() - no separate goroutine. Handler files call doXxx methods directly. Transaction timeout timer is in the Cluster.run() select loop. The only cross-goroutine coordination remaining is `g.waitControl()` for applying committed offsets to group state (group goroutine owns group commits).

**KIP-447**: OffsetFetch with `RequireStable=true` returns `UNSTABLE_OFFSET_COMMIT` if there are pending transactional offset commits for the group. Checked via `pids.hasUnstableOffsets(group)`.

## LSO (Last Stable Offset)

- `read_committed` consumers read up to LSO only
- LSO = earliest uncommitted transaction offset, or HWM if none
- `recalculateLSO()` scans batches for `inTx=true && !aborted`

## Watch Fetch Mechanism

When MinBytes is not satisfied, a `watchFetch` is created and registered on partitions (`pd.watch`).

Byte counting toward MinBytes:
- `w.push(pd, nbytes)` - Called when batches are pushed. For `readCommitted`, skips if `pd.inTx=true`
- `w.addBytes(pd, nbytes)` - Core method that counts bytes and fires watcher if MinBytes satisfied
- On transaction commit, `txns.go` calls `w.addBytes()` directly for `readCommitted` watchers to count the now-committed bytes (tracked in `pidinf.txPartBytes`)

## Fetch Sessions (KIP-227)

Sessions are scoped per broker node ID.

| SessionID | Epoch | Meaning |
|-----------|-------|---------|
| 0 | -1 | Legacy mode, no session |
| >0 | -1 | Unregister/kill session |
| any | 0 | Create new session |
| >0 | >0 | Use existing session (incremental fetch) |

## ACLs

ACL support is in `acl.go`. Uses kmsg types directly (`kmsg.ACLResourceType`, `kmsg.ACLOperation`, etc.).

**Key concepts:**
- DENY always takes precedence over ALLOW
- Implied permissions: DESCRIBE implied by READ/WRITE/DELETE/ALTER; DESCRIBE_CONFIGS implied by ALTER_CONFIGS
- Superusers (configured via `Superuser()` option) bypass all ACL checks
- Pattern types: Literal (exact match) and Prefixed (prefix match)

**Config options:**
- `EnableACLs()` - Enable ACL checking
- `Superuser(method, user, pass)` - Add superuser (bypasses ACL checks)
- `User(method, user, pass, acls...)` - Add user with optional seed ACLs

**Adding ACL checks to new handlers:**

When implementing a new Kafka protocol handler, you MUST add ACL checks. I will provide links to Kafka documentation specifying which resources and operations to check.

Common patterns:
```go
// Check specific resource
if !c.allowedACL(creq, topicName, kmsg.ACLResourceTypeTopic, kmsg.ACLOperationRead) {
    return errResp(kerr.TopicAuthorizationFailed.Code), nil
}

// Check cluster-level operation
if !c.allowedClusterACL(creq, kmsg.ACLOperationAlter) {
    return errResp(kerr.ClusterAuthorizationFailed.Code), nil
}

// Check if user has ANY permission on resource type (e.g., InitProducerID without txn)
if !c.anyAllowedACL(creq, kmsg.ACLResourceTypeCluster, kmsg.ACLOperationIdempotentWrite) {
    return errResp(kerr.ClusterAuthorizationFailed.Code), nil
}
```

**Resource types:** Topic, Group, Cluster, TransactionalId

**Common operations by resource:**
| Resource | Operations |
|----------|------------|
| Topic | Read, Write, Create, Delete, Alter, Describe, DescribeConfigs, AlterConfigs |
| Group | Read, Delete, Describe |
| Cluster | Create, Alter, Describe, ClusterAction, AlterConfigs, DescribeConfigs, IdempotentWrite |
| TransactionalId | Describe, Write |

## Authorized Operations (KIP-430)

Several requests support `IncludeAuthorizedOperations` fields that return a bitfield of operations the user is permitted to perform:

- **DescribeCluster**: `IncludeClusterAuthorizedOperations` → `ClusterAuthorizedOperations`
- **Metadata**: `IncludeClusterAuthorizedOperations` → `AuthorizedOperations` (cluster), `IncludeTopicAuthorizedOperations` → per-topic `AuthorizedOperations`
- **DescribeGroups**: `IncludeAuthorizedOperations` → per-group `AuthorizedOperations`

The bitfield format: each bit position corresponds to `kmsg.ACLOperation(i)`. Implementation is in `acl.go`:
- `c.clusterAuthorizedOps(creq)` - cluster operations
- `c.topicAuthorizedOps(creq, topic)` - topic operations
- `c.groupAuthorizedOps(creq, group)` - group operations
