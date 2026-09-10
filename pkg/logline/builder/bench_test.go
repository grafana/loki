package builder

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"strconv"
	"strings"
	"testing"
	"time"

	"go.uber.org/atomic"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/sasl/plain"

	"github.com/grafana/loki/v3/pkg/kafka"
	"github.com/grafana/loki/v3/pkg/logproto"
)

const capturedDataFile = "testdata/kafka_records.bin"

// TestCaptureKafkaRecords connects to a Kafka broker and saves raw Kafka
// records from partition 0 to disk for benchmarking.
//
// By default captures 2GB. Override with CAPTURE_MAX_MB.
//
// Run with:
//
//	CAPTURE_KAFKA=1 KAFKA_BROKER=localhost:9092 KAFKA_TOPIC=loki \
//	  go test ./pkg/logline/builder/ -run TestCaptureKafkaRecords -v -timeout 30m
//
// The broker must hold records written by Loki's Kafka ingest path, since the
// captured values are decoded with kafka.Decoder.
//
// Environment:
//
//	KAFKA_BROKER      seed broker address, defaults to localhost:9092
//	KAFKA_TOPIC       topic to consume, defaults to loki
//	KAFKA_PARTITION   partition to consume, defaults to 0
//	KAFKA_SASL_USER   SASL/PLAIN user, SASL is disabled when empty
//	KAFKA_SASL_PASS   SASL/PLAIN password
//	KAFKA_DIAL_SEED   set to 1 to route every connection to KAFKA_BROKER
func TestCaptureKafkaRecords(t *testing.T) {
	if os.Getenv("CAPTURE_KAFKA") == "" {
		t.Skip("Set CAPTURE_KAFKA=1 to capture records from a Kafka broker")
	}

	broker := envOr("KAFKA_BROKER", "localhost:9092")
	topic := envOr("KAFKA_TOPIC", "loki")
	saslUser := envOr("KAFKA_SASL_USER", "")
	saslPass := envOr("KAFKA_SASL_PASS", "")

	maxBytes := int64(2 * 1024 * 1024 * 1024) // 2GB default
	if v := os.Getenv("CAPTURE_MAX_MB"); v != "" {
		mb, _ := strconv.ParseInt(v, 10, 64)
		if mb > 0 {
			maxBytes = mb * 1024 * 1024
		}
	}
	maxWait := 25 * time.Minute

	partition := int32(0)
	if v := os.Getenv("KAFKA_PARTITION"); v != "" {
		p, _ := strconv.ParseInt(v, 10, 32)
		partition = int32(p)
	}

	opts := []kgo.Opt{
		kgo.SeedBrokers(broker),
		kgo.ConsumePartitions(map[string]map[int32]kgo.Offset{
			topic: {partition: kgo.NewOffset().AtEnd().Relative(-500000)},
		}),
		kgo.FetchMaxWait(5 * time.Second),
		kgo.FetchMaxBytes(50 * 1024 * 1024), // 50MB per fetch for throughput
	}

	if saslUser != "" {
		opts = append(opts, kgo.SASL(plain.Plain(func(_ context.Context) (plain.Auth, error) {
			return plain.Auth{User: saslUser, Pass: saslPass}, nil
		})))
	}

	// Some deployments advertise broker addresses that are unreachable from the
	// machine running the benchmark, for example when reaching the cluster over
	// a port-forward or an SSH tunnel. KAFKA_DIAL_SEED ignores the advertised
	// addresses and dials the seed broker instead. Only usable when that broker
	// leads the partition being captured.
	if os.Getenv("KAFKA_DIAL_SEED") != "" {
		opts = append(opts, kgo.Dialer(func(_ context.Context, _, _ string) (net.Conn, error) {
			return net.DialTimeout("tcp", broker, 10*time.Second)
		}))
	}

	client, err := kgo.NewClient(opts...)
	if err != nil {
		t.Fatalf("failed to create kafka client: %v", err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), maxWait)
	defer cancel()

	// Stream directly to disk to avoid holding 2GB in memory.
	// Format: [count:u32] then per record [tsNanos:i64][len:u32][value:bytes]
	// We write a placeholder count, then overwrite it at the end.
	outPath := filepath.Join("testdata", fmt.Sprintf("kafka_records_p%d.bin", partition))
	f, err := os.Create(outPath)
	if err != nil {
		t.Fatalf("create output: %v", err)
	}
	defer f.Close()

	// Placeholder for record count
	if err := binary.Write(f, binary.LittleEndian, uint32(0)); err != nil {
		t.Fatalf("write placeholder: %v", err)
	}

	var totalBytes int64
	var recordCount uint32
	start := time.Now()
	lastLog := start

	t.Logf("Consuming from %s partition %d (topic=%s, target=%.0f MB)...",
		broker, partition, topic, float64(maxBytes)/(1024*1024))

	for totalBytes < maxBytes {
		fetches := client.PollFetches(ctx)
		if ctx.Err() != nil {
			t.Logf("Context done after %v (captured %.2f GB)", time.Since(start), float64(totalBytes)/(1024*1024*1024))
			break
		}
		if err := fetches.Err(); err != nil {
			t.Logf("poll error: %v", err)
			continue
		}

		fetches.EachRecord(func(r *kgo.Record) {
			if len(r.Value) == 0 {
				return
			}
			if err := binary.Write(f, binary.LittleEndian, r.Timestamp.UnixNano()); err != nil {
				t.Fatalf("write ts: %v", err)
			}
			if err := binary.Write(f, binary.LittleEndian, uint32(len(r.Value))); err != nil {
				t.Fatalf("write len: %v", err)
			}
			if _, err := f.Write(r.Value); err != nil {
				t.Fatalf("write value: %v", err)
			}
			totalBytes += int64(len(r.Value))
			recordCount++
		})

		if time.Since(lastLog) > 5*time.Second {
			elapsed := time.Since(start)
			mbps := float64(totalBytes) / (1024 * 1024) / elapsed.Seconds()
			t.Logf("  %d records, %.2f / %.2f GB (%.1f MB/s)",
				recordCount, float64(totalBytes)/(1024*1024*1024), float64(maxBytes)/(1024*1024*1024), mbps)
			lastLog = time.Now()
		}
	}

	if recordCount == 0 {
		t.Fatal("captured zero records")
	}

	// Write final record count at the start of the file
	if _, err := f.Seek(0, 0); err != nil {
		t.Fatalf("seek: %v", err)
	}
	if err := binary.Write(f, binary.LittleEndian, recordCount); err != nil {
		t.Fatalf("write count: %v", err)
	}

	elapsed := time.Since(start)
	t.Logf("Wrote %d records (%.2f GB) to %s in %v (%.1f MB/s)",
		recordCount, float64(totalBytes)/(1024*1024*1024), outPath, elapsed,
		float64(totalBytes)/(1024*1024)/elapsed.Seconds())
}

type capturedRecord struct {
	timestamp time.Time
	value     []byte
}

func loadCapturedRecords(path string) ([]capturedRecord, error) {
	return loadCapturedRecordsN(path, 0)
}

// loadCapturedRecordsN loads up to maxRecords captured records from disk.
// If maxRecords is 0 or exceeds the file's count, all records are loaded.
func loadCapturedRecordsN(path string, maxRecords int) ([]capturedRecord, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var count uint32
	if err := binary.Read(f, binary.LittleEndian, &count); err != nil {
		return nil, fmt.Errorf("read count: %w", err)
	}

	n := int(count)
	if maxRecords > 0 && maxRecords < n {
		n = maxRecords
	}

	records := make([]capturedRecord, 0, n)
	for i := 0; i < n; i++ {
		var tsNanos int64
		if err := binary.Read(f, binary.LittleEndian, &tsNanos); err != nil {
			return nil, fmt.Errorf("read ts [%d]: %w", i, err)
		}
		var vLen uint32
		if err := binary.Read(f, binary.LittleEndian, &vLen); err != nil {
			return nil, fmt.Errorf("read len [%d]: %w", i, err)
		}
		val := make([]byte, vLen)
		if _, err := io.ReadFull(f, val); err != nil {
			return nil, fmt.Errorf("read value [%d]: %w", i, err)
		}
		records = append(records, capturedRecord{
			timestamp: time.Unix(0, tsNanos),
			value:     val,
		})
	}
	return records, nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// BenchmarkCapturedPipeline replays captured production Kafka records through the
// builder pipeline. Measures the real-world hot path with production data.
//
//	go test ./pkg/logline/builder/ -bench BenchmarkCapturedPipeline -benchmem -benchtime 1x -timeout 30m
func BenchmarkCapturedPipeline(b *testing.B) {
	records, err := loadCapturedRecords(capturedDataFile)
	if err != nil {
		b.Skipf("No captured data at %s (run TestCaptureKafkaRecords first): %v", capturedDataFile, err)
	}

	decoder, err := kafka.NewDecoder()
	if err != nil {
		b.Fatalf("create decoder: %v", err)
	}

	type decodedRecord struct {
		stream    logproto.Stream
		labels    labels.Labels
		timestamp time.Time
	}

	var decoded []decodedRecord
	var totalLines int
	var totalBytes int64
	var totalRawBytes int64
	for _, rec := range records {
		totalRawBytes += int64(len(rec.value))
		stream, parsedLabels, err := decoder.Decode(rec.value)
		if err != nil {
			continue
		}
		for _, e := range stream.Entries {
			totalLines++
			totalBytes += int64(len(e.Line))
		}
		decoded = append(decoded, decodedRecord{stream: stream, labels: parsedLabels, timestamp: rec.timestamp})
	}

	b.Logf("Loaded %d records (%.2f GB raw) → %d decoded streams, %d log lines, %.2f GB text",
		len(records), float64(totalRawBytes)/(1024*1024*1024),
		len(decoded), totalLines, float64(totalBytes)/(1024*1024*1024))

	if len(decoded) == 0 {
		b.Fatal("no valid records to bench")
	}

	newBuilder := func(b *testing.B) *indexBuilder {
		cfg := Config{
			Logline: LoglineConfig{
				IndexVersion:     "v3",
				DocumentInterval: 100 * time.Millisecond,
				NgramLength:      6,
			},
			FlushOnIdle:            1 * time.Hour,
			FlushOnMaxAge:          1 * time.Hour,
			PostingsBufferPairs:    DefaultPostingsBufferPairs,
			PostingsSpillWatermark: DefaultPostingsSpillWatermark,
			ScratchDir:             b.TempDir(),
		}
		builder, err := newIndexBuilder(cfg, "2026-01-01", log.NewNopLogger(), NewMetrics(prometheus.NewRegistry()))
		if err != nil {
			b.Fatal(err)
		}
		return builder
	}

	// Full pipeline: pre-decoded streams through processStream
	b.Run("process_only", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(totalBytes)
		for i := 0; i < b.N; i++ {
			builder := newBuilder(b)
			for _, rec := range decoded {
				_ = builder.processStream(&rec.stream, &rec.labels, rec.timestamp, recordRef{})
			}
		}
	})

	// Decode cost in isolation
	b.Run("decode_only", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(totalRawBytes)
		for i := 0; i < b.N; i++ {
			for _, rec := range records {
				_, _, _ = decoder.Decode(rec.value)
			}
		}
	})

	// Combined: decode + process (what the service actually does)
	b.Run("decode_and_process", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(totalBytes)
		for i := 0; i < b.N; i++ {
			builder := newBuilder(b)
			for _, rec := range records {
				stream, parsedLabels, err := decoder.Decode(rec.value)
				if err != nil {
					continue
				}
				_ = builder.processStream(&stream, &parsedLabels, rec.timestamp, recordRef{})
			}
		}
	})

	// Process + clear: measures ingest plus memory release. clear() discards
	// the buffered pairs without finish()/merge, so this does NOT exercise the
	// flush path — TestBuilderE2E measures prepareIndexes end to end.
	b.Run("process_and_clear", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(totalBytes)
		for i := 0; i < b.N; i++ {
			builder := newBuilder(b)
			for _, rec := range decoded {
				_ = builder.processStream(&rec.stream, &rec.labels, rec.timestamp, recordRef{})
			}
			builder.clear()
		}
	})

	// Small-slice ingest: cold-start throughput on the first 10% of the stream.
	b.Run("process_chunked_10pct", func(b *testing.B) {
		b.ReportAllocs()
		chunkSize := len(decoded) / 10
		if chunkSize == 0 {
			chunkSize = 1
		}
		var chunkBytes int64
		for _, rec := range decoded[:chunkSize] {
			for _, e := range rec.stream.Entries {
				chunkBytes += int64(len(e.Line))
			}
		}
		b.SetBytes(chunkBytes)
		for i := 0; i < b.N; i++ {
			builder := newBuilder(b)
			for _, rec := range decoded[:chunkSize] {
				_ = builder.processStream(&rec.stream, &rec.labels, rec.timestamp, recordRef{})
			}
		}
	})
}

// BenchmarkPartitionComparison benchmarks the builder pipeline against captured data
// from each partition to identify per-partition throughput differences.
//
// Capture data first:
//
//	for p in 0 1 2; do
//	  CAPTURE_KAFKA=1 KAFKA_PARTITION=$p CAPTURE_MAX_MB=200 \
//	    go test ./pkg/logline/builder/ -run TestCaptureKafkaRecords -v -timeout 10m
//	done
//
// Then run:
//
//	go test ./pkg/logline/builder/ -bench BenchmarkPartitionComparison -benchmem -benchtime 1x -timeout 30m
func BenchmarkPartitionComparison(b *testing.B) {
	decoder, err := kafka.NewDecoder()
	if err != nil {
		b.Fatalf("create decoder: %v", err)
	}

	for _, partition := range []int{0, 1, 2} {
		path := filepath.Join("testdata", fmt.Sprintf("kafka_records_p%d.bin", partition))
		records, err := loadCapturedRecords(path)
		if err != nil {
			b.Logf("Skipping partition %d: %v", partition, err)
			continue
		}

		type decodedRecord struct {
			stream    logproto.Stream
			labels    labels.Labels
			timestamp time.Time
		}

		var decoded []decodedRecord
		var totalLines int
		var totalBytes int64
		var totalRawBytes int64
		var decodeErrors int
		for _, rec := range records {
			totalRawBytes += int64(len(rec.value))
			stream, parsedLabels, err := decoder.Decode(rec.value)
			if err != nil {
				decodeErrors++
				continue
			}
			for _, e := range stream.Entries {
				totalLines++
				totalBytes += int64(len(e.Line))
			}
			decoded = append(decoded, decodedRecord{stream: stream, labels: parsedLabels, timestamp: rec.timestamp})
		}

		b.Logf("Partition %d: %d records (%.1f MB raw), %d streams, %d lines, %.1f MB text, %d decode errors, %.0f bytes/line avg",
			partition, len(records), float64(totalRawBytes)/(1024*1024),
			len(decoded), totalLines, float64(totalBytes)/(1024*1024),
			decodeErrors, float64(totalBytes)/float64(max(totalLines, 1)))

		if len(decoded) == 0 {
			continue
		}

		newBld := func(b *testing.B) *indexBuilder {
			cfg := Config{
				Logline: LoglineConfig{
					IndexVersion:     "v3",
					DocumentInterval: 250 * time.Millisecond,
					NgramLength:      6,
				},
				FlushOnIdle:            1 * time.Hour,
				FlushOnMaxAge:          1 * time.Hour,
				PostingsBufferPairs:    DefaultPostingsBufferPairs,
				PostingsSpillWatermark: DefaultPostingsSpillWatermark,
				ScratchDir:             b.TempDir(),
			}
			bld, err := newIndexBuilder(cfg, "2026-01-01", log.NewNopLogger(), NewMetrics(prometheus.NewRegistry()))
			if err != nil {
				b.Fatal(err)
			}
			return bld
		}

		b.Run(fmt.Sprintf("p%d/decode_and_process", partition), func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(totalRawBytes)
			for i := 0; i < b.N; i++ {
				bld := newBld(b)
				for _, rec := range records {
					stream, parsedLabels, err := decoder.Decode(rec.value)
					if err != nil {
						continue
					}
					_ = bld.processStream(&stream, &parsedLabels, rec.timestamp, recordRef{})
				}
			}
		})

		b.Run(fmt.Sprintf("p%d/process_only", partition), func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(totalBytes)
			for i := 0; i < b.N; i++ {
				bld := newBld(b)
				for _, rec := range decoded {
					_ = bld.processStream(&rec.stream, &rec.labels, rec.timestamp, recordRef{})
				}
			}
		})
	}
}

// e2ePeakSampler tracks max HeapInuse on a fast ticker (RSS-ish proxy).
type e2ePeakSampler struct {
	peak atomic.Uint64
	stop chan struct{}
	done chan struct{}
}

func startE2EPeakSampler() *e2ePeakSampler {
	s := &e2ePeakSampler{stop: make(chan struct{}), done: make(chan struct{})}
	go func() {
		defer close(s.done)
		tk := time.NewTicker(20 * time.Millisecond)
		defer tk.Stop()
		var ms runtime.MemStats
		for {
			select {
			case <-s.stop:
				return
			case <-tk.C:
				runtime.ReadMemStats(&ms)
				for {
					cur := s.peak.Load()
					if ms.HeapInuse <= cur || s.peak.CompareAndSwap(cur, ms.HeapInuse) {
						break
					}
				}
			}
		}
	}()
	return s
}

func (s *e2ePeakSampler) stopPeak() uint64 {
	close(s.stop)
	<-s.done
	return s.peak.Load()
}

// TestBuilderE2E is the end-to-end builder benchmark: it streams the captured
// records through the real builder (decode → processStream) exactly as the
// service does, then runs prepareIndexes (the full flush-to-.lidx path), and
// reports throughput, total allocations, and peak heap — at each requested
// shard count. It only uses the builder's public surface, so implementations
// with different internals are measured identically.
//
// Gated: BENCH_E2E=1. Env knobs:
//
//	BENCH_DATA            data file (default testdata/kafka_records_p0.bin)
//	BENCH_SHARDS          comma list of shard counts (default "1,10")
//	BENCH_EXTRACT_THREADS extract_threads for the run (default 1 = serial path).
//	                      Note >1 pipelines ingest: the drain barrier runs in
//	                      prepareIndexes, so extraction time still in flight at
//	                      the end of the feed loop is attributed to prepare —
//	                      compare runs on total/throughput, not ingest alone.
//	BENCH_PROFILE_DIR     if set, write <data>_s<shard>.{cpu,heap}.pprof per run
//
//	BENCH_E2E=1 BENCH_DATA=testdata/kafka_records_p0.bin go test ./pkg/logline/builder/ -run TestBuilderE2E -count=1 -v -timeout 60m
func TestBuilderE2E(t *testing.T) {
	if os.Getenv("BENCH_E2E") == "" {
		t.Skip("Set BENCH_E2E=1 to run the end-to-end builder benchmark")
	}
	dataFile := envOr("BENCH_DATA", "testdata/kafka_records_p0.bin")
	shardCounts := parseShards(envOr("BENCH_SHARDS", "1,10"))

	records, err := loadCapturedRecords(dataFile)
	if err != nil {
		t.Fatalf("load %s: %v", dataFile, err)
	}
	decoder, err := kafka.NewDecoder()
	if err != nil {
		t.Fatal(err)
	}

	// Total indexed text bytes (throughput denominator), computed once.
	var totalText int64
	for _, rec := range records {
		stream, derr := decoder.DecodeWithoutLabels(rec.value)
		if derr != nil {
			continue
		}
		for i := range stream.Entries {
			totalText += int64(len(stream.Entries[i].Line))
		}
	}
	t.Logf("data=%s records=%d text=%.2f GiB", dataFile, len(records), float64(totalText)/(1<<30))

	for _, shardCount := range shardCounts {
		runBuilderE2E(t, records, decoder, shardCount, totalText)
	}
}

func runBuilderE2E(t *testing.T, records []capturedRecord, decoder *kafka.Decoder, shardCount int, totalText int64) {
	extractThreads := 1
	if v := os.Getenv("BENCH_EXTRACT_THREADS"); v != "" {
		n, err := strconv.Atoi(v)
		require.NoError(t, err, "BENCH_EXTRACT_THREADS must be an integer")
		extractThreads = n
	}
	cfg := Config{
		Logline: LoglineConfig{
			IndexVersion:     "v3",
			DocumentInterval: 100 * time.Millisecond,
			ShardCount:       shardCount,
			NgramLength:      6,
		},
		FlushOnIdle:            1 * time.Hour,
		FlushOnMaxAge:          1 * time.Hour,
		PostingsBufferPairs:    DefaultPostingsBufferPairs,
		PostingsSpillWatermark: DefaultPostingsSpillWatermark,
		ExtractThreads:         extractThreads,
		ScratchDir:             t.TempDir(),
	}
	if shardCount > 1 {
		cfg.Logline.ShardAlgorithm = "murmur3_mix"
	}
	builder, err := newIndexBuilder(cfg, "2026-01-01", log.NewNopLogger(), NewMetrics(prometheus.NewRegistry()))
	if err != nil {
		t.Fatal(err)
	}

	// Optional profiles for later inspection (BENCH_PROFILE_DIR=/path).
	profDir := os.Getenv("BENCH_PROFILE_DIR")
	var cpuFile *os.File
	if profDir != "" {
		require.NoError(t, os.MkdirAll(profDir, 0o755))
		cpuFile, err = os.Create(filepath.Join(profDir, fmt.Sprintf("%s_s%d.cpu.pprof", profTag(), shardCount)))
		require.NoError(t, err)
		require.NoError(t, pprof.StartCPUProfile(cpuFile))
	}

	runtime.GC()
	var m0 runtime.MemStats
	runtime.ReadMemStats(&m0)
	sampler := startE2EPeakSampler()

	istart := time.Now()
	for _, rec := range records {
		stream, parsedLabels, derr := decoder.Decode(rec.value)
		if derr != nil {
			continue
		}
		if err := builder.processStream(&stream, &parsedLabels, rec.timestamp, recordRef{}); err != nil {
			t.Fatalf("processStream: %v", err)
		}
	}
	ingestSec := time.Since(istart).Seconds()

	pstart := time.Now()
	files, err := builder.prepareIndexes()
	if err != nil {
		t.Fatalf("prepareIndexes: %v", err)
	}
	prepareSec := time.Since(pstart).Seconds()

	peak := sampler.stopPeak()
	var m1 runtime.MemStats
	runtime.ReadMemStats(&m1)

	if cpuFile != nil {
		pprof.StopCPUProfile()
		cpuFile.Close()
		hf, err := os.Create(filepath.Join(profDir, fmt.Sprintf("%s_s%d.heap.pprof", profTag(), shardCount)))
		require.NoError(t, err)
		runtime.GC()
		require.NoError(t, pprof.WriteHeapProfile(hf))
		hf.Close()
	}

	totalSec := ingestSec + prepareSec
	t.Logf("SHARD=%d WORKERS=%d files=%d | ingest=%.1fs prepare=%.1fs total=%.1fs | throughput=%.1f MB/s | alloc=%.2f GiB | peak_heap=%.0f MiB",
		shardCount, extractThreads, len(files), ingestSec, prepareSec, totalSec,
		float64(totalText)/(1<<20)/totalSec,
		float64(m1.TotalAlloc-m0.TotalAlloc)/(1<<30),
		float64(peak)/(1<<20))

	builder.clear()
}

// TestBuilderE2EFloor reports the resident heap floor held by the harness
// itself — the raw captured records ([]capturedRecord), which stay live for the
// whole run and are therefore counted in every peak_heap sample. Subtract this
// from peak_heap to get the builder's true working set. Gated: BENCH_E2E=1.
func TestBuilderE2EFloor(t *testing.T) {
	if os.Getenv("BENCH_E2E") == "" {
		t.Skip("Set BENCH_E2E=1 to run the heap-floor measurement")
	}
	dataFile := envOr("BENCH_DATA", "testdata/kafka_records_p0.bin")
	records, err := loadCapturedRecords(dataFile)
	if err != nil {
		t.Fatalf("load %s: %v", dataFile, err)
	}
	var raw int64
	for i := range records {
		raw += int64(len(records[i].value))
	}
	runtime.GC()
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	t.Logf("FLOOR data=%s records=%d raw_bytes=%.2f GiB heap_inuse=%.0f MiB",
		dataFile, len(records), float64(raw)/(1<<30), float64(ms.HeapInuse)/(1<<20))
	runtime.KeepAlive(records)
}

// profTag derives a short profile-file prefix from the data file name.
func profTag() string {
	b := filepath.Base(envOr("BENCH_DATA", "testdata/kafka_records_p0.bin"))
	return strings.TrimSuffix(b, filepath.Ext(b))
}

func parseShards(s string) []int {
	var out []int
	for p := range strings.SplitSeq(s, ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		n, err := strconv.Atoi(p)
		if err == nil {
			out = append(out, n)
		}
	}
	return out
}
