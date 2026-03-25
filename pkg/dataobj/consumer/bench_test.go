package consumer

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/grafana/loki/v3/pkg/dataobj/consumer/logsobj"
	"github.com/grafana/loki/v3/pkg/kafka"
	"github.com/grafana/loki/v3/pkg/logproto"
)

// testdataDir returns the absolute path to the repo-root testdata directory.
func testdataDir() string {
	// The testdata lives at the repo root, three levels up from
	// pkg/dataobj/consumer/.
	return filepath.Join("..", "..", "..", "testdata")
}

// capturedRecord holds a single Kafka record loaded from disk.
type capturedRecord struct {
	timestamp time.Time
	key       []byte // tenant ID
	value     []byte // protobuf-encoded logproto.Stream
}

// loadCapturedRecords loads all captured records from a binary file.
// Format: [count:u32] then per record [tsNanos:i64][len:u32][value:bytes].
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
			key:       []byte("benchmark-tenant"),
			value:     val,
		})
	}
	return records, nil
}

type decodedRecord struct {
	tenant    string
	stream    logproto.Stream
	timestamp time.Time
}

// decodeAllRecords pre-decodes all captured records, returning decoded records
// and summary statistics.
func decodeAllRecords(records []capturedRecord, decoder *kafka.Decoder) (decoded []decodedRecord, totalLines int, totalTextBytes int64, totalRawBytes int64) {
	for _, rec := range records {
		totalRawBytes += int64(len(rec.value))
		stream, err := decoder.DecodeWithoutLabels(rec.value)
		if err != nil {
			continue
		}
		for _, e := range stream.Entries {
			totalLines++
			totalTextBytes += int64(len(e.Line))
		}
		decoded = append(decoded, decodedRecord{
			tenant:    string(rec.key),
			stream:    stream,
			timestamp: rec.timestamp,
		})
	}
	return
}

// newBenchBuilder creates a logsobj.Builder configured for benchmarking with
// a very large target object size to avoid triggering flushes during append.
func newBenchBuilder(b *testing.B) *logsobj.Builder {
	b.Helper()
	cfg := logsobj.BuilderConfig{
		BuilderBaseConfig: logsobj.BuilderBaseConfig{
			TargetPageSize:          2 * 1024 * 1024,       // 2MB
			TargetObjectSize:        8 * 1024 * 1024 * 1024, // 8GB - don't trigger flush during bench
			TargetSectionSize:       128 * 1024 * 1024,      // 128MB
			BufferSize:              16 * 1024 * 1024,        // 16MB
			SectionStripeMergeLimit: 2,
		},
		DataobjSortOrder: "stream-asc",
	}
	builder, err := logsobj.NewBuilder(cfg, nil)
	if err != nil {
		b.Fatal(err)
	}
	return builder
}

// BenchmarkCapturedPipeline replays captured production Kafka records through the
// dataobj consumer pipeline. Measures the real-world hot path with production data.
//
// Uses the 2GB baseline file (testdata/kafka_records_p0.bin).
//
//	go test ./pkg/dataobj/consumer/ -bench BenchmarkCapturedPipeline -benchmem -benchtime 1x -timeout 60m
func BenchmarkCapturedPipeline(b *testing.B) {
	dataFile := filepath.Join(testdataDir(), "kafka_records_p0.bin")
	records, err := loadCapturedRecords(dataFile)
	if err != nil {
		b.Skipf("No captured data at %s (run capture first): %v", dataFile, err)
	}

	decoder, err := kafka.NewDecoder()
	if err != nil {
		b.Fatalf("create decoder: %v", err)
	}

	decoded, totalLines, totalTextBytes, totalRawBytes := decodeAllRecords(records, decoder)

	b.Logf("Loaded %d records (%.2f GB raw) -> %d decoded streams, %d log lines, %.2f GB text",
		len(records), float64(totalRawBytes)/(1024*1024*1024),
		len(decoded), totalLines, float64(totalTextBytes)/(1024*1024*1024))

	if len(decoded) == 0 {
		b.Fatal("no valid records to bench")
	}

	// Append only: pre-decoded streams through builder.Append()
	b.Run("append_only", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(totalTextBytes)
		for i := 0; i < b.N; i++ {
			builder := newBenchBuilder(b)
			for _, rec := range decoded {
				if err := builder.Append(rec.tenant, rec.stream); err != nil {
					b.Fatal(err)
				}
			}
		}
	})

	// Decode cost in isolation
	b.Run("decode_only", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(totalRawBytes)
		for i := 0; i < b.N; i++ {
			for _, rec := range records {
				_, _ = decoder.DecodeWithoutLabels(rec.value)
			}
		}
	})

	// Combined: decode + append (what the processor actually does)
	b.Run("decode_and_append", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(totalTextBytes)
		for i := 0; i < b.N; i++ {
			builder := newBenchBuilder(b)
			for _, rec := range records {
				stream, err := decoder.DecodeWithoutLabels(rec.value)
				if err != nil {
					continue
				}
				if err := builder.Append(string(rec.key), stream); err != nil {
					b.Fatal(err)
				}
			}
		}
	})

	// Full pipeline: decode + append + flush (produces a complete dataobj.Object)
	b.Run("decode_append_and_flush", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(totalTextBytes)
		for i := 0; i < b.N; i++ {
			builder := newBenchBuilder(b)
			for _, rec := range records {
				stream, err := decoder.DecodeWithoutLabels(rec.value)
				if err != nil {
					continue
				}
				if err := builder.Append(string(rec.key), stream); err != nil {
					b.Fatal(err)
				}
			}
			obj, closer, err := builder.Flush()
			if err != nil {
				b.Fatal(err)
			}
			b.ReportMetric(float64(obj.Size())/(1024*1024), "object_MB")
			closer.Close()
		}
	})

	// Append + reset: simulates one flush cycle. After reset, the builder
	// should release memory (no retained high-water-mark).
	b.Run("append_and_reset", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(totalTextBytes)
		for i := 0; i < b.N; i++ {
			builder := newBenchBuilder(b)
			for _, rec := range decoded {
				if err := builder.Append(rec.tenant, rec.stream); err != nil {
					b.Fatal(err)
				}
			}
			builder.Reset()
			runtime.GC()
		}
	})

	// Append in chunks to see how throughput changes as the builder grows
	b.Run("append_chunked_10pct", func(b *testing.B) {
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
			builder := newBenchBuilder(b)
			for _, rec := range decoded[:chunkSize] {
				if err := builder.Append(rec.tenant, rec.stream); err != nil {
					b.Fatal(err)
				}
			}
		}
	})
}

// BenchmarkCapturedPipelineLarge replays the 10GB captured data file to verify
// throughput at scale. This benchmark takes significantly longer to run.
//
//	go test ./pkg/dataobj/consumer/ -bench BenchmarkCapturedPipelineLarge -benchmem -benchtime 1x -timeout 120m
func BenchmarkCapturedPipelineLarge(b *testing.B) {
	dataFile := filepath.Join(testdataDir(), "kafka_records_10gb.bin")
	records, err := loadCapturedRecords(dataFile)
	if err != nil {
		b.Skipf("No captured data at %s: %v", dataFile, err)
	}

	decoder, err := kafka.NewDecoder()
	if err != nil {
		b.Fatalf("create decoder: %v", err)
	}

	decoded, totalLines, totalTextBytes, totalRawBytes := decodeAllRecords(records, decoder)

	b.Logf("Loaded %d records (%.2f GB raw) -> %d decoded streams, %d log lines, %.2f GB text",
		len(records), float64(totalRawBytes)/(1024*1024*1024),
		len(decoded), totalLines, float64(totalTextBytes)/(1024*1024*1024))

	if len(decoded) == 0 {
		b.Fatal("no valid records to bench")
	}

	// Combined: decode + append (real hot path at scale)
	b.Run("decode_and_append", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(totalTextBytes)
		for i := 0; i < b.N; i++ {
			builder := newBenchBuilder(b)
			for _, rec := range records {
				stream, err := decoder.DecodeWithoutLabels(rec.value)
				if err != nil {
					continue
				}
				if err := builder.Append(string(rec.key), stream); err != nil {
					b.Fatal(err)
				}
			}
		}
	})

	// Full pipeline with flush at scale
	b.Run("decode_append_and_flush", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(totalTextBytes)
		for i := 0; i < b.N; i++ {
			builder := newBenchBuilder(b)
			for _, rec := range records {
				stream, err := decoder.DecodeWithoutLabels(rec.value)
				if err != nil {
					continue
				}
				if err := builder.Append(string(rec.key), stream); err != nil {
					b.Fatal(err)
				}
			}
			obj, closer, err := builder.Flush()
			if err != nil {
				b.Fatal(err)
			}
			b.ReportMetric(float64(obj.Size())/(1024*1024), "object_MB")
			closer.Close()
		}
	})

	// Full pipeline with periodic flushes simulating production behavior.
	// Flushes the builder every time it exceeds the target object size (~1GB).
	b.Run("decode_append_periodic_flush", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(totalTextBytes)
		const targetFlushSize = 1024 * 1024 * 1024 // 1GB
		for i := 0; i < b.N; i++ {
			builder := newBenchBuilder(b)
			var flushCount int
			for _, rec := range records {
				stream, err := decoder.DecodeWithoutLabels(rec.value)
				if err != nil {
					continue
				}
				if err := builder.Append(string(rec.key), stream); err != nil {
					b.Fatal(err)
				}
				if builder.GetEstimatedSize() >= targetFlushSize {
					obj, closer, err := builder.Flush()
					if err != nil {
						b.Fatal(err)
					}
					closer.Close()
					_ = obj
					flushCount++
				}
			}
			// Final flush for remaining data.
			if builder.GetEstimatedSize() > 0 {
				_, closer, err := builder.Flush()
				if err != nil {
					b.Fatal(err)
				}
				closer.Close()
				flushCount++
			}
			b.ReportMetric(float64(flushCount), "flushes")
		}
	})
}

// BenchmarkProcessorThroughput benchmarks the full processor path including
// Kafka record decoding, builder append, and flush via the flushCommitter
// interface. This most closely matches production behavior.
//
//	go test ./pkg/dataobj/consumer/ -bench BenchmarkProcessorThroughput -benchmem -benchtime 1x -timeout 60m
func BenchmarkProcessorThroughput(b *testing.B) {
	dataFile := filepath.Join(testdataDir(), "kafka_records_p0.bin")
	records, err := loadCapturedRecords(dataFile)
	if err != nil {
		b.Skipf("No captured data at %s: %v", dataFile, err)
	}

	decoder, err := kafka.NewDecoder()
	if err != nil {
		b.Fatalf("create decoder: %v", err)
	}

	_, totalLines, totalTextBytes, totalRawBytes := decodeAllRecords(records, decoder)

	b.Logf("Loaded %d records (%.2f GB raw), %d lines, %.2f GB text",
		len(records), float64(totalRawBytes)/(1024*1024*1024),
		totalLines, float64(totalTextBytes)/(1024*1024*1024))

	// Simulate the processor's processRecord path: decode + append + flush
	// triggers via the builder full mechanism.
	b.Run("process_record_path", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(totalTextBytes)
		for i := 0; i < b.N; i++ {
			builder := newBenchBuilder(b)
			flusher := &mockFlushCommitter{}
			proc := &processor{
				builder:          builder,
				flushCommitter:   flusher,
				idleFlushTimeout: time.Hour,
				maxBuilderAge:    time.Hour,
				metrics:          newMetrics(prometheus.NewRegistry()),
			}
			proc.decoder, _ = kafka.NewDecoder()

			for j, rec := range records {
				r := newBenchRecord(rec, int64(j))
				if err := proc.processRecord(b.Context(), r); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportMetric(float64(flusher.flushes), "flushes")
		}
	})
}

// newBenchRecord creates a kgo.Record from a capturedRecord for feeding
// into the processor.
func newBenchRecord(rec capturedRecord, offset int64) *kgo.Record {
	return &kgo.Record{
		Key:       rec.key,
		Value:     rec.value,
		Timestamp: rec.timestamp,
		Offset:    offset,
	}
}
