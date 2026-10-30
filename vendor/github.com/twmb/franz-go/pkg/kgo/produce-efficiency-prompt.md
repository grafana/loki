# franz-go produce: efficiency

Find efficiency improvements in the PRODUCE codepath of franz-go (pkg/kgo).

Files in scope:
- sink.go              per-broker produce loop; owns recBufs; recBufs own recBatch
- producer.go          producer abstraction, sink selection, promise completion
- partitioner.go       record -> partition assignment
- txn.go               GroupTransactSession; transactional epoch lifecycle
- metadata.go          mergeTopicPartitions (new-partitions loop), writablePartitions,
                       partitionsForTopicProduce, doPartition
- record_and_fetch.go  Record / Promise types
- client.go            cross-cutting

Hot paths in priority order:
1. PER-RECORD: Client.Produce, partitioner, append to recBuf.
2. PER-BATCH:  assemble Produce request, encode/compress records, send.
3. PER-RESPONSE: parse Produce response, fire promises, advance recBatch.
4. PER-FLUSH: drain partitions, wait for in-flight to settle.

Already tuned - do not suggest these:
- recBatch reuse / sticky pooling.
- Sticky partitioner.
- bufferedRecords semaphore for bounded memory.
- Compression options (LZ4/Snappy/Gzip/Zstd) at batch granularity.
- TopicID-keyed produce (KIP-516) when broker supports it.

Find only:
1. Allocations on the PER-RECORD path: slice growth without preallocation,
   string<->[]byte conversion, interface boxing of concrete types, closure
   captures, time.Now overhead, log line construction not gated by level.
2. Allocations on the PER-BATCH path: byte slices that could come from a
   pool, repeated header building, varint encoding scratch space, repeated
   compressor instantiation.
3. Lock hold time in sink.go / recBuf: work under lock that could be done
   outside; broad locks that could be narrower; sleep/IO under lock.
4. Wasted CPU: redundant validation, recomputed values, unnecessary
   copying between buffers, recompression patterns.
5. Goroutine churn: per-record or per-batch goroutine spawn that could
   be amortized to per-broker.
6. Map/slice access patterns on hot path: lookups replaceable with indexed
   access; linear scans over partitions when an index exists.
7. Cache-line false sharing: fields written by different goroutines
   sitting in the same cache line.
8. Channel overhead on hot path: small unbuffered channels, repeated
   send/recv that could batch.

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
