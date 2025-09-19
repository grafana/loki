package bench

import (
	"encoding/json"
	"fmt"
	"iter"
	"maps"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/grafana/loki/v3/pkg/logproto"
)

const (
	DefaultDataDir        = "./data"
	configFileName        = "generator.json"
	errorLevel            = "error"         // Constant for the error level string
	MaxBatchSize          = 5 * 1024 * 1024 // 5MB max batch size
	TargetStreamsPerBatch = 10
	DefaultTargetSize     = 2 * 1024 * 1024 * 1024 // 2GB default target size
	TimeWindowDuration    = 5 * time.Minute        // 5 minute time buckets
	EstimatedEntrySize    = 500                    // ~500 bytes per entry (log line + metadata + labels)
)

// Batch represents a collection of log streams
type Batch struct {
	Streams []logproto.Stream
}

func (b Batch) Entries() int {
	var total int
	for _, stream := range b.Streams {
		total += len(stream.Entries)
	}
	return total
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

// LabelConfig configures the cardinality of generated labels
type LabelConfig struct {
	Clusters    int      // 1-10 clusters
	Namespaces  int      // 10-100 namespaces
	Services    int      // 100-1000 services
	Pods        int      // 1000-10000 pods
	Containers  int      // 1-5 containers per pod
	LogLevels   []string // Log levels to use
	EnvTypes    []string // Environment types
	Regions     []string // Regions
	Datacenters []string // Datacenters
}

// Default configuration with reasonable cardinality values
var defaultLabelConfig = LabelConfig{
	Clusters:    5,
	Namespaces:  50,
	Services:    200,
	Pods:        5000,
	Containers:  3,
	LogLevels:   []string{"debug", "info", "warn", "error"},
	EnvTypes:    []string{"prod", "staging", "dev"},
	Regions:     []string{"us-east-1", "us-west-2", "eu-west-1", "ap-southeast-1"},
	Datacenters: []string{"dc1", "dc2", "dc3"},
}

// GeneratorConfig contains all configuration for the log generator
type GeneratorConfig struct {
	StartTime         time.Time
	TimeSpread        time.Duration
	LabelConfig       LabelConfig
	NumStreams        int               // Number of streams to generate per batch
	NumPartitions     int               // Number of partitions to distribute streams across
	PartitionStrategy PartitionStrategy // Strategy for partitioning streams
	Seed              int64             // Source of randomness
	TargetSize        int64             // Target total dataset size in bytes
}

// PartitionStrategy defines how streams are distributed across partitions
type PartitionStrategy string

const (
	// PartitionByStreamLabels distributes streams by their labels and tenant (default)
	PartitionByStreamLabels PartitionStrategy = "stream_labels"
	// PartitionByServiceName distributes streams by service_name label and tenant
	PartitionByServiceName PartitionStrategy = "service_name"
)

// Default generator configuration with sensible values
var defaultGeneratorConfig = GeneratorConfig{
	StartTime:         time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
	TimeSpread:        time.Hour, // Reduced from 24h to 2h for denser, more realistic data
	LabelConfig:       defaultLabelConfig,
	NumStreams:        1000,                    // Default to 250 streams per batch
	NumPartitions:     8,                       // Default to 10 partitions
	PartitionStrategy: PartitionByStreamLabels, // Default to stream labels partitioning
	Seed:              1,                       // Default to seed 1 for reproducibility
	TargetSize:        DefaultTargetSize,       // 2GB default target size
}

// NewRand creates a new random source using the configured seed
func (c *GeneratorConfig) NewRand() *rand.Rand {
	return rand.New(rand.NewSource(c.Seed))
}

// StreamMetadata holds the consistent properties of a stream
type StreamMetadata struct {
	Labels string
	App    Application
}

// Generator represents a log generator with configuration
type Generator struct {
	config      GeneratorConfig
	rnd         *rand.Rand
	streamsMeta []StreamMetadata       // Pre-generated stream metadata used across batches
	apps        map[string]Application // Map of available applications by name
}

// Opt represents configuration options for the generator
type Opt struct {
	startTime         time.Time
	timeSpread        time.Duration
	labelConfig       LabelConfig
	numStreams        int               // Number of streams to generate per batch
	numPartitions     int               // Number of partitions to distribute streams across
	partitionStrategy PartitionStrategy // Strategy for partitioning streams
	seed              int64             // Source of randomness
	targetSize        int64             // Target total dataset size in bytes
}

// WithStartTime sets the start time for log generation
func (o Opt) WithStartTime(t time.Time) Opt {
	o.startTime = t
	return o
}

// WithTimeSpread sets the time spread for log generation
func (o Opt) WithTimeSpread(d time.Duration) Opt {
	o.timeSpread = d
	return o
}

// WithLabelCardinality configures the cardinality of different labels
func (o Opt) WithLabelCardinality(clusters, namespaces, services, pods, containers int) Opt {
	o.labelConfig.Clusters = clusters
	o.labelConfig.Namespaces = namespaces
	o.labelConfig.Services = services
	o.labelConfig.Pods = pods
	o.labelConfig.Containers = containers
	return o
}

// WithLabelConfig sets the entire label configuration
func (o Opt) WithLabelConfig(cfg LabelConfig) Opt {
	o.labelConfig = cfg
	return o
}

// WithNumStreams sets the number of streams to generate per batch
func (o Opt) WithNumStreams(n int) Opt {
	o.numStreams = n
	return o
}

// WithSeed sets the seed for random number generation
func (o Opt) WithSeed(seed int64) Opt {
	o.seed = seed
	return o
}

// WithNumPartitions sets the number of partitions to distribute streams across
func (o Opt) WithNumPartitions(n int) Opt {
	o.numPartitions = n
	return o
}

// WithPartitionStrategy sets the strategy for partitioning streams
func (o Opt) WithPartitionStrategy(strategy PartitionStrategy) Opt {
	o.partitionStrategy = strategy
	return o
}

// DefaultOpt returns the default options
func DefaultOpt() Opt {
	return Opt{
		startTime:         defaultGeneratorConfig.StartTime,
		timeSpread:        defaultGeneratorConfig.TimeSpread,
		labelConfig:       defaultGeneratorConfig.LabelConfig,
		numStreams:        defaultGeneratorConfig.NumStreams,
		numPartitions:     defaultGeneratorConfig.NumPartitions,
		partitionStrategy: defaultGeneratorConfig.PartitionStrategy,
		seed:              1, // Default to seed 1 for reproducibility
		targetSize:        defaultGeneratorConfig.TargetSize,
	}
}

// NewGenerator creates a new generator with the given options
func NewGenerator(opt Opt) *Generator {
	g := &Generator{
		config: GeneratorConfig{
			StartTime:         opt.startTime,
			TimeSpread:        opt.timeSpread,
			LabelConfig:       opt.labelConfig,
			NumStreams:        opt.numStreams,
			NumPartitions:     opt.numPartitions,
			PartitionStrategy: opt.partitionStrategy,
			Seed:              opt.seed,
			TargetSize:        opt.targetSize,
		},
		rnd:  rand.New(rand.NewSource(opt.seed)),
		apps: make(map[string]Application),
	}

	// Initialize available applications
	for _, app := range defaultApplications {
		g.apps[app.Name] = app
	}

	return g
}

// generateStreamMetadata pre-generates metadata for all streams
func (g *Generator) generateStreamMetadata() {
	numStreams := g.config.NumStreams
	if numStreams == 0 {
		numStreams = defaultGeneratorConfig.NumStreams
	}

	g.streamsMeta = make([]StreamMetadata, numStreams)

	// Create slice of available app names for random selection
	var appNames []string
	for name := range g.apps {
		appNames = append(appNames, name)
	}

	// Sort app names for deterministic selection
	sort.Strings(appNames)

	// For each stream, generate consistent metadata
	// High-traffic apps get more streams proportionally
	for i := 0; i < numStreams; i++ {
		// Pick application based on traffic level - high traffic apps get more streams
		var appName string
		if g.rnd.Float32() < 0.6 { // 60% chance for high traffic apps
			// Select from high traffic apps
			highTrafficApps := []string{"web-server", "database", "kafka", "nginx", "kubernetes"}
			appName = highTrafficApps[g.rnd.Intn(len(highTrafficApps))]
		} else if g.rnd.Float32() < 0.8 { // 20% chance for medium traffic apps
			// Select from medium traffic apps
			mediumTrafficApps := []string{"cache", "auth-service", "mimir", "loki", "tempo", "prometheus"}
			appName = mediumTrafficApps[g.rnd.Intn(len(mediumTrafficApps))]
		} else { // 20% chance for low traffic apps
			// Select from low traffic apps
			lowTrafficApps := []string{"syslog", "grafana"}
			appName = lowTrafficApps[g.rnd.Intn(len(lowTrafficApps))]
		}
		app := g.apps[appName]

		// Generate deterministic labels for this stream
		cluster := fmt.Sprintf("cluster-%d", g.rnd.Intn(g.config.LabelConfig.Clusters))
		namespace := fmt.Sprintf("namespace-%d", g.rnd.Intn(g.config.LabelConfig.Namespaces))
		service := fmt.Sprintf("service-%d", g.rnd.Intn(g.config.LabelConfig.Services))
		pod := fmt.Sprintf("pod-%d", g.rnd.Intn(g.config.LabelConfig.Pods))
		container := fmt.Sprintf("container-%d", g.rnd.Intn(g.config.LabelConfig.Containers))
		env := g.config.LabelConfig.EnvTypes[g.rnd.Intn(len(g.config.LabelConfig.EnvTypes))]
		region := g.config.LabelConfig.Regions[g.rnd.Intn(len(g.config.LabelConfig.Regions))]
		dc := g.config.LabelConfig.Datacenters[g.rnd.Intn(len(g.config.LabelConfig.Datacenters))]

		// Build Loki label string
		labels := fmt.Sprintf(
			`{cluster="%s", namespace="%s", service="%s", pod="%s", container="%s", env="%s", region="%s", datacenter="%s", service_name="%s"}`,
			cluster, namespace, service, pod, container, env, region, dc, app.Name,
		)

		g.streamsMeta[i] = StreamMetadata{
			Labels: labels,
			App:    app,
		}
	}
}

// Batches returns an iterator that produces log batches
// Each batch contains the configured number of streams with generated log entries
func (g *Generator) Batches() iter.Seq[*Batch] {
	var (
		// calculate numer of batches for the target size
		totalBatches = int(g.config.TargetSize / MaxBatchSize)
		// calculate time windows to spread batches over.
		numTimeWindows = int(g.config.TimeSpread / TimeWindowDuration)

		// calculate batches per time bucket
		numBatchesPerWindow = max(1, totalBatches/numTimeWindows)
		// calculate entries per stream based on batch size and target stream count
		// Target: 5MB batch / 10 streams = 500KB per stream
		// 500KB / 500 bytes per entry = ~1000 entries per stream
		baseEntriesPerStream = MaxBatchSize / TargetStreamsPerBatch / EstimatedEntrySize
	)

	// Pre-generate stream metadata once
	g.generateStreamMetadata()

	return func(yield func(*Batch) bool) {
		for window := range numTimeWindows {
			startTime := g.config.StartTime.Add(time.Duration(window) * TimeWindowDuration)

			for range numBatchesPerWindow {
				// Randomly select streams for this batch
				streams := make([]logproto.Stream, 0, TargetStreamsPerBatch)
				currentBatchSize := 0
				selectedStreams := make(map[int]bool)

				// Keep adding streams until we reach target count or batch size limit
				for len(streams) < TargetStreamsPerBatch && currentBatchSize < MaxBatchSize {
					// Pick a random stream we haven't used in this batch
					streamIdx := g.rnd.Intn(len(g.streamsMeta))
					if selectedStreams[streamIdx] {
						continue
					}

					meta := g.streamsMeta[streamIdx]
					selectedStreams[streamIdx] = true

					// Calculate entries per stream based on traffic level
					var entriesPerStream int
					switch meta.App.TrafficLevel {
					case HighTraffic:
						entriesPerStream = baseEntriesPerStream + g.rnd.Intn(baseEntriesPerStream/2) // 1000-1500 entries
					case MediumTraffic:
						entriesPerStream = baseEntriesPerStream // ~1000 entries
					case LowTraffic:
						entriesPerStream = baseEntriesPerStream - g.rnd.Intn(baseEntriesPerStream/2) // 500-1000 entries
					default:
						entriesPerStream = baseEntriesPerStream
					}

					// Generate entries for this time bucket
					entries := g.generateEntriesForTimeBucket(meta, startTime, TimeWindowDuration, entriesPerStream)

					// Estimate size of this stream
					streamSize := len(meta.Labels)
					for _, entry := range entries {
						streamSize += len(entry.Line)
						for _, sm := range entry.StructuredMetadata {
							streamSize += len(sm.Name) + len(sm.Value)
						}
					}

					streams = append(streams, logproto.Stream{
						Labels:  meta.Labels,
						Entries: entries,
					})
					currentBatchSize += streamSize
				}

				if len(streams) > 0 {
					if !yield(&Batch{Streams: streams}) {
						return
					}
				}
			}
		}
	}
}

// generateEntriesForTimeBucket creates log entries for a specific stream within a time bucket
func (g *Generator) generateEntriesForTimeBucket(meta StreamMetadata, bucketStartTime time.Time, bucketDuration time.Duration, targetEntries int) []logproto.Entry {
	app := meta.App
	faker := NewFaker(g.rnd)

	entries := make([]logproto.Entry, 0, targetEntries)

	// Prepare OTEL attributes for this stream
	otel := OTELAttributes{
		Resource: make(map[string]string),
	}
	maps.Copy(otel.Resource, app.OTELResource)

	// Replace template variables
	var templateKeys []string
	for k, v := range otel.Resource {
		if strings.HasPrefix(v, "${") && strings.HasSuffix(v, "}") {
			templateKeys = append(templateKeys, k)
		}
	}
	sort.Strings(templateKeys)

	for _, k := range templateKeys {
		v := otel.Resource[k]
		switch v {
		case "${HOSTNAME}":
			otel.Resource[k] = faker.Hostname()
		case "${BROKER_ID}":
			otel.Resource[k] = fmt.Sprintf("%d", g.rnd.Intn(10))
		}
	}

	// Generate points spread across the time bucket
	spreadInterval := bucketDuration / time.Duration(targetEntries)

	for i := range targetEntries {
		ts := bucketStartTime.Add(time.Duration(i) * spreadInterval)

		// Randomly determine if this is a burst point (up to 5x logs)
		// 10% chance of being a burst point
		isBurstPoint := g.rnd.Float32() < 0.1
		entriesPerPoint := 1
		if isBurstPoint {
			entriesPerPoint = 1 + g.rnd.Intn(5) // 1-5 entries for burst points
		}

		// Generate multiple entries for this point (1 for normal, 1-5 for bursts)
		for j := 0; j < entriesPerPoint; j++ {
			jitter := time.Duration(g.rnd.Int63n(int64(spreadInterval)))
			finalTs := ts.Add(jitter)

			// Randomly select log level
			level := g.config.LabelConfig.LogLevels[g.rnd.Intn(len(g.config.LabelConfig.LogLevels))]

			// Generate trace context for some entries
			var traceCtx *OTELTraceContext
			if g.rnd.Float32() < 0.3 {
				traceCtx = &OTELTraceContext{
					TraceID: faker.TraceID(),
					SpanID:  faker.SpanID(),
				}
			}

			// Generate log line
			line := app.LogGenerator(level, finalTs, faker)

			// Create metadata
			var metadata []logproto.LabelAdapter
			metadata = append(metadata,
				logproto.LabelAdapter{Name: "level", Value: level},
				logproto.LabelAdapter{Name: "detected_level", Value: level},
			)

			// Add resource attributes
			var resourceKeys []string
			for k := range otel.Resource {
				resourceKeys = append(resourceKeys, k)
			}
			sort.Strings(resourceKeys)
			for _, k := range resourceKeys {
				metadata = append(metadata, logproto.LabelAdapter{
					Name:  "resource_" + k,
					Value: otel.Resource[k],
				})
			}

			// Add trace context if present
			if traceCtx != nil {
				metadata = append(metadata,
					logproto.LabelAdapter{Name: "trace_id", Value: traceCtx.TraceID},
					logproto.LabelAdapter{Name: "span_id", Value: traceCtx.SpanID},
				)
			}

			entries = append(entries, logproto.Entry{
				Timestamp:          finalTs,
				Line:               line,
				StructuredMetadata: metadata,
			})
		}
	}

	return entries
}

// GenerateDataset generates a dataset of approximately the specified size
func (g *Generator) GenerateDataset(targetSize int64, outputFile string) error {
	var totalSize int64
	streams := make([]logproto.Stream, 0, g.config.NumStreams)

	for batch := range g.Batches() {
		batchSize := int64(batch.Size())
		streams = append(streams, batch.Streams...)
		totalSize += batchSize
		if totalSize >= targetSize {
			break
		}
	}

	req := logproto.PushRequest{Streams: streams}
	data, err := req.Marshal()
	if err != nil {
		return fmt.Errorf("failed to marshal dataset: %w", err)
	}

	return os.WriteFile(outputFile, data, 0o644)
}

// OTELAttributes represents OpenTelemetry attributes for logs
type OTELAttributes struct {
	Resource map[string]string // Resource attributes constant for the service
	Trace    *OTELTraceContext // Optional trace context
}

// OTELTraceContext represents OpenTelemetry trace context
type OTELTraceContext struct {
	TraceID string
	SpanID  string
}

// SaveConfig saves the generator configuration to a file in the data directory
func SaveConfig(dataDir string, config *GeneratorConfig) error {
	configPath := filepath.Join(dataDir, configFileName)
	configData, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal generator config: %w", err)
	}
	if err := os.WriteFile(configPath, configData, 0o644); err != nil {
		return fmt.Errorf("failed to write generator config: %w", err)
	}
	return nil
}

// LoadConfig loads the generator configuration from the data directory
func LoadConfig(dataDir string) (*GeneratorConfig, error) {
	configPath := filepath.Join(dataDir, configFileName)
	configData, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read generator config: %w", err)
	}

	var config GeneratorConfig
	if err := json.Unmarshal(configData, &config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal generator config: %w", err)
	}

	return &config, nil
}
