# kfake Development Context

## Running

Standalone server. main.go carries `//go:build none` so `go test ./...` skips it; naming the file makes go ignore that constraint:
```bash
go run main.go -l debug    # -l/--log-level: none, error, warn, info, debug
```
Do not add `-tags none`: it pulls main.go into package kfake and the build fails with "found packages kfake and main".

Test suite:
```bash
./run_tests.sh [options]
  -t, --test PATTERN     Test pattern (Txn, Group, Txn/range, Group/sticky)
  -n, --iterations NUM   Max iterations (default: 50)
  -r, --records NUM      Number of records (default: 500000)
  --race                 Enable race detector
  -l, --log-level LEVEL  Log level for both client and server (debug, info)
  --client-log LEVEL     Log level for kgo test client only
  --server-log LEVEL     Log level for kfake server only
  -v, --version VERSION  Kafka version to emulate (e.g., 2.8, 3.5)
  --pprof ADDR           Enable pprof on the server (e.g., :6060)
  --data-dir DIR         Persistence directory for the kfake server
  --restart SECS         Kill and restart the server after SECS seconds (requires --data-dir)
  --timeout DURATION     Test timeout (default: 180s, 450s with --race)
  --keep-logs            Keep per-iteration logs (client_N.log, server_N.log)
  --clean                Kill servers and remove /tmp/kfake_test_logs
  -k, --kill             Kill processes on ports 9092-9094 and exit
```

Logs go to `/tmp/kfake_test_logs/` (server.log, client.log). On test failure, the server stays alive for debugging.

## Consumer Group (Next-Gen / KIP-848)

The kgo client gates 848 behind `kgo.ServerSideBalancer()`. `newClient848` in helpers_test.go passes it; a direct `kgo.NewClient` with `kgo.ConsumerGroup` must add the option itself.

Assignors: uniform (default, maps to kgo sticky balancer) and range.

## Transactions

- KIP-890: Produce v12+ implicitly adds partitions via `pids.get()` with a non-nil `pd`. EndTxn v5+ bumps the epoch after each transaction; an EndTxn retry with `epoch == serverEpoch-1` and `!inTx` returns success with the current epoch.
- State is `inTx` + `lastWasCommit`, not a state machine: completion is synchronous, so there are no PREPARE/COMPLETE states.
- All txn coordinator epoch errors use PRODUCER_FENCED; only the produce path uses INVALID_PRODUCER_EPOCH.
- InitProducerID accepts epoch <= server epoch, so a stale epoch from a timeout bump recovers instead of retrying forever. Idempotent (nil txid) always gets a fresh PID.
- KIP-447: OffsetFetch with `RequireStable=true` returns `UNSTABLE_OFFSET_COMMIT` when `pids.hasUnstableOffsets(group)`.

**Concurrency**: everything runs on `Cluster.run()`, nothing has a goroutine of its own, and timers hand their work back through the loop.

## ACLs

**Config options:**
- `EnableACLs()` - Enable ACL checking
- `Superuser(method, user, pass)` - Add superuser (bypasses ACL checks)
- `User(method, user, pass, acls...)` - Add user with optional seed ACLs

**Adding ACL checks to new handlers:**

When implementing a new Kafka protocol handler, you MUST add ACL checks. Kafka's authorization documentation says which resources and operations each request checks.

Handlers do not call allowedACL directly: c.deny and c.denyCluster check the
ACL and the faults for that entity together (see the section below).

Common patterns:
```go
// Check specific resource
if e := c.deny(creq, topicName, kmsg.ACLResourceTypeTopic, kmsg.ACLOperationRead, faultKey{topic: topicName}); e != nil {
    return errResp(e.Code), nil
}

// Check cluster-level operation
if e := c.denyCluster(creq, kmsg.ACLOperationAlter); e != nil {
    return errResp(e.Code), nil
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

## Faults and authorization in handlers

Adding a request:
- `regKey(NN, min, max)` in `NN_<name>.go`, the handler in the `cluster.go` switch, `checkReqVersion` first.

Authorization and faults:
- Check every resource the request touches through `c.deny` (topic, group, transactional ID: pass the resource type and operation) or `c.denyCluster`.
- Check beside the point the handler emits that entity's error, before any side effect.
- A request that carries partitions also calls `creq.faults.check` inside the partition loop, with the partition in the key.
- A request with nothing to authorize goes in the `entityless` set in `faults.go`.

The fault key:
- Name every identifier the request carries that is known at that site: topic, topicID, partition, group, txnID, resource.
- A selector the key does not name never matches, so a missing field silently drops faults.

Emission:
- Answer first with `e.Code` (and `e.Message` where the response carries one), then:
```go
if creq.skipsWork(e) {
    continue
}
```
- A kind whose real broker can answer REQUEST_TIMED_OUT after applying the work goes in the `afterApply` table in `faults.go`.
- Such a kind's emission closure keeps the first answer for an entity; see `donep` in `00_produce.go`.

Tests after touching a handler:
- `TestFaultCoverage`: every registered key answers a selector-less fault.
- The ACL tests for that request.
- The package with `-race`.

Comments:
- ASCII, terse, plain statements. "we" is the broker, "you" is the caller.

## Writing tests

- `t.Parallel()` by default; each test gets its own cluster from `newCluster`.
- Keep sleeps and timeouts minimal: kfake is in-process. Use `kgo.FetchMaxWait(250*time.Millisecond)` where partition discovery speed matters.
- New tests go in an existing bucket file such as `issues_test.go`, not in a file named after the feature. Split a class out only when it is large on its own, about 1k lines: transactions, consumer, share consume.
