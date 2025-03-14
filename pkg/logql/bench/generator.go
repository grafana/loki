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
	DefaultDataDir = "./data"
	configFileName = "generator.json"
	errorLevel     = "error" // Constant for the error level string
)

// Batch represents a collection of log streams
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
	StartTime  time.Time
	TimeSpread time.Duration
	// DenseIntervals defines periods of high log density
	// Each interval will have 10x more logs than normal periods
	DenseIntervals []DenseInterval
	LabelConfig    LabelConfig
	NumStreams     int   // Number of streams to generate per batch
	Seed           int64 // Source of randomness
}

// DenseInterval represents a period of high log volume
type DenseInterval struct {
	Start    time.Time
	Duration time.Duration
}

// Default generator configuration with sensible values
var defaultGeneratorConfig = GeneratorConfig{
	StartTime:   time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
	TimeSpread:  24 * time.Hour,
	LabelConfig: defaultLabelConfig,
	NumStreams:  250, // Default to 250 streams per batch
	Seed:        1,   // Default to seed 1 for reproducibility
	DenseIntervals: []DenseInterval{
		{
			Start:    time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC),
			Duration: time.Hour,
		},
		{
			Start:    time.Date(2024, 1, 1, 15, 0, 0, 0, time.UTC),
			Duration: 30 * time.Minute,
		},
	},
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
	startTime      time.Time
	timeSpread     time.Duration
	denseIntervals []DenseInterval
	labelConfig    LabelConfig
	numStreams     int   // Number of streams to generate per batch
	seed           int64 // Source of randomness
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

// WithDenseInterval adds a dense interval to the configuration
func (o Opt) WithDenseInterval(start time.Time, duration time.Duration) Opt {
	o.denseIntervals = append(o.denseIntervals, DenseInterval{
		Start:    start,
		Duration: duration,
	})
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

// DefaultOpt returns the default options
func DefaultOpt() Opt {
	return Opt{
		startTime:      defaultGeneratorConfig.StartTime,
		timeSpread:     defaultGeneratorConfig.TimeSpread,
		denseIntervals: defaultGeneratorConfig.DenseIntervals,
		labelConfig:    defaultGeneratorConfig.LabelConfig,
		numStreams:     defaultGeneratorConfig.NumStreams,
		seed:           1, // Default to seed 1 for reproducibility
	}
}

// NewGenerator creates a new generator with the given options
func NewGenerator(opt Opt) *Generator {
	g := &Generator{
		config: GeneratorConfig{
			StartTime:      opt.startTime,
			TimeSpread:     opt.timeSpread,
			DenseIntervals: opt.denseIntervals,
			LabelConfig:    opt.labelConfig,
			NumStreams:     opt.numStreams,
			Seed:           opt.seed,
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
	for i := 0; i < numStreams; i++ {
		// Pick a deterministic application based on stream index
		appIndex := i % len(g.apps)
		appName := appNames[appIndex]
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
	// Pre-generate stream metadata once
	g.generateStreamMetadata()

	return func(yield func(*Batch) bool) {
		for {
			// Generate streams for this batch using the same metadata but new entries
			streams := make([]logproto.Stream, len(g.streamsMeta))
			for j := range streams {
				meta := g.streamsMeta[j]

				// Generate entries specific to this stream's application and format
				entries := g.generateEntriesForStream(meta)

				streams[j] = logproto.Stream{
					Labels:  meta.Labels,
					Entries: entries,
				}
			}

			if !yield(&Batch{Streams: streams}) {
				return
			}
		}
	}
}

// generateEntriesForStream creates log entries for a specific stream
// using the application and format from the stream metadata
func (g *Generator) generateEntriesForStream(meta StreamMetadata) []logproto.Entry {
	app := meta.App
	faker := NewFaker(g.rnd)

	// Calculate how many entries to generate based on time spread
	baseEntries := 10 + g.rnd.Intn(90) // 10-100 entries per stream
	entries := make([]logproto.Entry, 0, baseEntries)

	// Generate timestamps spread across the time range
	spreadInterval := g.config.TimeSpread / time.Duration(baseEntries)

	// Prepare OTEL attributes for this stream
	otel := OTELAttributes{
		Resource: make(map[string]string),
	}

	// Copy resource attributes from the application
	maps.Copy(otel.Resource, app.OTELResource)

	// Sort keys for deterministic iteration order
	var templateKeys []string
	for k, v := range otel.Resource {
		if strings.HasPrefix(v, "${") && strings.HasSuffix(v, "}") {
			templateKeys = append(templateKeys, k)
		}
	}
	sort.Strings(templateKeys)

	// Replace template variables in deterministic order
	for _, k := range templateKeys {
		v := otel.Resource[k]
		switch v {
		case "${HOSTNAME}":
			otel.Resource[k] = faker.Hostname()
		case "${BROKER_ID}":
			otel.Resource[k] = fmt.Sprintf("%d", g.rnd.Intn(10))
		}
	}

	// Generate entries with timestamps spread across the configured time range
	for i := range baseEntries {
		ts := g.config.StartTime.Add(time.Duration(i) * spreadInterval)

		// Check if timestamp falls in a dense interval
		isDense := false
		for _, interval := range g.config.DenseIntervals {
			if ts.After(interval.Start) && ts.Before(interval.Start.Add(interval.Duration)) {
				isDense = true
				break
			}
		}

		// Generate more entries during dense intervals
		numEntries := 1
		if isDense {
			numEntries = 10 // 10x more logs during dense periods
		}

		for range numEntries {
			// Add small jitter within spread interval
			jitter := time.Duration(g.rnd.Int63n(int64(spreadInterval)))
			entryTs := ts.Add(jitter)

			// Randomly select log level for this entry, biased towards the stream's default level
			level := g.config.LabelConfig.LogLevels[g.rnd.Intn(len(g.config.LabelConfig.LogLevels))]

			// Generate trace context for some entries (about 30%)
			var traceCtx *OTELTraceContext
			if g.rnd.Float32() < 0.3 {
				traceCtx = &OTELTraceContext{
					TraceID: faker.TraceID(),
					SpanID:  faker.SpanID(),
				}
			}

			// Generate log line using the application's generators for the selected format
			line := app.LogGenerator(level, entryTs, faker)

			// Create metadata in a deterministic order
			var metadata []logproto.LabelAdapter

			metadata = append(metadata,
				logproto.LabelAdapter{Name: "level", Value: level},
				logproto.LabelAdapter{Name: "detected_level", Value: level},
			)

			// Then add resource attributes in sorted order
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

			// Finally add trace context if present
			if traceCtx != nil {
				metadata = append(metadata,
					logproto.LabelAdapter{Name: "trace_id", Value: traceCtx.TraceID},
					logproto.LabelAdapter{Name: "span_id", Value: traceCtx.SpanID},
				)
			}

			entries = append(entries, logproto.Entry{
				Timestamp:          entryTs,
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
