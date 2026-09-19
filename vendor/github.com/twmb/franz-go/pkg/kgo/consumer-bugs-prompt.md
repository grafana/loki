# franz-go consumer: bugs & races

You are analyzing the franz-go Kafka client library (pkg/kgo). Find correctness
bugs and race conditions in the CONSUMER codepath. Do not flag style, naming,
missing tests, or refactors.

Files in scope:
- source.go            per-broker fetch loop; owns cursors (one per partition)
- consumer.go          consumer abstraction, source/cursor management
- consumer_group.go    group consumer: join/sync/heartbeat, commits,
                       KIP-848 manage loop, static membership
- consumer_direct.go   user-assigned consumer; metadata-driven resolution
- txn.go               consume side of GroupTransactSession; read_committed
- metadata.go          cursor migration on partition reassignment
- client.go            close, broker selection, retry

Invariants (assume these hold):
- cursor.useState is atomic.Bool: Swap(true) -> fetchable;
  Store(false) -> in-flight / not fetchable.
- Cursor.source MUST be read BEFORE useState.Swap(true): after Swap, a
  concurrent fetch can complete and move() can overwrite c.source.
- move() is safe because it removes the cursor from the old source's list
  BEFORE Swap; no concurrent pickup until addCursor on the new source.
- Within a partition, fetched offsets must be monotonic; rewinds only via
  OffsetForLeaderEpoch / ListOffsets validation.
- read_committed drops aborted records using LSO + abortedTransactions.
- Lock ordering: c.mu -> g.mu. g.uncommitted protected by g.mu;
  usingCursors protected by c.mu.
- GroupTransactSession: users must not Poll concurrently with End().
- KIP-848 manage loop treats errChosenBrokerDead as retriable; not fatal.

Known intentional behavior - DO NOT flag:
- "load offsets, then validate via OffsetForLeaderEpoch" two-step on
  assignment.
- Cursor state-machine transitions between sources.
- ctxRecRecycle context value for Fetches pooling.
- Sharder fan-out for cross-broker requests.

Find only:
1. Data races, especially around cursor migration, source replacement,
   group state transitions, fetch session state.
2. Offset corruption: unjustified rewind; offsets advancing past records
   never yielded; double-yielded records.
3. Commit safety: committing offsets for a partition we no longer own;
   committing offsets for records the user hasn't acknowledged
   (autocommit modes); missing commits on close.
4. Aborted txn handling under read_committed: dropping records that should
   be yielded, or yielding records that should be dropped (LSO / aborted
   list edge cases).
5. Rebalance correctness:
   - eager: revoke fires before new assignment is used.
   - cooperative-sticky: only revoked partitions stop; retained partitions
     have no gap.
   - KIP-848: target reconciliation, member epoch bumps, lost-partition
     detection, fence handling.
6. Fetch session desync (KIP-227): client/broker disagree on session state.
7. Goroutine leaks past Close.
8. Channel close races (double-close, send on closed).
9. Static membership (KIP-345): instance ID handling during reconnect /
   fenced rejoin.

Output per finding:
- Severity: critical (data loss / dup / corruption) | high (hang / leak)
            | medium (rare race, recoverable) | low
- File:line
- What:    one sentence
- How:     numbered walkthrough of goroutines/events that triggers it
- Fix:     one-paragraph sketch (not full code)

If a category yields nothing, say "none found" - don't pad. If you cannot
trace a finding to a concrete sequence of events, omit it.
