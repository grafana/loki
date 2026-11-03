# franz-go produce: bugs & races

You are analyzing the franz-go Kafka client library (pkg/kgo). Find correctness
bugs and race conditions in the PRODUCE codepath. Do not flag style, naming,
missing tests, or refactor opportunities.

Files in scope (read in this order):
- sink.go              per-broker produce loop; owns recBufs; recBufs own recBatch
- producer.go          producer abstraction, sink selection, promise completion
- partitioner.go       record -> partition assignment
- txn.go               GroupTransactSession; transactional epoch lifecycle
- metadata.go          mergeTopicPartitions (new-partitions loop), writablePartitions,
                       partitionsForTopicProduce, doPartition
- record_and_fetch.go  Record / Promise types
- client.go            cross-cutting: broker selection, retry, close

Invariants and conventions (assume these hold; don't re-derive):
- A recBuf has exactly one active writer at a time: the sink that owns it.
- Sequence numbers must be monotonic per (PID, epoch, partition).
- Within a partition, in-order delivery is REQUIRED; reordering between
  in-flight batches must be prevented.
- bufferedRecords semaphore bounds total in-flight records globally.
- PID/epoch lifecycle: InitProducerID -> [AddPartitionsToTxn ->] Produce
  -> [EndTxn]. KIP-890 (EndTxn v5+) bumps the epoch in EndTxn; retries
  must accept stale_epoch+1 == current.
- Lock ordering: c.mu -> g.mu.
- Context-key idiom is pointer-to-string (ctxPinReq, ctxRecRecycle, etc.);
  not a bug.

Known intentional behavior - DO NOT flag any of these:
- Produce capped at v12 when any partition lacks a TopicID (Event Hubs guard).
- OffsetFetch / OffsetCommit pinned to v9 when any topic lacks a TopicID
  (same reason); OffsetFetch topic-id resolution skipped below v10.
- recBatch field ordering chosen for atomic-64 alignment on 32-bit platforms.
- Per-partition sequence reset after a fatal idempotent error (e.g.
  UNKNOWN_PRODUCER_ID) - intentional recovery path.
- The "first finished promise wins" pattern for aborted batches.

Find only:
1. Data races: unsynchronized reads/writes, especially around sink/recBuf
   migration during metadata refresh.
2. Lost or duplicate writes: a record buffered but never produced; a record
   produced twice across retry/leader-change/txn-abort paths.
3. Out-of-order delivery: any sequence where two batches for the same
   partition can be acked in a different order than produced.
4. Idempotent/transactional lifecycle bugs: bad PID/epoch transitions,
   AddPartitions / EndTxn ordering, retries after stale epoch, fenced
   producer recovery, AddPartitions never sent for a partition we produce to.
5. Promise leaks: a record promise that never fires (success OR error)
   under some path - context cancel, client close, txn abort, etc.
6. Goroutine leaks past client.Close().
7. Off-by-one / boundary errors in batch sizing, sequences, partition counts.
8. TopicID resolution races with metadata refresh during in-flight produce.

Output per finding:
- Severity: critical (data loss / dup / corruption) | high (hang / leak)
            | medium (rare race, recoverable) | low
- File:line
- What:    one sentence
- How:     numbered walkthrough of goroutines/events that triggers it
- Fix:     one-paragraph sketch (not full code)

If a category yields nothing, say "none found" - don't pad. If you cannot
trace a finding to a concrete sequence of events, omit it.
