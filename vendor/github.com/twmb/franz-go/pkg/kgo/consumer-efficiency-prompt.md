# franz-go consumer: efficiency

Find efficiency improvements in the CONSUMER codepath of franz-go (pkg/kgo).

Files in scope:
- source.go            per-broker fetch loop; owns cursors (one per partition)
- consumer.go          consumer abstraction, source/cursor management
- consumer_group.go    group consumer: join/sync/heartbeat, commits,
                       KIP-848 manage loop, static membership
- consumer_direct.go   user-assigned consumer; metadata-driven resolution
- txn.go               consume side of GroupTransactSession; read_committed
- metadata.go          cursor migration on partition reassignment
- client.go            close, broker selection, retry

Hot paths in priority order:
1. PER-FETCH-RESPONSE: parse, decompress, decode records, yield via PollFetches.
2. PER-RECORD: any handling that scales with record count - key/value/header
   allocation, header parsing, time conversion.
3. PER-FETCH: pick cursors, build Fetch request, send.
4. PER-COMMIT: build OffsetCommit, send (usually low rate).
5. PER-HEARTBEAT / PER-REBALANCE: low rate but latency-sensitive.

Already tuned - do not suggest:
- Fetch buffer pooling.
- Record / Fetches recycling via ctxRecRecycle.
- cursor.useState atomic state.
- Fetch sessions (KIP-227).

Find only:
1. Allocations during fetch response parsing scaling with record count:
   key/value/header byte slices, RecordHeader slice growth, string headers,
   time conversions per record.
2. Decompression: are decompressors and output buffers pooled across
   fetches? Are we decompressing whole batches when read_committed will
   drop most records?
3. Lock contention on consumer / group mutexes during fetch yield;
   broader locks than necessary.
4. Cursor list iteration: linear scans where indexed access works.
5. Group: redundant metadata refreshes, redundant coordinator lookups,
   redundant SyncGroup payloads.
6. Heartbeat / manage loop overhead scaling with assignment size that
   could be cached.
7. Aborted txn handling under read_committed: per-record work that could
   be amortized to per-batch using LSO + aborted list.
8. Goroutine reuse: per-fetch goroutines that could be reused per-broker.

Output per finding:
- Cost class: per-record | per-batch | per-response | per-flush | startup
- File:line
- What: one sentence
- Cost: e.g. "allocates a 64-byte slice per record; 1M rec/s -> 60MB/s garbage"
- Fix:  sketch

Skip: micro-optimizations on cold paths (config parsing, client init,
error paths). Skip "consider sync.Pool" unless you've identified the
specific allocation hot spot it would address. Skip anything where you
can't name the cost class.
