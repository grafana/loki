package builder

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/grafana/loki/v3/pkg/kafka"
	"github.com/grafana/loki/v3/pkg/logline"
	"github.com/grafana/loki/v3/pkg/logline/shard"
	"github.com/grafana/loki/v3/pkg/logproto"
)

// pipelineTestEntries builds a deterministic multi-date dataset large enough
// to force many spills per worker under a tiny postings buffer.
func pipelineTestEntries(n int) []logproto.Entry {
	base := time.Date(2026, 3, 22, 23, 59, 0, 0, time.UTC) // straddles UTC midnight
	lines := []string{
		"error connecting to database server timeout",
		"user alice logged in from console device",
		"payment processed for order 12345 succeeded",
		"warning disk usage high on node seventeen",
		"request %d failed with status 500 at endpoint alpha",
	}
	entries := make([]logproto.Entry, 0, n)
	for i := 0; i < n; i++ {
		line := lines[i%len(lines)]
		if strings.Contains(line, "%d") {
			line = fmt.Sprintf(line, i%13)
		}
		entries = append(entries, logproto.Entry{
			Timestamp: base.Add(time.Duration(i) * time.Second),
			Line:      line,
		})
	}
	return entries
}

// waitForGoroutineBaseline polls NumGoroutine down to the captured baseline.
// A plain loop, not require.Eventually: Eventually runs its condition in a
// fresh goroutine per tick, which would keep the count above a tight baseline
// forever.
func waitForGoroutineBaseline(t *testing.T, baseline int, msg string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for runtime.NumGoroutine() > baseline && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	require.LessOrEqual(t, runtime.NumGoroutine(), baseline, msg)
}

// feedInChunks streams entries through processStream in small per-call chunks
// so pipeline mode scatters items across the competing workers (a single call
// is one queue item and would land on one worker).
func feedInChunks(t *testing.T, b *indexBuilder, entries []logproto.Entry, chunk int) {
	t.Helper()
	for start := 0; start < len(entries); start += chunk {
		end := start + chunk
		if end > len(entries) {
			end = len(entries)
		}
		s := logproto.Stream{Entries: entries[start:end]}
		require.NoError(t, b.processStream(&s, nil, time.Now(), recordRef{}))
	}
}

// TestBuilder_RoundTrip_ExtractThreads is the routing-invariance proof: the
// TestBuilder_RoundTrip technique (semantic equality against referenceRanges
// computed directly from the input) parameterized over extract_threads 1..4.
// Tiny buffers force many spills per worker; per-entry chunking scatters the
// stream across workers, so the flush merge must reunify pairs from every
// worker's runs — identical output for every N proves worker routing is
// correctness-free.
func TestBuilder_RoundTrip_ExtractThreads(t *testing.T) {
	entries := pipelineTestEntries(120)

	extractFn, err := logline.ExtractorForVersion("v3")
	require.NoError(t, err)

	for _, shardCount := range []int{1, 4} {
		shardFn := shard.Noop
		if shardCount > 1 {
			shardFn, err = shard.New("murmur3_mix")
			require.NoError(t, err)
		}
		want := referenceRanges(entries, extractFn, shardFn, shardCount)

		for workers := 1; workers <= MaxExtractThreads; workers++ {
			t.Run(fmt.Sprintf("shard%d_workers%d", shardCount, workers), func(t *testing.T) {
				cfg := roundTripConfig(t, shardCount, 32)
				cfg.ExtractThreads = workers
				b, err := newIndexBuilder(cfg, "2026-01-01", log.NewNopLogger(), NewMetrics(prometheus.NewRegistry()))
				require.NoError(t, err)
				defer b.clear()

				feedInChunks(t, b, entries, 1)

				files, err := b.prepareIndexes()
				require.NoError(t, err)
				require.Equal(t, sortedKeys(want), fileKeys(files), "(date,shard) file set mismatch")

				require.Equal(t, want, termRangesByFile(t, files),
					"extract_threads=%d output must equal the reference", workers)
			})
		}
	}
}

// TestBuilder_ExtractPipelineDrainBarrier flushes while items are still
// mid-pipeline and asserts the barrier's guarantees: no pairs lost (output
// equals a serial reference over the same input), all worker goroutines exited
// after prepare, work actually spread across workers, and retry-after-failure
// still works because the runs survive prepareIndexes.
func TestBuilder_ExtractPipelineDrainBarrier(t *testing.T) {
	entries := pipelineTestEntries(500)

	// Serial reference over identical input.
	ctl, err := newIndexBuilder(roundTripConfig(t, 4, 32), "2026-01-01", log.NewNopLogger(), NewMetrics(prometheus.NewRegistry()))
	require.NoError(t, err)
	defer ctl.clear()
	feedInChunks(t, ctl, entries, 5)
	ctlFiles, err := ctl.prepareIndexes()
	require.NoError(t, err)
	want := termRangesByFile(t, ctlFiles)

	baseline := runtime.NumGoroutine()

	cfg := roundTripConfig(t, 4, 32)
	cfg.ExtractThreads = 3
	b, err := newIndexBuilder(cfg, "2026-01-01", log.NewNopLogger(), NewMetrics(prometheus.NewRegistry()))
	require.NoError(t, err)
	defer b.clear()

	// Enqueue everything and flush immediately: with 100 queued items and a
	// 32-pair buffer the workers are guaranteed to still be mid-pipeline when
	// the drain barrier runs inside prepareIndexes.
	feedInChunks(t, b, entries, 5)
	files, err := b.prepareIndexes()
	require.NoError(t, err)
	require.Equal(t, want, termRangesByFile(t, files), "drain barrier must lose no pairs vs the serial reference")

	// Workers exited: the goroutine count returns to the pre-builder baseline.
	waitForGoroutineBaseline(t, baseline, "extract workers must exit after the drain barrier")

	// Work was actually distributed: run files from more than one worker
	// (run_w<i>_<seq>.frun) fed the merge.
	prefixes := map[string]bool{}
	var runPaths []string
	for _, w := range b.ingesters() {
		runPaths = append(runPaths, w.postings.runPaths...)
	}
	require.NotEmpty(t, runPaths)
	for _, p := range runPaths {
		name := filepath.Base(p)
		require.True(t, strings.HasPrefix(name, "run_w"), "pipeline runs must carry per-worker names, got %s", name)
		parts := strings.SplitN(name, "_", 3) // run_w<i>_<seq>.frun → ["run", "w<i>", "<seq>.frun"]
		require.Len(t, parts, 3)
		prefixes[parts[1]] = true
		_, err := os.Stat(p)
		require.NoError(t, err, "run %s must survive prepareIndexes", p)
	}
	require.GreaterOrEqual(t, len(prefixes), 2, "500 items must not all land on one worker")

	// Retry-after-failure: corrupt one surviving run so a retried prepare
	// fails, then heal it — the next retry must reproduce the reference from
	// the same runs (union rebuilt idempotently across attempts).
	victim := runPaths[0]
	fh, err := os.OpenFile(victim, os.O_RDWR, 0)
	require.NoError(t, err)
	_, err = fh.WriteAt([]byte("XRUN"), 0)
	require.NoError(t, err)
	require.NoError(t, fh.Close())

	_, err = b.prepareIndexes()
	require.Error(t, err)
	require.Contains(t, err.Error(), "bad run magic")
	for _, p := range runPaths {
		_, err := os.Stat(p)
		require.NoError(t, err, "run %s must survive a failed prepareIndexes", p)
	}

	fh, err = os.OpenFile(victim, os.O_RDWR, 0)
	require.NoError(t, err)
	_, err = fh.WriteAt([]byte(runMagic), 0)
	require.NoError(t, err)
	require.NoError(t, fh.Close())

	retried, err := b.prepareIndexes()
	require.NoError(t, err)
	require.Equal(t, want, termRangesByFile(t, retried), "healed retry must match the serial reference")
}

// TestBuilder_ExtractPipelineWorkerErrorPropagates verifies the failure
// contract: a worker spill error surfaces on the main path (processStream
// fails fast, exactly like a serial spill error failing running()), a
// subsequent prepareIndexes reports the same latched error, and no worker
// goroutine outlives clear().
func TestBuilder_ExtractPipelineWorkerErrorPropagates(t *testing.T) {
	baseline := runtime.NumGoroutine()

	cfg := roundTripConfig(t, 1, 32)
	cfg.ExtractThreads = 2
	b, err := newIndexBuilder(cfg, "2026-01-01", log.NewNopLogger(), NewMetrics(prometheus.NewRegistry()))
	require.NoError(t, err)

	// Block the lazily-created run directory with a regular file: the first
	// spill's MkdirAll fails, which is the same non-recoverable I/O class as a
	// full scratch volume.
	require.NoError(t, os.WriteFile(b.runDir, []byte("not a directory"), 0o644))

	entries := pipelineTestEntries(10)
	poison := recordRef{valid: true, partition: 7, offset: 99, tenantID: "t"}
	var streamErr error
	require.Eventually(t, func() bool {
		s := logproto.Stream{Entries: entries}
		if err := b.processStream(&s, nil, time.Now(), poison); err != nil {
			streamErr = err
			return true
		}
		return false
	}, 10*time.Second, 5*time.Millisecond, "worker spill failure must surface through processStream")
	require.ErrorContains(t, streamErr, "not a directory")
	require.ErrorContains(t, streamErr, "partition=7 offset=99")

	// The latched error also fails the flush path (drain barrier), so a cycle
	// racing the failure can never commit offsets for the lost pairs.
	_, err = b.prepareIndexes()
	require.Error(t, err)
	require.Contains(t, err.Error(), "extract pipeline")
	require.Contains(t, err.Error(), "partition=7 offset=99")

	b.clear()
	waitForGoroutineBaseline(t, baseline, "no worker goroutine may outlive clear()")
}

// TestService_ExtractThreadsFlushCommit drives the real service flush path
// (processRecordBatch → swapBuilder → executeFlush: prepare/upload/commit)
// with extract_threads=2, pinning the pipeline's integration with the
// at-least-once machinery. Run under -race this also exercises enqueue vs
// worker vs shouldFlush concurrency.
func TestService_ExtractThreadsFlushCommit(t *testing.T) {
	cluster, cfg := setupKafkaTest(t)
	defer cluster.Close()
	cfg.ExtractThreads = 2
	// Keep the trigger checks below deterministic no-ops (they still read the
	// run-disk/memory gauges concurrently with worker ingest — the -race
	// coverage this test exists for).
	cfg.FlushOnIdle = time.Hour

	bucket, indexStore := newTestStore(t)
	svc, err := New(indexStore, cfg, "2026-01-01", newDefaultFakePartitionRing(), log.NewNopLogger(), prometheus.NewRegistry())
	require.NoError(t, err)
	defer svc.client.Close()

	// The commit path filters to owned partitions. The kgo group client joins
	// in the background after New, but that assignment races this test's
	// direct flush; mark partition 0 owned deterministically (the assign
	// callback merges, so a late real assignment is harmless).
	svc.storeOwnedPartitions([]kafka.PartitionID{0})

	// Produce a record so the group coordinator has the topic registered for
	// the offset commit.
	producer, err := kgo.NewClient(kgo.SeedBrokers(cluster.ListenAddrs()[0]))
	require.NoError(t, err)
	defer producer.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var lastOffset kafka.Offset
	for batch := 0; batch < 5; batch++ {
		var records []rawRecord
		for i := 0; i < 10; i++ {
			data := mustMarshalStream(t, fmt.Sprintf("error connecting to database server timeout attempt %d", batch*10+i))
			res := producer.ProduceSync(ctx, &kgo.Record{Topic: testTopic, Value: data, Key: []byte("tenant")})
			require.NoError(t, res.FirstErr())
			lastOffset = kafka.Offset(res[0].Record.Offset)
			records = append(records, rawRecord{
				value:     data,
				timestamp: time.Now(),
				partition: 0,
				offset:    lastOffset,
				tenantID:  "tenant",
			})
		}
		require.NoError(t, svc.processRecordBatch(records))
		// Trigger check while workers are still extracting: shouldFlush sums
		// runDiskBytes/estimatedMemoryBytes across the worker buffers
		// concurrently with their spills (the poll loop does this every batch).
		svc.flushAndCommit(false)
	}

	svc.flushAndCommit(true)
	svc.builderMtx.Lock()
	done := svc.pendingFlush
	svc.builderMtx.Unlock()
	require.NotNil(t, done)
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("timed out waiting for pipeline flush to complete")
	}

	require.NotEmpty(t, bucket.Objects(), "pipeline flush must upload index files")

	adm := kadm.NewClient(svc.client)
	offsets, err := adm.FetchOffsets(ctx, cfg.Kafka.ConsumerGroup)
	require.NoError(t, err)
	committed, ok := offsets.Lookup(testTopic, 0)
	require.True(t, ok, "expected committed offset for partition 0")
	require.Equal(t, int64(lastOffset)+1, committed.At)
}
