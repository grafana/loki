package bench

import (
	"fmt"
	"iter"
	"math/rand"
	"os"
	"time"

	"github.com/grafana/loki/v3/pkg/logproto"
)

type Batch struct {
	Streams []logproto.Stream
}

// Size of batch in bytes including all entries, labels and structured metadata.
func (b Batch) Size() int {
	var size int
	for _, stream := range b.Streams {
		size += len(stream.Labels)
		for _, entry := range stream.Entries {
			size += len(entry.Line)
			for _, sm := range entry.StructuredMetadata {
				size += len(sm.Name) + len(sm.Value)
			}
		}
	}
	return size
}

// LogFormat represents different log formats we want to generate
type LogFormat string

const (
	LogFormatJSON    LogFormat = "json"
	LogFormatLogfmt  LogFormat = "logfmt"
	LogFormatNginx   LogFormat = "nginx"
	LogFormatApache  LogFormat = "apache"
	LogFormatSyslog  LogFormat = "syslog"
	LogFormatDefault LogFormat = "default"
)

// Label cardinality configuration
type LabelConfig struct {
	Clusters    int // 1-10 clusters
	Namespaces  int // 10-100 namespaces
	Services    int // 100-1000 services
	Pods        int // 1000-10000 pods
	Containers  int // 1-5 containers per pod
	LogFormats  []LogFormat
	LogLevels   []string
	Components  []string
	EnvTypes    []string
	Regions     []string
	Datacenters []string
}

var defaultLabelConfig = LabelConfig{
	Clusters:    5,
	Namespaces:  50,
	Services:    200,
	Pods:        5000,
	Containers:  3,
	LogFormats:  []LogFormat{LogFormatJSON, LogFormatLogfmt, LogFormatNginx, LogFormatApache, LogFormatSyslog, LogFormatDefault},
	LogLevels:   []string{"debug", "info", "warn", "error"},
	Components:  []string{"api", "db", "cache", "queue", "worker", "scheduler", "proxy"},
	EnvTypes:    []string{"prod", "staging", "dev"},
	Regions:     []string{"us-east-1", "us-west-2", "eu-west-1", "ap-southeast-1"},
	Datacenters: []string{"dc1", "dc2", "dc3"},
}

// GenerateBatch creates an iterator that produces batches of log entries with configurable cardinality
func GenerateBatch(numStreams int) iter.Seq[Batch] {
	cfg := defaultLabelConfig
	rnd := rand.New(rand.NewSource(1)) // Use fixed seed for reproducibility

	// Pre-generate all possible label combinations
	var streams []logproto.Stream
	for i := 0; i < numStreams; i++ {
		cluster := fmt.Sprintf("cluster-%d", rnd.Intn(cfg.Clusters))
		namespace := fmt.Sprintf("namespace-%d", rnd.Intn(cfg.Namespaces))
		service := fmt.Sprintf("service-%d", rnd.Intn(cfg.Services))
		pod := fmt.Sprintf("pod-%d", rnd.Intn(cfg.Pods))
		container := fmt.Sprintf("container-%d", rnd.Intn(cfg.Containers))
		env := cfg.EnvTypes[rnd.Intn(len(cfg.EnvTypes))]
		region := cfg.Regions[rnd.Intn(len(cfg.Regions))]
		dc := cfg.Datacenters[rnd.Intn(len(cfg.Datacenters))]
		component := cfg.Components[rnd.Intn(len(cfg.Components))]

		// Create label string in Prometheus format
		labels := fmt.Sprintf(
			`{cluster="%s", namespace="%s", service="%s", pod="%s", container="%s", env="%s", region="%s", datacenter="%s", component="%s"}`,
			cluster, namespace, service, pod, container, env, region, dc, component,
		)

		// Generate entries for this stream
		format := cfg.LogFormats[rnd.Intn(len(cfg.LogFormats))]
		level := cfg.LogLevels[rnd.Intn(len(cfg.LogLevels))]
		entries := generateEntries(format, level, rnd)

		stream := logproto.Stream{
			Labels:  labels,
			Entries: entries,
		}
		streams = append(streams, stream)
	}

	// Return an iterator that yields batches
	batchSize := 100 // Number of streams per batch
	return func(yield func(Batch) bool) {
		for i := 0; i < len(streams); i += batchSize {
			end := i + batchSize
			if end > len(streams) {
				end = len(streams)
			}
			batch := Batch{
				Streams: streams[i:end],
			}
			if !yield(batch) {
				break
			}
		}
	}
}

// generateEntries creates log entries in different formats with OTEL attributes
func generateEntries(format LogFormat, level string, rnd *rand.Rand) []logproto.Entry {
	numEntries := 10 + rnd.Intn(90) // 10-100 entries per stream
	entries := make([]logproto.Entry, numEntries)
	now := time.Now()

	for i := 0; i < numEntries; i++ {
		ts := now.Add(time.Duration(i) * time.Second)
		line := generateLogLine(format, level, rnd)

		// Add OTEL structured metadata
		metadata := []logproto.LabelAdapter{
			{Name: "level", Value: level},
			{Name: "trace.id", Value: generateTraceID(rnd)},
			{Name: "span.id", Value: generateSpanID(rnd)},
			{Name: "component", Value: "application"},
		}

		entries[i] = logproto.Entry{
			Timestamp:          ts,
			Line:               line,
			StructuredMetadata: metadata,
		}
	}

	return entries
}

// generateLogLine creates a log line in the specified format
func generateLogLine(format LogFormat, level string, rnd *rand.Rand) string {
	switch format {
	case LogFormatJSON:
		return fmt.Sprintf(`{"level":"%s","ts":"%s","msg":"Request processed","method":"GET","path":"/api/v1/users","status":%d,"duration":%d}`,
			level,
			time.Now().Format(time.RFC3339),
			httpStatus[rnd.Intn(len(httpStatus))],
			rnd.Intn(1000),
		)
	case LogFormatLogfmt:
		return fmt.Sprintf(`level=%s ts=%s msg="Request processed" method=GET path=/api/v1/users status=%d duration=%dms`,
			level,
			time.Now().Format(time.RFC3339),
			httpStatus[rnd.Intn(len(httpStatus))],
			rnd.Intn(1000),
		)
	case LogFormatNginx:
		return fmt.Sprintf(`%s - - [%s] "GET /api/v1/users HTTP/1.1" %d %d "%s" "Mozilla/5.0"`,
			randomIP(rnd),
			time.Now().Format("02/Jan/2006:15:04:05 -0700"),
			httpStatus[rnd.Intn(len(httpStatus))],
			rnd.Intn(10000),
			randomReferer(rnd),
		)
	case LogFormatApache:
		return fmt.Sprintf(`%s - - [%s] "GET /api/v1/users HTTP/1.1" %d %d`,
			randomIP(rnd),
			time.Now().Format("02/Jan/2006:15:04:05 -0700"),
			httpStatus[rnd.Intn(len(httpStatus))],
			rnd.Intn(10000),
		)
	case LogFormatSyslog:
		return fmt.Sprintf(`<%d>1 %s %s %s %d - - %s`,
			rnd.Intn(191)+1,
			time.Now().Format(time.RFC3339),
			randomHostname(rnd),
			"app",
			rnd.Intn(99999),
			fmt.Sprintf("Process started with pid=%d", rnd.Intn(99999)),
		)
	default:
		return fmt.Sprintf(`%s [%s] %s: Message %d`,
			time.Now().Format(time.RFC3339),
			level,
			"app",
			rnd.Intn(99999),
		)
	}
}

var httpStatus = []int{200, 201, 204, 301, 302, 400, 401, 403, 404, 500, 503}

func generateTraceID(rnd *rand.Rand) string {
	b := make([]byte, 16)
	rnd.Read(b)
	return fmt.Sprintf("%x", b)
}

func generateSpanID(rnd *rand.Rand) string {
	b := make([]byte, 8)
	rnd.Read(b)
	return fmt.Sprintf("%x", b)
}

func randomIP(rnd *rand.Rand) string {
	return fmt.Sprintf("%d.%d.%d.%d", rnd.Intn(256), rnd.Intn(256), rnd.Intn(256), rnd.Intn(256))
}

func randomHostname(rnd *rand.Rand) string {
	return fmt.Sprintf("host-%d", rnd.Intn(100))
}

func randomReferer(rnd *rand.Rand) string {
	referers := []string{
		"https://grafana.com",
		"https://github.com",
		"https://google.com",
		"https://example.com",
	}
	return referers[rnd.Intn(len(referers))]
}

//go:generate go run ./cmd/generate/main.go -size 1073741824 -output dataset.pb

// GenerateDataset generates a dataset of approximately the specified size in bytes
func GenerateDataset(targetSize int64, outputFile string) error {
	// Start with a reasonable number of streams and adjust based on size
	numStreams := 1000
	batch := <-Pull(GenerateBatch(numStreams))
	avgStreamSize := int64(batch.Size()) / int64(len(batch.Streams))

	// Calculate required number of streams to reach target size
	estimatedStreams := int(targetSize / avgStreamSize)

	// Generate full dataset
	streams := make([]logproto.Stream, 0, estimatedStreams)
	for batch := range Pull(GenerateBatch(estimatedStreams)) {
		streams = append(streams, batch.Streams...)
		if int64(len(streams))*avgStreamSize >= targetSize {
			break
		}
	}

	// Create PushRequest
	req := logproto.PushRequest{
		Streams: streams,
	}

	// Marshal to protobuf
	data, err := req.Marshal()
	if err != nil {
		return fmt.Errorf("failed to marshal dataset: %w", err)
	}

	// Write to file
	if err := os.WriteFile(outputFile, data, 0o644); err != nil {
		return fmt.Errorf("failed to write dataset: %w", err)
	}

	return nil
}

// Pull converts a push-style iterator to a pull-style channel
func Pull[T any](seq iter.Seq[T]) <-chan T {
	ch := make(chan T)
	go func() {
		defer close(ch)
		seq(func(t T) bool {
			ch <- t
			return true
		})
	}()
	return ch
}
