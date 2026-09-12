package builder

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/grafana/loki/v3/pkg/kafka"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logline"
	"github.com/grafana/loki/v3/pkg/logproto"
)

func TestBuilder_Flush_EmptyBuilder(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := Config{
		Kafka: kafka.Config{
			ReaderConfig:               kafka.ClientConfig{Address: "localhost:9092"},
			Topic:                      "test-topic",
			ConsumerGroup:              "test-group",
			ProducerMaxRecordSizeBytes: 15 * 1024 * 1024,
		},
		Index: IndexConfig{
			DocumentInterval: 100 * time.Millisecond,
			NgramLength:      3},
		FlushOnIdle:   1 * time.Minute,
		FlushOnMaxAge: 5 * time.Minute,
		ScratchDir:    tmpDir,
	}

	require.NoError(t, cfg.Validate())

	logger := log.NewNopLogger()
	metrics := NewMetrics(prometheus.NewRegistry())
	builder, err := newIndexBuilder(cfg, "2026-01-01", logger, metrics)
	require.NoError(t, err)

	flushFiles, err := builder.prepareIndexes()
	require.NoError(t, err)
	require.Nil(t, flushFiles)

	// Should not create any files
	files, err := os.ReadDir(tmpDir)
	require.NoError(t, err)
	require.Len(t, files, 0)
}

func TestBuilder_ProcessStream_WithData(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := Config{
		Kafka: kafka.Config{
			ReaderConfig:               kafka.ClientConfig{Address: "localhost:9092"},
			Topic:                      "test-topic",
			ConsumerGroup:              "test-group",
			ProducerMaxRecordSizeBytes: 15 * 1024 * 1024,
		},
		Index: IndexConfig{
			DocumentInterval: 100 * time.Millisecond,
			NgramLength:      3},
		FlushOnIdle:   1 * time.Minute,
		FlushOnMaxAge: 5 * time.Minute,
		ScratchDir:    tmpDir,
	}

	require.NoError(t, cfg.Validate())

	logger := log.NewNopLogger()
	metrics := NewMetrics(prometheus.NewRegistry())
	builder, err := newIndexBuilder(cfg, "2026-01-01", logger, metrics)
	require.NoError(t, err)

	// Create a valid logproto.Stream
	stream := &logproto.Stream{
		Labels: `{job="test"}`,
		Entries: []logproto.Entry{
			{
				Timestamp: time.Now(),
				Line:      "error: connection failed at 2024-01-01",
			},
		},
	}

	// Process the stream
	_ = builder.processStream(stream, parseLabelsOrNil(stream.Labels), time.Now(), recordRef{})

	// One date observed → one (date, shard) index will be produced.
	require.Equal(t, 1, len(builder.ing.dateRanges))
}

func TestBuilder_V3ExtractsLabelValues(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := Config{
		Kafka: kafka.Config{
			ReaderConfig:               kafka.ClientConfig{Address: "localhost:9092"},
			Topic:                      "test-topic",
			ConsumerGroup:              "test-group",
			ProducerMaxRecordSizeBytes: 15 * 1024 * 1024,
		},
		Index: IndexConfig{
			DocumentInterval: 100 * time.Millisecond,
			Version:          "v3",
			NgramLength:      6,
		},
		FlushOnMaxBytes: 4096,
		FlushOnIdle:     1 * time.Minute,
		FlushOnMaxAge:   5 * time.Minute,
		ScratchDir:      tmpDir,
	}
	require.NoError(t, cfg.Validate())

	logger := log.NewNopLogger()
	metrics := NewMetrics(prometheus.NewRegistry())
	builder, err := newIndexBuilder(cfg, "2026-01-01", logger, metrics)
	require.NoError(t, err)

	stream := &logproto.Stream{
		Labels: `{app="labelvalue"}`,
		Entries: []logproto.Entry{
			{Timestamp: time.Now().UTC(), Line: "abc"},
		},
	}
	_ = builder.processStream(stream, parseLabelsOrNil(stream.Labels), time.Now().UTC(), recordRef{})

	files, err := builder.prepareIndexes()
	require.NoError(t, err)
	require.NotEmpty(t, files)

	reader, _, err := logline.OpenFile(files[0].file.Name())
	require.NoError(t, err)
	defer reader.Close()

	idx, err := reader.FindTerm("LABELV")
	require.NoError(t, err)
	require.GreaterOrEqual(t, idx, 0)

	bm, err := reader.GetBitmap(idx)
	require.NoError(t, err)
	require.False(t, bm.IsEmpty())
}

func TestBuilder_FlushIntegration(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := Config{
		Kafka: kafka.Config{
			ReaderConfig:               kafka.ClientConfig{Address: "localhost:9092"},
			Topic:                      "test-topic",
			ConsumerGroup:              "test-group",
			ProducerMaxRecordSizeBytes: 15 * 1024 * 1024,
		},
		Index: IndexConfig{
			// 200ms: divides 24h, and its docID window (epoch + 2^32 ticks ≈
			// 27y) ends comfortably past Validate's one-year future runway.
			DocumentInterval: 200 * time.Millisecond,
			NgramLength:      3,
		},
		FlushOnIdle:   1 * time.Minute,
		FlushOnMaxAge: 5 * time.Minute,
		ScratchDir:    tmpDir,
	}

	require.NoError(t, cfg.Validate())

	logger := log.NewNopLogger()
	metrics := NewMetrics(prometheus.NewRegistry())
	builder, err := newIndexBuilder(cfg, "2026-01-01", logger, metrics)
	require.NoError(t, err)

	// Process multiple log lines
	logLines := []string{
		"error: connection failed at 2024-01-01",
		"warning: retry attempt 1",
		"info: connection established",
		"debug: processing request",
	}

	for _, line := range logLines {
		stream := &logproto.Stream{
			Labels: `{job="test"}`,
			Entries: []logproto.Entry{
				{
					Timestamp: time.Now(),
					Line:      line,
				},
			},
		}

		_ = builder.processStream(stream, parseLabelsOrNil(stream.Labels), time.Now(), recordRef{})
	}

	flushFiles, err := builder.prepareIndexes()
	require.NoError(t, err)
	require.Len(t, flushFiles, 1)

	// Verify the produced .lidx is readable and has content.
	reader, _, err := logline.OpenFile(flushFiles[0].file.Name())
	require.NoError(t, err)
	defer reader.Close()

	require.Greater(t, reader.ReadHeader().TermCount, uint64(0))
	require.Greater(t, reader.ReadHeader().DocumentCount, uint32(0))

	builder.clear()
}

func TestBuilder_ProcessStream_MultipleEntries(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := Config{
		Kafka: kafka.Config{
			ReaderConfig:               kafka.ClientConfig{Address: "localhost:9092"},
			Topic:                      "test-topic",
			ConsumerGroup:              "test-group",
			ProducerMaxRecordSizeBytes: 15 * 1024 * 1024,
		},
		Index: IndexConfig{
			DocumentInterval: 100 * time.Millisecond,
			NgramLength:      3},
		FlushOnIdle:   1 * time.Minute,
		FlushOnMaxAge: 5 * time.Minute,
		ScratchDir:    tmpDir,
	}

	require.NoError(t, cfg.Validate())

	logger := log.NewNopLogger()
	metrics := NewMetrics(prometheus.NewRegistry())
	builder, err := newIndexBuilder(cfg, "2026-01-01", logger, metrics)
	require.NoError(t, err)

	// Create a stream with multiple entries
	stream := &logproto.Stream{
		Labels: `{job="test"}`,
		Entries: []logproto.Entry{
			{
				Timestamp: time.Now(),
				Line:      "error: connection failed",
			},
			{
				Timestamp: time.Now(),
				Line:      "warning: retry attempt 1",
			},
			{
				Timestamp: time.Now(),
				Line:      "info: connection established",
			},
		},
	}

	// Process the stream (single message with 3 log entries)
	_ = builder.processStream(stream, parseLabelsOrNil(stream.Labels), time.Now(), recordRef{})

	// All 3 entries share today's date → one index carrying their documents.
	require.Equal(t, 1, len(builder.ing.dateRanges))

	files, err := builder.prepareIndexes()
	require.NoError(t, err)
	require.Len(t, files, 1)

	reader, _, err := logline.OpenFile(files[0].file.Name())
	require.NoError(t, err)
	defer reader.Close()
	require.Greater(t, len(reader.Documents()), 0, "index should have documents from all entries")
}

func TestProcessStream_FutureEntriesGetOwnBucket(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := Config{
		Kafka: kafka.Config{
			ReaderConfig:               kafka.ClientConfig{Address: "localhost:9092"},
			Topic:                      "test-topic",
			ConsumerGroup:              "test-group",
			ProducerMaxRecordSizeBytes: 15 * 1024 * 1024,
		},
		Index: IndexConfig{
			DocumentInterval: 100 * time.Millisecond,
			NgramLength:      3},
		FlushOnIdle:   1 * time.Minute,
		FlushOnMaxAge: 5 * time.Minute,
		ScratchDir:    tmpDir,
	}

	require.NoError(t, cfg.Validate())

	logger := log.NewNopLogger()
	metrics := NewMetrics(prometheus.NewRegistry())
	builder, err := newIndexBuilder(cfg, "2026-01-01", logger, metrics)
	require.NoError(t, err)

	now := time.Now().UTC()
	tomorrow := now.Add(24 * time.Hour)
	expectedDate := builder.ing.formatDate(tomorrow)
	todayDate := builder.ing.formatDate(now)

	stream := &logproto.Stream{
		Labels: `{app="test"}`,
		Entries: []logproto.Entry{
			{Timestamp: tomorrow, Line: "future log line with enough text for ngrams"},
		},
	}

	_ = builder.processStream(stream, parseLabelsOrNil(stream.Labels), now, recordRef{})

	// The future entry must be tracked under its own date, not today's (no clamping).
	_, hasFuture := builder.ing.dateRanges[expectedDate]
	require.True(t, hasFuture, "expected date %s to be observed", expectedDate)

	if expectedDate != todayDate {
		_, hasToday := builder.ing.dateRanges[todayDate]
		require.False(t, hasToday, "future entry should not be clamped to today %s", todayDate)
	}
}

func TestFormatDate(t *testing.T) {
	ing := &streamIngester{}

	tests := []struct {
		name     string
		input    time.Time
		expected string
	}{
		{"StandardDate", time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC), "2024-01-15"},
		{"SameDayCache", time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC), "2024-01-15"}, // Should hit cache
		{"NextDay", time.Date(2024, 1, 16, 9, 2, 3, 4, time.UTC), "2024-01-16"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ing.formatDate(tt.input)
			require.Equal(t, tt.expected, result)
		})
	}
}

// BenchmarkProcessStream benchmarks the performance-critical ProcessStream function.
func BenchmarkProcessStream(b *testing.B) {
	tmpDir := b.TempDir()

	cfg := Config{
		Kafka: kafka.Config{
			ReaderConfig:               kafka.ClientConfig{Address: "localhost:9092"},
			Topic:                      "bench-topic",
			ConsumerGroup:              "bench-group",
			ProducerMaxRecordSizeBytes: 15 * 1024 * 1024,
		},
		Index: IndexConfig{
			DocumentInterval: DefaultDocumentInterval,
			NgramLength:      DefaultNgramLength,
		},
		FlushOnIdle:   1 * time.Hour,
		FlushOnMaxAge: 24 * time.Hour,
		ScratchDir:    tmpDir,
	}

	require.NoError(b, cfg.Validate())

	logger := log.NewNopLogger()
	metrics := NewMetrics(prometheus.NewRegistry())

	// Benchmark scenarios with different log line characteristics
	benchmarks := []struct {
		name        string
		entryCount  int
		logLineSize int
	}{
		{"SingleEntry_SmallLog", 1, 50},
		{"SingleEntry_MediumLog", 1, 200},
		{"SingleEntry_LargeLog", 1, 1000},
		{"MultipleEntries_10x100", 10, 100},
		{"MultipleEntries_50x100", 50, 100},
		{"MultipleEntries_100x200", 100, 200},
	}
	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			// Create a new builder for each sub-benchmark to avoid state pollution
			builder, err := newIndexBuilder(cfg, "2026-01-01", logger, metrics)
			require.NoError(b, err)

			// Generate realistic log lines
			entries := make([]logproto.Entry, bm.entryCount)
			for i := 0; i < bm.entryCount; i++ {
				// Create varied log content to simulate realistic scenarios
				logLine := generateLogLine(i, bm.logLineSize)
				entries[i] = logproto.Entry{
					Timestamp: time.Now(),
					Line:      logLine,
				}
			}

			// Create stream
			stream := &logproto.Stream{
				Labels:  `{job="benchmark"}`,
				Entries: entries,
			}

			// Reset timer to exclude setup cost
			b.ResetTimer()

			// Run benchmark
			for i := 0; i < b.N; i++ {
				_ = builder.processStream(stream, parseLabelsOrNil(stream.Labels), time.Now(), recordRef{})

				// Flush periodically to prevent memory exhaustion in long benchmarks
				if i%1000 == 0 && i > 0 {
					b.StopTimer()
					if files, err := builder.prepareIndexes(); err == nil && files != nil {
						builder.clear()
					}
					b.StartTimer()
				}
			}

			// Report approximate bytes processed
			totalBytes := 0
			for _, entry := range entries {
				totalBytes += len(entry.Line)
			}
			b.SetBytes(int64(totalBytes))
		})
	}
}

// BenchmarkProcessStream_NoFlush benchmarks ProcessStream in isolation without any flushes.
func BenchmarkProcessStream_NoFlush(b *testing.B) {
	tmpDir := b.TempDir()

	cfg := Config{
		Kafka: kafka.Config{
			ReaderConfig:               kafka.ClientConfig{Address: "localhost:9092"},
			Topic:                      "bench-topic",
			ConsumerGroup:              "bench-group",
			ProducerMaxRecordSizeBytes: 15 * 1024 * 1024,
		},
		Index: IndexConfig{
			DocumentInterval: DefaultDocumentInterval,
			NgramLength:      DefaultNgramLength,
		},
		FlushOnIdle:   24 * time.Hour,
		FlushOnMaxAge: 24 * time.Hour,
		ScratchDir:    tmpDir,
	}

	require.NoError(b, cfg.Validate())

	logger := log.NewNopLogger()
	metrics := NewMetrics(prometheus.NewRegistry())
	builder, err := newIndexBuilder(cfg, "2026-01-01", logger, metrics)
	require.NoError(b, err)

	// Typical production log line (~200 bytes with 5 entries)
	entries := make([]logproto.Entry, 5)
	for i := range 5 {
		entries[i] = logproto.Entry{
			Timestamp: time.Now(),
			Line:      generateLogLine(i, 200),
		}
	}

	stream := &logproto.Stream{
		Labels:  `{job="benchmark"}`,
		Entries: entries,
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = builder.processStream(stream, parseLabelsOrNil(stream.Labels), time.Now(), recordRef{})
	}

	// Calculate approximate bytes
	totalBytes := 0
	for _, entry := range entries {
		totalBytes += len(entry.Line)
	}
	b.SetBytes(int64(totalBytes))
}

// BenchmarkProcessStream_ParallelConsumers simulates multiple consumers processing in parallel.
func BenchmarkProcessStream_ParallelConsumers(b *testing.B) {
	tmpDir := b.TempDir()

	// Create test data once
	entries := make([]logproto.Entry, 10)
	for i := range 10 {
		entries[i] = logproto.Entry{
			Timestamp: time.Now(),
			Line:      generateLogLine(i, 150),
		}
	}

	stream := &logproto.Stream{
		Labels:  `{job="benchmark"}`,
		Entries: entries,
	}

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		// Each goroutine gets its own builder (simulating different consumer instances)
		cfg := Config{
			Kafka: kafka.Config{
				ReaderConfig:               kafka.ClientConfig{Address: "localhost:9092"},
				Topic:                      "bench-topic",
				ConsumerGroup:              "bench-group",
				ProducerMaxRecordSizeBytes: 15 * 1024 * 1024,
			},
			Index: IndexConfig{
				DocumentInterval: DefaultDocumentInterval,
				NgramLength:      DefaultNgramLength,
			},
			FlushOnIdle:   1 * time.Hour,
			FlushOnMaxAge: 24 * time.Hour,
			ScratchDir:    tmpDir,
		}

		require.NoError(b, cfg.Validate())

		logger := log.NewNopLogger()
		metrics := NewMetrics(prometheus.NewRegistry())
		builder, err := newIndexBuilder(cfg, "2026-01-01", logger, metrics)
		require.NoError(b, err)

		i := 0

		for pb.Next() {
			_ = builder.processStream(stream, parseLabelsOrNil(stream.Labels), time.Now(), recordRef{})
			i++

			// Periodic flush to prevent OOM
			if i%500 == 0 {
				if files, err := builder.prepareIndexes(); err == nil && files != nil {
					_ = files
					builder.clear()
				}
			}
		}
	})

	// Calculate approximate bytes
	totalBytes := 0
	for _, entry := range entries {
		totalBytes += len(entry.Line)
	}
	b.SetBytes(int64(totalBytes))
}

// generateLogLine creates a realistic log line of approximately the specified size.
func generateLogLine(index int, targetSize int) string {
	// Common log patterns
	templates := []string{
		"level=error msg=\"connection failed\" host=%s port=%d attempt=%d err=\"%s\"",
		"level=info msg=\"request processed\" method=%s path=%s status=%d duration=%dms",
		"level=warn msg=\"high memory usage\" component=%s mem_used=%dMB threshold=%dMB",
		"level=debug msg=\"query executed\" database=%s table=%s rows=%d time=%dms query=\"%s\"",
	}

	template := templates[index%len(templates)]

	// Generate content to reach target size
	var line string
	switch index % 4 {
	case 0:
		line = fmt.Sprintf(template, "prod-db-01.example.com", 5432+index, index, "connection timeout after 30s")
	case 1:
		line = fmt.Sprintf(template, "GET", "/api/v1/users/"+fmt.Sprint(index), 200, 45+index)
	case 2:
		line = fmt.Sprintf(template, "query-engine", 2048+index, 4096)
	case 3:
		line = fmt.Sprintf(template, "analytics", "events", 1000+index, 123, "SELECT * FROM events WHERE timestamp > NOW() - INTERVAL '1 hour'")
	}

	// Pad to target size with realistic content
	if len(line) < targetSize {
		padding := " data=" + repeatString("x", targetSize-len(line)-6)
		line += padding
	}

	return line[:min(len(line), targetSize)]
}

// repeatString repeats a string to reach the target length.
func repeatString(s string, count int) string {
	if count <= 0 {
		return ""
	}
	result := make([]byte, count)
	for i := range count {
		result[i] = s[i%len(s)]
	}
	return string(result)
}

// TestBuilder_TracksDateRanges verifies the builder records a per-date time
// range for every distinct date it observes (the spans stamped on each date's
// .lidx metadata at flush).
func TestBuilder_TracksDateRanges(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := Config{
		Kafka: kafka.Config{
			ReaderConfig:               kafka.ClientConfig{Address: "localhost:9092"},
			Topic:                      "test-topic",
			ConsumerGroup:              "test-group",
			ProducerMaxRecordSizeBytes: 15 * 1024 * 1024,
		},
		Index: IndexConfig{
			DocumentInterval: 100 * time.Millisecond,
			NgramLength:      3},
		FlushOnIdle:   1 * time.Minute,
		FlushOnMaxAge: 5 * time.Minute,
		ScratchDir:    tmpDir,
	}

	require.NoError(t, cfg.Validate())

	logger := log.NewNopLogger()
	metrics := NewMetrics(prometheus.NewRegistry())
	builder, err := newIndexBuilder(cfg, "2026-01-01", logger, metrics)
	require.NoError(t, err)

	// Nothing observed yet.
	require.NotNil(t, builder.ing.dateRanges)
	require.Equal(t, 0, len(builder.ing.dateRanges))

	// Process streams from multiple days
	now := time.Now()
	for i := range 3 {
		stream := &logproto.Stream{
			Labels: `{job="test"}`,
			Entries: []logproto.Entry{
				{
					Timestamp: now.Add(-time.Duration(i) * 24 * time.Hour),
					Line:      "ERROR: test log line",
				},
			},
		}
		_ = builder.processStream(stream, parseLabelsOrNil(stream.Labels), now.Add(-time.Duration(i)*24*time.Hour), recordRef{})
	}

	// Should have observed 3 distinct dates.
	require.Equal(t, 3, len(builder.ing.dateRanges))
}

// TestBuilder_QueueTimestampInObjectKey verifies that the object key
// for uploaded .lidx files uses the queue enqueued timestamp (Kafka record timestamp)
// and not the log entry timestamp. This ensures object keys reflect data freshness
// from when it was produced to Kafka.
func TestBuilder_QueueTimestampInObjectKey(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := Config{
		Kafka: kafka.Config{
			ReaderConfig:               kafka.ClientConfig{Address: "localhost:9092"},
			Topic:                      "test-topic",
			ConsumerGroup:              "test-group",
			ProducerMaxRecordSizeBytes: 15 * 1024 * 1024,
		},
		Index: IndexConfig{
			DocumentInterval: 100 * time.Millisecond,
			NgramLength:      3},
		FlushOnIdle:   1 * time.Minute,
		FlushOnMaxAge: 5 * time.Minute,
		ScratchDir:    tmpDir,
	}

	require.NoError(t, cfg.Validate())

	logger := log.NewNopLogger()
	metrics := NewMetrics(prometheus.NewRegistry())
	builder, err := newIndexBuilder(cfg, "2026-01-01", logger, metrics)
	require.NoError(t, err)

	// Scenario: Processing old logs (from 5 days ago) but they were just enqueued now
	now := time.Now()
	queueTime := now         // Data was enqueued to Kafka now
	oldLogTime := time.Date( // But logs are from 5 days ago.
		now.UTC().Year(),
		now.UTC().Month(),
		now.UTC().Day(),
		12, 0, 0, 0,
		time.UTC,
	).Add(-5 * 24 * time.Hour)

	stream := &logproto.Stream{
		Labels: `{job="test"}`,
		Entries: []logproto.Entry{
			{
				Timestamp: oldLogTime, // Old log timestamp
				Line:      "ERROR: replayed log from 5 days ago",
			},
			{
				Timestamp: oldLogTime.Add(1 * time.Hour), // Another old log
				Line:      "WARN: another replayed log",
			},
		},
	}

	// Process stream with recent queue timestamp
	_ = builder.processStream(stream, parseLabelsOrNil(stream.Labels), queueTime, recordRef{})

	files, err := builder.prepareIndexes()
	require.NoError(t, err)
	require.NotNil(t, files)
	require.Len(t, files, 1)

	fileInfo := files[0]

	// Verify the minEnqueuedTime is the queue timestamp (recent), not log timestamp (old)
	require.Equal(t, queueTime.Truncate(time.Second), fileInfo.minEnqueuedTime.Truncate(time.Second),
		"fileInfo should use queue enqueued timestamp")
	require.NotEqual(t, oldLogTime.Truncate(time.Second), fileInfo.minEnqueuedTime.Truncate(time.Second),
		"fileInfo should NOT use log entry timestamp")

	// This means the object key (which uses minEnqueuedTime as epoch) will reflect
	// when the data was produced to Kafka, not when the logs were originally created.
	// This is important for monitoring data freshness and lag.
}

func TestBuilder_OutOfWindowTimestamps_Panic(t *testing.T) {
	// Entries outside the fixed-epoch docID window (before docIDEpoch,
	// 2026-01-01, or at/past epoch + 2^32 ticks) must PANIC at ingest — a
	// documented never-panic override (CLAUDE.md invariant #7). Dropping them
	// while the zero-file flush path still commits Kafka offsets would
	// permanently, silently skip replayed Adaptive Logs archive data.
	cfg := Config{
		Kafka: kafka.Config{
			ReaderConfig:               kafka.ClientConfig{Address: "localhost:9092"},
			Topic:                      "test-topic",
			ConsumerGroup:              "test-group",
			ProducerMaxRecordSizeBytes: 15 * 1024 * 1024,
		},
		Index: IndexConfig{
			DocumentInterval: 100 * time.Millisecond,
			NgramLength:      3},
		FlushOnIdle:   1 * time.Minute,
		FlushOnMaxAge: 5 * time.Minute,
		ScratchDir:    t.TempDir(),
	}
	require.NoError(t, cfg.Validate())

	metrics := NewMetrics(prometheus.NewRegistry())
	// Empty minDate disables the minDate filter so entries reach the window
	// check (a pre-epoch minDate is rejected by newIndexBuilder, and with any
	// valid minDate pre-epoch entries are dropped before the check). This
	// exercises the pre-epoch panic branch, which in production defends only
	// against minDate-bypass bugs — see CLAUDE.md invariant #7.
	builder, err := newIndexBuilder(cfg, "", log.NewNopLogger(), metrics)
	require.NoError(t, err)
	defer builder.clear()

	process := func(ts time.Time, ref recordRef) {
		stream := &logproto.Stream{
			Labels:  `{job="test"}`,
			Entries: []logproto.Entry{{Timestamp: ts, Line: "boundary probe line"}},
		}
		_ = builder.processStream(stream, parseLabelsOrNil(stream.Labels), time.Now(), ref)
	}

	windowEnd := docIDWindowEnd(cfg.Index.DocumentInterval)
	require.Panics(t, func() { process(docIDEpoch.Add(-time.Second), recordRef{}) },
		"pre-epoch timestamp must panic")
	require.Panics(t, func() { process(windowEnd, recordRef{}) },
		"timestamp at the exclusive window end must panic")

	// The panic must attribute the poison Kafka record: the operator
	// remediating the crash loop advances the committed offset past it, so
	// partition/offset/tenant must appear in the message when the caller
	// supplies them.
	ref := recordRef{valid: true, partition: 3, offset: 42, tenantID: "tenant-a"}
	func() {
		defer func() {
			r := recover()
			require.NotNil(t, r, "out-of-window timestamp with a valid recordRef must panic")
			msg, ok := r.(string)
			require.True(t, ok, "panic value must be the attribution string, got %T", r)
			require.Contains(t, msg, "partition=3")
			require.Contains(t, msg, "offset=42")
			require.Contains(t, msg, `tenant="tenant-a"`)
		}()
		process(docIDEpoch.Add(-time.Second), ref)
	}()

	// The window boundaries themselves are representable: the epoch is tick 0
	// and the last interval before the end is tick 2^32-1.
	require.NotPanics(t, func() { process(docIDEpoch, recordRef{}) })
	require.NotPanics(t, func() { process(windowEnd.Add(-cfg.Index.DocumentInterval), recordRef{}) })
}

func TestBuilder_MinDate_DropsOldEntries(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := Config{
		Kafka: kafka.Config{
			ReaderConfig:               kafka.ClientConfig{Address: "localhost:9092"},
			Topic:                      "test-topic",
			ConsumerGroup:              "test-group",
			ProducerMaxRecordSizeBytes: 15 * 1024 * 1024,
		},
		Index: IndexConfig{
			DocumentInterval: 100 * time.Millisecond,
			NgramLength:      3},
		FlushOnIdle:   1 * time.Minute,
		FlushOnMaxAge: 5 * time.Minute,
		ScratchDir:    tmpDir,
	}
	require.NoError(t, cfg.Validate())

	reg := prometheus.NewRegistry()
	metrics := NewMetrics(reg)
	builder, err := newIndexBuilder(cfg, "2026-03-15", log.NewNopLogger(), metrics)
	require.NoError(t, err)

	now := time.Now()
	stream := &logproto.Stream{
		Labels: `{job="test"}`,
		Entries: []logproto.Entry{
			{Timestamp: time.Date(2026, 3, 14, 12, 0, 0, 0, time.UTC), Line: "before min_date"},    // dropped
			{Timestamp: time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC), Line: "exactly on min_date"}, // kept
			{Timestamp: time.Date(2026, 3, 16, 6, 0, 0, 0, time.UTC), Line: "after min_date"},      // kept
		},
	}

	_ = builder.processStream(stream, parseLabelsOrNil(stream.Labels), now, recordRef{})

	// Only dates on or after min_date should be observed.
	require.Len(t, builder.ing.dateRanges, 2, "expected 2026-03-15 and 2026-03-16 only")
	_, has14 := builder.ing.dateRanges["2026-03-14"]
	require.False(t, has14, "2026-03-14 should be dropped")

	// Verify the dropped metric incremented.
	require.Equal(t, float64(1), testutil.ToFloat64(metrics.droppedLinesPreMinDate))

	// Verify flushed files only contain the two valid dates.
	files, err := builder.prepareIndexes()
	require.NoError(t, err)
	require.Len(t, files, 2)

	dates := map[string]bool{}
	for _, f := range files {
		dates[f.date] = true
	}
	require.True(t, dates["2026-03-15"])
	require.True(t, dates["2026-03-16"])
}

func TestNewIndexBuilder_RejectsPreEpochMinDate(t *testing.T) {
	// A min_date before docIDEpoch would let pre-epoch entries past the minDate
	// drop and into a guaranteed out-of-window panic; the config is rejected at
	// construction instead (CLAUDE.md invariant #7).
	cfg := Config{
		Kafka: kafka.Config{
			ReaderConfig:               kafka.ClientConfig{Address: "localhost:9092"},
			Topic:                      "test-topic",
			ConsumerGroup:              "test-group",
			ProducerMaxRecordSizeBytes: 15 * 1024 * 1024,
		},
		Index: IndexConfig{
			DocumentInterval: 100 * time.Millisecond,
			NgramLength:      3},
		FlushOnIdle:   1 * time.Minute,
		FlushOnMaxAge: 5 * time.Minute,
		ScratchDir:    t.TempDir(),
	}
	require.NoError(t, cfg.Validate())
	logger := log.NewNopLogger()
	metrics := NewMetrics(prometheus.NewRegistry())

	_, err := newIndexBuilder(cfg, "2025-12-31", logger, metrics)
	require.ErrorContains(t, err, "predates the docID epoch 2026-01-01")

	_, err = newIndexBuilder(cfg, "0001-01-01", logger, metrics)
	require.ErrorContains(t, err, "predates the docID epoch 2026-01-01")

	// The epoch itself, later dates, and the empty string (filter disabled) are
	// all accepted.
	for _, minDate := range []string{"2026-01-01", "2026-07-04", ""} {
		b, err := newIndexBuilder(cfg, minDate, logger, metrics)
		require.NoError(t, err, "min_date %q must be accepted", minDate)
		b.clear()
	}
}
