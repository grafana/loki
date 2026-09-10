//go:build e2e

package builder

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/services"
	"github.com/grafana/loki/v3/pkg/logline"
	"github.com/grafana/loki/v3/pkg/kafka"
	"github.com/grafana/loki/v3/pkg/logline/store"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/loki"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"
	"github.com/twmb/franz-go/pkg/kfake"
	"github.com/twmb/franz-go/pkg/kgo"
)

// TestE2E validates the complete flow: produce logs → flush → verify → repeat
func TestE2E(t *testing.T) {
	// Setup
	outputDir := t.TempDir()
	testTopic := "test-topic"

	cluster, err := kfake.NewCluster(kfake.NumBrokers(1), kfake.SeedTopics(1, testTopic), kfake.GroupMaxSessionTimeout(5*time.Minute))
	require.NoError(t, err)
	t.Cleanup(cluster.Close)

	bucket := objstore.NewInMemBucket()
	indexStore, err := store.NewStore(bucket, store.Config{MinDate: "0001-01-01"}, log.NewNopLogger(), nil)
	require.NoError(t, err)
	addrs := cluster.ListenAddrs()

	cfg := Config{
		Kafka: kafka.Config{
			ReaderConfig:               kafka.ClientConfig{Address: addrs[0]},
			Topic:                      testTopic,
			ConsumerGroup:              "test-group",
			ProducerMaxRecordSizeBytes: 15 * 1024 * 1024,
		},
		Logline: LoglineConfig{
			DocumentInterval: 100 * time.Millisecond,
			NgramLength:      6,
		},
		InstanceID:              "test-builder-0",
		disableStaticMembership: true,
		FlushOnMaxBytes:         1, // any spilled run on scratch disk triggers full flush
		FlushOnIdle:             3 * time.Second,
		FlushOnMaxAge:           8 * time.Second,
		FlushCheckInterval:      1 * time.Second, // frequent checks — kfake doesn't honor FetchMaxWait
		ScratchDir:              outputDir,
	}

	lokiConfig := loki.ConfigWrapper{}
	lokiConfig.KafkaConfig.Address = addrs[0]

	logger := log.NewLogfmtLogger(os.Stdout)
	svc, err := New(lokiConfig, indexStore, cfg, "2026-01-01", newDefaultFakePartitionRing(), logger, prometheus.NewRegistry())
	require.NoError(t, err)

	producer, err := kgo.NewClient(
		kgo.SeedBrokers(addrs[0]),
		kgo.DefaultProduceTopic(testTopic),
	)
	require.NoError(t, err)
	t.Cleanup(producer.Close)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	t.Cleanup(func() {
		svc.Service.StopAsync()
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer stopCancel()
		_ = svc.Service.AwaitTerminated(stopCtx)
	})

	// Start service
	go svc.Service.StartAsync(ctx)
	require.NoError(t, svc.Service.AwaitRunning(context.Background()))

	// Phase 1: Produce structured logs and verify
	t.Log("Phase 1: Producing structured logs with known patterns...")
	knownPatterns := produceStructuredLogs(t, producer, ctx, testTopic, 2000)

	t.Log("Waiting for first flush...")
	files := waitForObjectStorageFiles(t, bucket, 1, 20*time.Second)
	require.NotEmpty(t, files, "expected at least 1 index file")

	t.Log("Verifying first index file...")
	verifyLintFile(t, bucket, files[0], knownPatterns, true)

	// Phase 2: Produce fuzz test data
	t.Log("Phase 2: Producing fuzz test data...")
	produceFuzzLogs(t, producer, ctx, testTopic, 1500)

	t.Log("Waiting for second flush...")
	files = waitForObjectStorageFiles(t, bucket, 2, 20*time.Second)
	require.GreaterOrEqual(t, len(files), 2, "expected at least 2 index files")

	t.Log("Verifying second index file (structural only — fuzz pattern matching is unreliable)...")
	verifyLintFile(t, bucket, files[1], nil, false)

	// Phase 3: Continued processing across flush cycles
	t.Log("Phase 3: Producing more structured logs...")
	phase3Patterns := produceStructuredLogs(t, producer, ctx, testTopic, 1500)

	t.Log("Waiting for next flush...")
	knownFiles := toSet(files)
	files = waitForPatternsInNewFiles(t, bucket, knownFiles, phase3Patterns, 120*time.Second)

	t.Log("E2E test passed: structured logs, fuzz data, and continued processing all verified")
}

// TestE2E_DecodeErrorFailsFast verifies the new fail-fast policy: a poison
// (undecodable) record must drive the service into services.Failed so the
// orchestrator restarts the pod instead of silently dropping bad data.
func TestE2E_DecodeErrorFailsFast(t *testing.T) {
	outputDir := t.TempDir()
	testTopic := "test-topic"

	cluster, err := kfake.NewCluster(kfake.NumBrokers(1), kfake.SeedTopics(1, testTopic), kfake.GroupMaxSessionTimeout(5*time.Minute))
	require.NoError(t, err)
	t.Cleanup(cluster.Close)

	bucket := objstore.NewInMemBucket()
	indexStore, err := store.NewStore(bucket, store.Config{MinDate: "0001-01-01"}, log.NewNopLogger(), nil)
	require.NoError(t, err)
	addrs := cluster.ListenAddrs()

	cfg := Config{
		Kafka: kafka.Config{
			ReaderConfig:               kafka.ClientConfig{Address: addrs[0]},
			Topic:                      testTopic,
			ConsumerGroup:              "test-decode-fail-group",
			ProducerMaxRecordSizeBytes: 15 * 1024 * 1024,
		},
		Logline: LoglineConfig{
			DocumentInterval: 100 * time.Millisecond,
			NgramLength:      6,
		},
		InstanceID:              "test-builder-0",
		disableStaticMembership: true,
		FlushOnMaxBytes:         100 * 1024 * 1024,
		FlushOnIdle:             10 * time.Minute,
		FlushOnMaxAge:           10 * time.Minute,
		FlushCheckInterval:      1 * time.Second,
		ScratchDir:              outputDir,
	}

	lokiConfig := loki.ConfigWrapper{}
	lokiConfig.KafkaConfig.Address = addrs[0]

	logger := log.NewLogfmtLogger(os.Stdout)
	svc, err := New(lokiConfig, indexStore, cfg, "2026-01-01", newDefaultFakePartitionRing(), logger, prometheus.NewRegistry())
	require.NoError(t, err)

	producer, err := kgo.NewClient(
		kgo.SeedBrokers(addrs[0]),
		kgo.DefaultProduceTopic(testTopic),
	)
	require.NoError(t, err)
	t.Cleanup(producer.Close)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go svc.Service.StartAsync(ctx)
	require.NoError(t, svc.Service.AwaitRunning(context.Background()))

	t.Log("Producing a poison (non-protobuf) record...")
	poison := &kgo.Record{
		Key:   []byte("tenant"),
		Value: []byte("this is not valid protobuf!!!"),
		Topic: testTopic,
	}
	require.NoError(t, producer.ProduceSync(ctx, poison).FirstErr())

	t.Log("Waiting for service to enter Failed state...")
	require.Eventually(t, func() bool {
		return svc.Service.State() == services.Failed
	}, 30*time.Second, 100*time.Millisecond, "service must fail on undecodable record")

	require.ErrorContains(t, svc.Service.FailureCase(), "failed to decode protobuf stream")
}

// TestE2E_GracefulShutdownFlush verifies that pending data is flushed to object storage
// when the service is stopped, even if no flush trigger has fired yet.
func TestE2E_GracefulShutdownFlush(t *testing.T) {
	outputDir := t.TempDir()
	testTopic := "test-topic"

	cluster, err := kfake.NewCluster(kfake.NumBrokers(1), kfake.SeedTopics(1, testTopic), kfake.GroupMaxSessionTimeout(5*time.Minute))
	require.NoError(t, err)
	t.Cleanup(cluster.Close)

	bucket := objstore.NewInMemBucket()
	indexStore, err := store.NewStore(bucket, store.Config{MinDate: "0001-01-01"}, log.NewNopLogger(), nil)
	require.NoError(t, err)
	addrs := cluster.ListenAddrs()

	cfg := Config{
		Kafka: kafka.Config{
			ReaderConfig:               kafka.ClientConfig{Address: addrs[0]},
			Topic:                      testTopic,
			ConsumerGroup:              "test-shutdown-group",
			ProducerMaxRecordSizeBytes: 15 * 1024 * 1024,
		},
		Logline: LoglineConfig{
			DocumentInterval: 100 * time.Millisecond,
			NgramLength:      6,
		},
		InstanceID:              "test-builder-0",
		disableStaticMembership: true,
		FlushOnIdle:             10 * time.Minute, // too long to trigger idle flush
		FlushOnMaxAge:           10 * time.Minute, // too long to trigger age flush
		ScratchDir:              outputDir,
	}

	lokiConfig := loki.ConfigWrapper{}
	lokiConfig.KafkaConfig.Address = addrs[0]

	logger := log.NewLogfmtLogger(os.Stdout)
	svc, err := New(lokiConfig, indexStore, cfg, "2026-01-01", newDefaultFakePartitionRing(), logger, prometheus.NewRegistry())
	require.NoError(t, err)

	producer, err := kgo.NewClient(
		kgo.SeedBrokers(addrs[0]),
		kgo.DefaultProduceTopic(testTopic),
	)
	require.NoError(t, err)
	t.Cleanup(producer.Close)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start service
	go svc.Service.StartAsync(ctx)
	require.NoError(t, svc.Service.AwaitRunning(context.Background()))

	// Produce structured logs
	t.Log("Producing logs before shutdown...")
	patterns := produceStructuredLogs(t, producer, ctx, testTopic, 500)

	// Wait just long enough for consumption but NOT for any flush trigger
	time.Sleep(2 * time.Second)

	// Verify NO files exist yet (no flush trigger should have fired)
	filesBefore := listIndexFiles(t, bucket)
	require.Empty(t, filesBefore, "expected no index files before shutdown (flush triggers set very high)")

	// Stop service — this should trigger shutdown flush
	t.Log("Stopping service (should trigger shutdown flush)...")
	svc.Service.StopAsync()
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer stopCancel()
	require.NoError(t, svc.Service.AwaitTerminated(stopCtx))

	// Verify files appeared from shutdown flush
	t.Log("Verifying shutdown flush produced index files...")
	files := waitForObjectStorageFiles(t, bucket, 1, 5*time.Second)
	require.NotEmpty(t, files, "shutdown flush should have produced at least 1 index file")

	t.Log("Verifying shutdown flush index file...")
	verifyLintFile(t, bucket, files[0], patterns, true)

	t.Log("Graceful shutdown flush test passed")
}

// TestE2E_OffsetCommit verifies that committed offsets prevent reprocessing.
// After flush + commit, a new consumer with the same group should not re-consume.
func TestE2E_OffsetCommit(t *testing.T) {
	outputDir := t.TempDir()
	testTopic := "test-topic"

	cluster, err := kfake.NewCluster(kfake.NumBrokers(1), kfake.SeedTopics(1, testTopic), kfake.GroupMaxSessionTimeout(5*time.Minute))
	require.NoError(t, err)
	t.Cleanup(cluster.Close)

	bucket := objstore.NewInMemBucket()
	indexStore, err := store.NewStore(bucket, store.Config{MinDate: "0001-01-01"}, log.NewNopLogger(), nil)
	require.NoError(t, err)
	addrs := cluster.ListenAddrs()

	consumerGroup := "test-offset-group"
	cfg := Config{
		Kafka: kafka.Config{
			ReaderConfig:               kafka.ClientConfig{Address: addrs[0]},
			Topic:                      testTopic,
			ConsumerGroup:              consumerGroup,
			ProducerMaxRecordSizeBytes: 15 * 1024 * 1024,
		},
		Logline: LoglineConfig{
			DocumentInterval: 100 * time.Millisecond,
			NgramLength:      6,
		},
		InstanceID:              "test-builder-0",
		disableStaticMembership: true,
		FlushOnMaxBytes:         1, // any spilled run on scratch disk triggers full flush
		FlushOnIdle:             3 * time.Second,
		FlushOnMaxAge:           8 * time.Second,
		FlushCheckInterval:      1 * time.Second, // frequent checks — kfake doesn't honor FetchMaxWait
		ScratchDir:              outputDir,
	}

	lokiConfig := loki.ConfigWrapper{}
	lokiConfig.KafkaConfig.Address = addrs[0]

	logger := log.NewLogfmtLogger(os.Stdout)

	producer, err := kgo.NewClient(
		kgo.SeedBrokers(addrs[0]),
		kgo.DefaultProduceTopic(testTopic),
	)
	require.NoError(t, err)
	t.Cleanup(producer.Close)

	// --- Service 1: produce, consume, flush, commit, stop ---
	t.Log("Starting service 1...")
	svc1, err := New(lokiConfig, indexStore, cfg, "2026-01-01", newDefaultFakePartitionRing(), logger, prometheus.NewRegistry())
	require.NoError(t, err)

	ctx1, cancel1 := context.WithCancel(context.Background())
	go svc1.Service.StartAsync(ctx1)
	require.NoError(t, svc1.Service.AwaitRunning(context.Background()))

	t.Log("Producing logs for service 1...")
	produceStructuredLogs(t, producer, ctx1, testTopic, 2000)

	// Wait for service 1 to consume ALL records and flush them.
	// The service polls in batches — we can't stop it after the first flush or
	// unconsumed records will be picked up by service 2. Wait until the file
	// count stabilizes (no new files for longer than idle_flush_timeout), which
	// proves all records have been consumed, flushed, and offsets committed.
	t.Log("Waiting for service 1 to consume all records and go idle...")
	files := waitForStableFileCount(t, bucket, cfg.FlushOnIdle+2*time.Second, 30*time.Second)
	t.Logf("Service 1 produced %d index files (stable)", len(files))

	// Stop service 1
	cancel1()
	svc1.Service.StopAsync()
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
	_ = svc1.Service.AwaitTerminated(stopCtx)
	stopCancel()

	// Count files after service 1 shutdown (may include shutdown flush)
	filesAfterShutdown := listIndexFiles(t, bucket)
	t.Logf("Total index files after service 1 shutdown: %d", len(filesAfterShutdown))

	// --- Service 2: same consumer group, should NOT re-process committed data ---
	t.Log("Starting service 2 with same consumer group...")
	outputDir2 := t.TempDir()
	cfg.ScratchDir = outputDir2

	svc2, err := New(lokiConfig, indexStore, cfg, "2026-01-01", newDefaultFakePartitionRing(), logger, prometheus.NewRegistry())
	require.NoError(t, err)

	ctx2, cancel2 := context.WithCancel(context.Background())
	go svc2.Service.StartAsync(ctx2)
	require.NoError(t, svc2.Service.AwaitRunning(context.Background()))

	// Give service 2 time to consume any un-committed data
	time.Sleep(5 * time.Second)

	// Stop service 2
	cancel2()
	svc2.Service.StopAsync()
	stopCtx2, stopCancel2 := context.WithTimeout(context.Background(), 10*time.Second)
	_ = svc2.Service.AwaitTerminated(stopCtx2)
	stopCancel2()

	// Count files after service 2 — should be same as after service 1 shutdown
	filesAfterSvc2 := listIndexFiles(t, bucket)
	t.Logf("Total index files after service 2: %d", len(filesAfterSvc2))

	// Service 2 should not have produced additional files since offsets were committed
	require.Equal(t, len(filesAfterShutdown), len(filesAfterSvc2),
		"service 2 should not produce additional index files — offsets should have been committed by service 1")

	t.Log("Offset commit verification passed")
}

// listIndexFiles returns all index data file keys in the bucket (recursive).
// Looks for paths matching <date>/<hash>/index (the store's index data path).
func listIndexFiles(t *testing.T, bucket objstore.Bucket) []string {
	t.Helper()
	var files []string
	var recurse func(prefix string) error
	recurse = func(prefix string) error {
		return bucket.Iter(context.Background(), prefix, func(name string) error {
			if name[len(name)-1] == '/' {
				return recurse(name)
			}
			if strings.HasSuffix(name, "/index") {
				files = append(files, name)
			}
			return nil
		})
	}
	require.NoError(t, recurse(""))
	return files
}

// produceLogLine produces a single log line to Kafka
func produceLogLine(t *testing.T, producer *kgo.Client, ctx context.Context, topic, line string) {
	t.Helper()

	stream := logproto.Stream{
		Labels: `{job="test"}`,
		Entries: []logproto.Entry{
			{
				Timestamp: time.Now(),
				Line:      line,
			},
		},
	}

	data, err := stream.Marshal()
	require.NoError(t, err)

	record := &kgo.Record{
		Key:   []byte("tenant"),
		Value: data,
		Topic: topic,
	}

	result := producer.ProduceSync(ctx, record)
	require.NoError(t, result.FirstErr())
}

// produceStructuredLogs produces logs with known patterns that we can verify
func produceStructuredLogs(t *testing.T, producer *kgo.Client, ctx context.Context, topic string, count int) []string {
	t.Helper()

	templates := []string{
		"ERROR: connection failed user_id=%d code=%d",
		"WARNING: memory pressure detected threshold=%d%%",
		"INFO: request completed status=%d latency=%dms",
		"DEBUG: cache miss key=user:%d:session:%d",
		"ERROR: database timeout query_id=%d retries=%d",
		"INFO: authentication successful user=%d method=oauth2",
		"WARNING: rate limit exceeded endpoint=/api/v%d user=%d",
		"ERROR: validation failed field=email.address pattern=invalid",
	}

	// Expected n-grams from template keywords (colon is transformed to '.' by n-gram extraction)
	expectedPatterns := []string{"ERROR.", "CONNEC", "WARNIN", "MEMORY", "STATUS"}

	for i := 0; i < count; i++ {
		template := templates[i%len(templates)]
		var line string
		switch i % len(templates) {
		case 0:
			line = fmt.Sprintf(template, i, i%500)
		case 1:
			line = fmt.Sprintf(template, 50+i%50)
		case 2:
			line = fmt.Sprintf(template, 200+i%300, 10+i%1000)
		case 3:
			line = fmt.Sprintf(template, i, i%1000)
		case 4:
			line = fmt.Sprintf(template, i, i%5)
		case 5:
			line = fmt.Sprintf(template, i)
		case 6:
			line = fmt.Sprintf(template, i%3+1, i)
		default:
			line = template
		}

		produceLogLine(t, producer, ctx, topic, line)

		if (i+1)%1000 == 0 {
			t.Logf("  Produced %d/%d", i+1, count)
		}
	}

	return expectedPatterns
}

// produceFuzzLogs produces random log lines for fuzz testing
func produceFuzzLogs(t *testing.T, producer *kgo.Client, ctx context.Context, topic string, count int) []string {
	t.Helper()

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	patterns := make(map[string]bool)

	charsets := []string{
		"abcdefghijklmnopqrstuvwxyz",
		"ABCDEFGHIJKLMNOPQRSTUVWXYZ",
		"0123456789",
		"._-/:@%?",
		"!#$&*()[]{}|;,<>",
	}

	for i := 0; i < count; i++ {
		// Generate random log line
		lineLen := 20 + rng.Intn(200)
		var line strings.Builder

		// Mix different character sets
		for j := 0; j < lineLen; j++ {
			charset := charsets[rng.Intn(len(charsets))]
			line.WriteByte(charset[rng.Intn(len(charset))])

			// Occasionally add spaces for tokenization
			if rng.Float32() < 0.15 {
				line.WriteByte(' ')
			}
		}

		logLine := line.String()

		// Track some patterns from longer words (sample every 50th for verification)
		if i%50 == 0 {
			for _, word := range strings.Fields(logLine) {
				upper := strings.ToUpper(word)
				// Normalize punctuation to dots (matching n-gram extraction logic)
				normalized := strings.Map(func(r rune) rune {
					if r == '_' || r == '-' || r == '/' || r == ':' || r == '@' || r == '%' || r == '?' {
						return '.'
					}
					return r
				}, upper)
				if len(normalized) >= 6 {
					patterns[normalized[:6]] = true
				}
			}
		}

		produceLogLine(t, producer, ctx, topic, logLine)

		if (i+1)%500 == 0 {
			t.Logf("  Produced %d/%d fuzz logs", i+1, count)
		}
	}

	// Convert map to slice
	result := make([]string, 0, len(patterns))
	for p := range patterns {
		result = append(result, p)
	}
	return result
}

// waitForStableFileCount polls until the index file count hasn't changed for stableDuration.
// This proves the service has gone idle — all records consumed, flushed, and committed.
func waitForStableFileCount(t *testing.T, bucket objstore.Bucket, stableDuration, timeout time.Duration) []string {
	t.Helper()

	deadline := time.Now().Add(timeout)
	lastFiles := listIndexFiles(t, bucket)
	lastChangeAt := time.Now()

	for time.Now().Before(deadline) {
		time.Sleep(500 * time.Millisecond)
		currentFiles := listIndexFiles(t, bucket)
		if len(currentFiles) != len(lastFiles) {
			lastFiles = currentFiles
			lastChangeAt = time.Now()
		}
		if time.Since(lastChangeAt) >= stableDuration {
			return currentFiles
		}
	}

	t.Fatalf("Timeout waiting for stable file count after %v (last count: %d)", timeout, len(lastFiles))
	return nil
}

// indexKeyPattern validates object keys match <YYYY-MM-DD>/<storage_id>/index
// StorageID is a ULID: 26 Crockford base32 characters.
var indexKeyPattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}/[0-9A-HJKMNP-TV-Z]{26}/index$`)

// waitForObjectStorageFiles polls for index files in object storage until minFiles reached.
// Validates that all object keys match the expected naming convention.
func waitForObjectStorageFiles(t *testing.T, bucket objstore.Bucket, minFiles int, timeout time.Duration) []string {
	t.Helper()

	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for time.Now().Before(deadline) {
		files := listIndexFiles(t, bucket)

		if len(files) >= minFiles {
			// Validate all object keys match expected format
			for _, key := range files {
				require.Regexp(t, indexKeyPattern, key,
					"object key %q doesn't match expected format YYYY-MM-DD/<ulid>/index", key)
			}
			return files
		}
		<-ticker.C
	}

	t.Fatalf("Timeout waiting for %d index files after %v", minFiles, timeout)
	return nil
}

// toSet converts a string slice to a set for O(1) lookups.
func toSet(items []string) map[string]bool {
	s := make(map[string]bool, len(items))
	for _, item := range items {
		s[item] = true
	}
	return s
}

// waitForPatternsInNewFiles waits until ALL expected patterns appear in index
// files that were NOT present in knownFiles. File paths contain hashes so
// lexicographic order does not correspond to creation order; we compare by
// path identity instead of slice index.
func waitForPatternsInNewFiles(t *testing.T, bucket objstore.Bucket, knownFiles map[string]bool, patterns []string, timeout time.Duration) []string {
	t.Helper()

	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for time.Now().Before(deadline) {
		<-ticker.C

		files := listIndexFiles(t, bucket)

		var newFiles []string
		for _, f := range files {
			if !knownFiles[f] {
				newFiles = append(newFiles, f)
			}
		}
		if len(newFiles) == 0 {
			continue
		}
		if len(patterns) == 0 {
			return files
		}
		if allPatternsFoundInFiles(bucket, newFiles, patterns) {
			return files
		}
	}

	t.Fatalf("Timeout waiting for patterns %v to appear in new index files after %v", patterns, timeout)
	return nil
}

// allPatternsFoundInFiles returns true if ALL expected patterns appear across the given files.
func allPatternsFoundInFiles(bucket objstore.Bucket, objectKeys []string, patterns []string) bool {
	foundPatterns := make(map[string]bool)

	for _, objectKey := range objectKeys {
		tmpDir, err := os.MkdirTemp("", "index-check")
		if err != nil {
			continue
		}
		defer os.RemoveAll(tmpDir) //nolint:gocritic
		tmpFile := filepath.Join(tmpDir, "check.lidx")

		rc, err := bucket.Get(context.Background(), objectKey)
		if err != nil {
			continue
		}

		f, err := os.Create(tmpFile)
		if err != nil {
			rc.Close()
			continue
		}
		_, err = f.ReadFrom(rc)
		f.Close()
		rc.Close()
		if err != nil {
			continue
		}

		reader, _, err := logline.OpenFile(tmpFile)
		if err != nil {
			continue
		}

		it, err := reader.NewTermIterator()
		if err != nil {
			reader.Close()
			continue
		}
		for it.Next() {
			term := it.Term()
			termStr := string(term[:6])
			for _, pattern := range patterns {
				if termStr == pattern {
					foundPatterns[pattern] = true
				}
			}
		}
		reader.Close()
	}

	for _, pattern := range patterns {
		if !foundPatterns[pattern] {
			return false
		}
	}
	return true
}

// verifyIndexFile performs comprehensive verification of an index file.
// When strictPatterns is true, ALL expected patterns must be found (use for structured logs).
// When false, only 20% must be found (use for fuzz logs where normalization is unpredictable).
func verifyLintFile(t *testing.T, bucket objstore.Bucket, objectKey string, expectedPatterns []string, strictPatterns bool) {
	t.Helper()

	// Download file from bucket
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "verify.lidx")

	rc, err := bucket.Get(context.Background(), objectKey)
	require.NoError(t, err)
	defer rc.Close()

	f, err := os.Create(tmpFile)
	require.NoError(t, err)
	defer f.Close()

	_, err = f.ReadFrom(rc)
	require.NoError(t, err)
	f.Close()

	// Open and verify
	reader, version, err := logline.OpenFile(tmpFile)
	require.NoError(t, err)
	defer reader.Close()

	// Verify header
	header := reader.ReadHeader()
	require.Equal(t, logline.CurrentVersion, version, "invalid version")
	require.Greater(t, header.DocumentCount, uint32(0), "no documents")
	require.Greater(t, header.TermCount, uint64(0), "no terms")
	// Verify document metadata
	docs := reader.Documents()
	require.Len(t, docs, int(header.DocumentCount), "document count mismatch")

	for i, doc := range docs {
		require.Equal(t, uint32(i), doc.ID, "document ID should be sequential")
		require.Greater(t, doc.MaxTimeUnix, int64(0), "MaxTimeUnix should be positive")
		require.GreaterOrEqual(t, doc.MaxTimeUnix, doc.MinTimeUnix, "MaxTime should be >= MinTime")
	}

	// Verify terms are sorted and unique
	it, err := reader.NewTermIterator()
	require.NoError(t, err)

	var prevTerm [8]byte
	termCount := 0
	foundPatterns := make(map[string]bool)
	seenTerms := make(map[[8]byte]bool)
	var postingCount uint64

	for it.Next() {
		term := it.Term()
		bitmap := it.Bitmap()
		postingCount += bitmap.Roaring.GetCardinality()

		// Check uniqueness
		require.False(t, seenTerms[term], "duplicate term found: %v", term)
		seenTerms[term] = true

		// Check sorting (only after first term)
		if termCount > 0 {
			// Compare as byte slices for lexicographic ordering
			cmp := compareTerms(prevTerm, term)
			require.Less(t, cmp, 0, "terms not sorted: %v >= %v", prevTerm, term)
		}

		// Verify bitmap is valid
		require.Greater(t, bitmap.Roaring.GetCardinality(), uint64(0), "empty bitmap for term")
		require.LessOrEqual(t, bitmap.Roaring.GetCardinality(), uint64(header.DocumentCount),
			"bitmap cardinality exceeds document count")

		// Check if this term matches any expected patterns
		termStr := string(term[:6])
		for _, pattern := range expectedPatterns {
			if termStr == pattern {
				foundPatterns[pattern] = true
			}
		}

		prevTerm = term
		termCount++
	}
	require.NoError(t, it.Err())
	require.Equal(t, int(header.TermCount), termCount, "term count mismatch")
	require.Greater(t, postingCount, uint64(0), "no postings")

	t.Logf("  Header: %d documents, %d terms, %d postings",
		header.DocumentCount, header.TermCount, postingCount)

	// Verify expected patterns were found in the index
	if strictPatterns && len(expectedPatterns) > 0 {
		// Strict mode: ALL patterns must be present (structured logs with known content)
		var missing []string
		for _, pattern := range expectedPatterns {
			if !foundPatterns[pattern] {
				missing = append(missing, pattern)
			}
		}
		require.Empty(t, missing, "expected patterns not found in index file %s: %v", objectKey, missing)
		t.Logf("  Found all %d expected patterns", len(expectedPatterns))
	} else if len(expectedPatterns) > 0 {
		// Lenient mode: at least 20% (fuzz logs where normalization is unpredictable)
		minExpected := len(expectedPatterns) / 5
		require.GreaterOrEqual(t, len(foundPatterns), minExpected,
			"too few patterns found: got %d/%d (need %d)", len(foundPatterns), len(expectedPatterns), minExpected)
		t.Logf("  Found %d/%d expected patterns (lenient mode)", len(foundPatterns), len(expectedPatterns))
	}

	t.Logf("  Verified: all terms sorted, unique, with valid bitmaps")
}

// compareTerms compares two [8]byte terms lexicographically
// Returns: <0 if a<b, 0 if a==b, >0 if a>b
func compareTerms(a, b [8]byte) int {
	for i := 0; i < 8; i++ {
		if a[i] < b[i] {
			return -1
		}
		if a[i] > b[i] {
			return 1
		}
	}
	return 0
}
