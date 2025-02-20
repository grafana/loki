package bench

import (
	"encoding/json"
	"fmt"
	"iter"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
)

const (
	DefaultDataDir = "./data"
	configFileName = "generator.json"
)

// TestCase represents a LogQL test case for benchmarking and testing
type TestCase struct {
	Query     string
	Start     time.Time
	End       time.Time
	Direction logproto.Direction
	Step      time.Duration // Step size for metric queries
}

// Name returns a descriptive name for the test case.
// For log queries, it includes the direction.
// For metric queries (rate, sum), it returns the query with step size.
func (c TestCase) Name() string {
	expr, err := syntax.ParseExpr(c.Query)
	if err != nil {
		return fmt.Sprintf("%s [%v]", c.Query, c.Direction)
	}
	if _, ok := expr.(syntax.SampleExpr); ok {
		return c.Query
	}
	return fmt.Sprintf("%s [%v]", c.Query, c.Direction)
}

// Description returns a detailed description of the test case including time range
func (c TestCase) Description() string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Query: %s\n", c.Query))
	b.WriteString(fmt.Sprintf("Time Range: %v to %v\n", c.Start.Format(time.RFC3339), c.End.Format(time.RFC3339)))
	if c.Step > 0 {
		b.WriteString(fmt.Sprintf("Step: %v\n", c.Step))
	}
	b.WriteString(fmt.Sprintf("Direction: %v", c.Direction))
	return b.String()
}

type labelMatcher struct {
	name, value string
}

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

type GeneratorConfig struct {
	StartTime time.Time
	// TimeSpread is the total time range to spread logs across
	TimeSpread time.Duration
	// DenseIntervals defines periods of high log density
	// Each interval will have 10x more logs than normal periods
	DenseIntervals []struct {
		Start    time.Time
		Duration time.Duration
	}
	LabelConfig LabelConfig
	NumStreams  int   // Number of streams to generate per batch
	Seed        int64 // Source of randomness
}

var defaultGeneratorConfig = GeneratorConfig{
	StartTime:   time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
	TimeSpread:  24 * time.Hour,
	LabelConfig: defaultLabelConfig,
	NumStreams:  1000, // Default to 1000 streams per batch
	Seed:        1,    // Default to seed 1 for reproducibility
	DenseIntervals: []struct {
		Start    time.Time
		Duration time.Duration
	}{
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

// NewRand creates a new rand.Rand using the generator config seed
func (c *GeneratorConfig) NewRand() *rand.Rand {
	return rand.New(rand.NewSource(c.Seed))
}

// Generator represents a log generator with configuration
type Generator struct {
	config GeneratorConfig
	rnd    *rand.Rand
}

// Opt represents configuration options for the generator
type Opt struct {
	startTime      time.Time
	timeSpread     time.Duration
	denseIntervals []struct {
		Start    time.Time
		Duration time.Duration
	}
	labelConfig LabelConfig
	numStreams  int   // Number of streams to generate per batch
	seed        int64 // Source of randomness
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
	o.denseIntervals = append(o.denseIntervals, struct {
		Start    time.Time
		Duration time.Duration
	}{
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

// WithSeed sets the seed for random number generation using an int64
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
	return &Generator{
		config: GeneratorConfig{
			StartTime:      opt.startTime,
			TimeSpread:     opt.timeSpread,
			DenseIntervals: opt.denseIntervals,
			LabelConfig:    opt.labelConfig,
			NumStreams:     opt.numStreams,
			Seed:           opt.seed,
		},
		rnd: rand.New(rand.NewSource(opt.seed)), // Use configured source
	}
}

// Generate returns an iterator of batches with the configured number of streams
func (g *Generator) Batches() iter.Seq[*Batch] {
	return func(yield func(*Batch) bool) {
		// Pre-generate all possible label combinations
		var streams []logproto.Stream
		numStreams := g.config.NumStreams
		if numStreams == 0 {
			numStreams = defaultGeneratorConfig.NumStreams
		}

		for i := 0; i < numStreams; i++ {
			cluster := fmt.Sprintf("cluster-%d", g.rnd.Intn(g.config.LabelConfig.Clusters))
			namespace := fmt.Sprintf("namespace-%d", g.rnd.Intn(g.config.LabelConfig.Namespaces))
			service := fmt.Sprintf("service-%d", g.rnd.Intn(g.config.LabelConfig.Services))
			pod := fmt.Sprintf("pod-%d", g.rnd.Intn(g.config.LabelConfig.Pods))
			container := fmt.Sprintf("container-%d", g.rnd.Intn(g.config.LabelConfig.Containers))
			env := g.config.LabelConfig.EnvTypes[g.rnd.Intn(len(g.config.LabelConfig.EnvTypes))]
			region := g.config.LabelConfig.Regions[g.rnd.Intn(len(g.config.LabelConfig.Regions))]
			dc := g.config.LabelConfig.Datacenters[g.rnd.Intn(len(g.config.LabelConfig.Datacenters))]
			component := g.config.LabelConfig.Components[g.rnd.Intn(len(g.config.LabelConfig.Components))]

			labels := fmt.Sprintf(
				`{cluster="%s", namespace="%s", service_name="%s", pod="%s", container="%s", env="%s", region="%s", datacenter="%s", component="%s"}`,
				cluster, namespace, service, pod, container, env, region, dc, component,
			)

			format := g.config.LabelConfig.LogFormats[g.rnd.Intn(len(g.config.LabelConfig.LogFormats))]
			level := g.config.LabelConfig.LogLevels[g.rnd.Intn(len(g.config.LabelConfig.LogLevels))]
			entries := g.generateEntries(format, level)

			streams = append(streams, logproto.Stream{
				Labels:  labels,
				Entries: entries,
			})
		}

		// Send streams in batches
		batchSize := 100 // TODO: Make this configurable if needed
		for i := 0; i < len(streams); i += batchSize {
			end := i + batchSize
			if end > len(streams) {
				end = len(streams)
			}
			if !yield(&Batch{Streams: streams[i:end]}) {
				return
			}
		}
	}
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

// generateEntries is now a method of Generator
func (g *Generator) generateEntries(format LogFormat, level string) []logproto.Entry {
	return generateEntriesWithConfig(format, level, g.rnd, g.config)
}

// generateEntriesWithConfig creates log entries with OTEL attributes and deterministic timestamps
func generateEntriesWithConfig(format LogFormat, level string, rnd *rand.Rand, cfg GeneratorConfig) []logproto.Entry {
	// Calculate base number of entries based on time spread
	baseEntries := 10 + rnd.Intn(90) // 10-100 entries per stream
	entries := make([]logproto.Entry, 0, baseEntries)

	// Generate timestamps spread across the time range
	spreadInterval := cfg.TimeSpread / time.Duration(baseEntries)

	// Select a random application for this stream
	app := defaultApplications[rnd.Intn(len(defaultApplications))]

	// Create OTEL attributes for this stream
	otel := OTELAttributes{
		Resource: app.OTELResource,
	}

	// Add dynamic resource attributes
	for k, v := range otel.Resource {
		if strings.HasPrefix(v, "${") && strings.HasSuffix(v, "}") {
			// Replace template variables
			switch v {
			case "${HOSTNAME}":
				otel.Resource[k] = randomHostname(rnd)
			case "${BROKER_ID}":
				otel.Resource[k] = fmt.Sprintf("%d", rnd.Intn(10))
			}
		}
	}

	for i := 0; i < baseEntries; i++ {
		ts := cfg.StartTime.Add(time.Duration(i) * spreadInterval)

		// Check if this timestamp falls in a dense interval
		isDense := false
		for _, interval := range cfg.DenseIntervals {
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

		for j := 0; j < numEntries; j++ {
			// Add small jitter within the spread interval for multiple entries
			jitter := time.Duration(rnd.Int63n(int64(spreadInterval)))
			entryTs := ts.Add(jitter)

			// Generate trace context for this entry (about 30% of entries)
			var traceCtx *OTELTraceContext
			if rnd.Float32() < 0.3 {
				traceCtx = &OTELTraceContext{
					TraceID: generateTraceID(rnd),
					SpanID:  generateSpanID(rnd),
				}
			}

			// Generate the log line
			line := generateLogLine(format, level, rnd)

			// Create metadata in a deterministic order
			var metadata []logproto.LabelAdapter

			// First add the level and detected_level
			metadata = append(metadata,
				logproto.LabelAdapter{Name: "level", Value: level},
				logproto.LabelAdapter{Name: "detected_level", Value: detectLogLevel(line)},
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

// detectLogLevel analyzes a log line to determine its level
func detectLogLevel(line string) string {
	line = strings.ToLower(line)
	if strings.Contains(line, "error") || strings.Contains(line, "exception") || strings.Contains(line, "fail") {
		return "error"
	}
	if strings.Contains(line, "warn") || strings.Contains(line, "warning") {
		return "warn"
	}
	if strings.Contains(line, "info") || strings.Contains(line, "information") {
		return "info"
	}
	if strings.Contains(line, "debug") {
		return "debug"
	}
	return "info" // default level
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

// OTELAttributes represents OpenTelemetry attributes for logs
type OTELAttributes struct {
	Resource map[string]string // Resource attributes that are constant for the service
	Trace    *OTELTraceContext // Optional trace context
}

type OTELTraceContext struct {
	TraceID string
	SpanID  string
}

// Application represents a type of application that generates logs
type Application struct {
	Name           string
	Component      string
	LogPatterns    []LogPattern
	ErrorPatterns  []LogPattern
	SampledMetrics []string
	OTELResource   map[string]string // OTEL resource attributes for this application
}

// LogPattern represents a template for generating log lines
type LogPattern struct {
	Format  LogFormat
	Pattern string
	Args    []ArgGenerator // Functions that generate arguments for the pattern
}

// ArgGenerator is a function that generates an argument for a log pattern
type ArgGenerator func(*rand.Rand) interface{}

var defaultApplications = []Application{
	{
		Name:      "web-server",
		Component: "api",
		LogPatterns: []LogPattern{
			{
				Format:  LogFormatJSON,
				Pattern: `{"level":"%s","ts":"%s","msg":"HTTP request","method":"%s","path":"%s","status":%d,"duration":%d,"user_agent":"%s","client_ip":"%s"}`,
				Args: []ArgGenerator{
					func(r *rand.Rand) interface{} { return httpMethods[r.Intn(len(httpMethods))] },
					func(r *rand.Rand) interface{} { return apiPaths[r.Intn(len(apiPaths))] },
					func(r *rand.Rand) interface{} { return httpStatus[r.Intn(len(httpStatus))] },
					func(r *rand.Rand) interface{} { return r.Intn(1000) },
					func(r *rand.Rand) interface{} { return userAgents[r.Intn(len(userAgents))] },
					func(r *rand.Rand) interface{} { return randomIP(r) },
				},
			},
		},
		ErrorPatterns: []LogPattern{
			{
				Format:  LogFormatJSON,
				Pattern: `{"level":"error","ts":"%s","msg":"Error processing request","method":"%s","path":"%s","error":"%s","trace_id":"%s"}`,
				Args: []ArgGenerator{
					func(r *rand.Rand) interface{} { return httpMethods[r.Intn(len(httpMethods))] },
					func(r *rand.Rand) interface{} { return apiPaths[r.Intn(len(apiPaths))] },
					func(r *rand.Rand) interface{} { return errorMessages[r.Intn(len(errorMessages))] },
					func(r *rand.Rand) interface{} { return generateTraceID(r) },
				},
			},
		},
		OTELResource: map[string]string{
			"service_name":           "web-server",
			"service_version":        "1.0.0",
			"service_namespace":      "default",
			"telemetry_sdk_name":     "opentelemetry",
			"telemetry_sdk_language": "go",
			"deployment_environment": "production",
		},
	},
	{
		Name:      "database",
		Component: "db",
		LogPatterns: []LogPattern{
			{
				Format:  LogFormatJSON,
				Pattern: `{"level":"%s","ts":"%s","msg":"Query executed","query_type":"%s","table":"%s","duration":%d,"rows_affected":%d}`,
				Args: []ArgGenerator{
					func(r *rand.Rand) interface{} { return queryTypes[r.Intn(len(queryTypes))] },
					func(r *rand.Rand) interface{} { return dbTables[r.Intn(len(dbTables))] },
					func(r *rand.Rand) interface{} { return r.Intn(500) },
					func(r *rand.Rand) interface{} { return r.Intn(1000) },
				},
			},
		},
		ErrorPatterns: []LogPattern{
			{
				Format:  LogFormatJSON,
				Pattern: `{"level":"error","ts":"%s","msg":"Database error","operation":"%s","table":"%s","error":"%s"}`,
				Args: []ArgGenerator{
					func(r *rand.Rand) interface{} { return queryTypes[r.Intn(len(queryTypes))] },
					func(r *rand.Rand) interface{} { return dbTables[r.Intn(len(dbTables))] },
					func(r *rand.Rand) interface{} { return dbErrors[r.Intn(len(dbErrors))] },
				},
			},
		},
		OTELResource: map[string]string{
			"service_name":    "mysql",
			"service_version": "8.0.28",
			"db_system":       "mysql",
			"db_version":      "8.0.28",
			"db_instance":     "primary",
			"db_cluster":      "production",
		},
	},
	{
		Name:      "cache",
		Component: "cache",
		LogPatterns: []LogPattern{
			{
				Format:  LogFormatJSON,
				Pattern: `{"level":"%s","ts":"%s","msg":"Cache operation","operation":"%s","key":"%s","size":%d,"ttl":%d}`,
				Args: []ArgGenerator{
					func(r *rand.Rand) interface{} { return cacheOps[r.Intn(len(cacheOps))] },
					func(r *rand.Rand) interface{} { return fmt.Sprintf("key-%d", r.Intn(1000)) },
					func(r *rand.Rand) interface{} { return r.Intn(10000) },
					func(r *rand.Rand) interface{} { return r.Intn(3600) },
				},
			},
		},
		ErrorPatterns: []LogPattern{
			{
				Format:  LogFormatJSON,
				Pattern: `{"level":"error","ts":"%s","msg":"Cache error","operation":"%s","error":"%s"}`,
				Args: []ArgGenerator{
					func(r *rand.Rand) interface{} { return cacheOps[r.Intn(len(cacheOps))] },
					func(r *rand.Rand) interface{} { return cacheErrors[r.Intn(len(cacheErrors))] },
				},
			},
		},
		OTELResource: map[string]string{
			"service_name":     "redis",
			"service_version":  "6.2.6",
			"redis_cluster":    "false",
			"redis_db":         "0",
			"redis_max_memory": "17179869184",
		},
	},
	{
		Name:      "auth-service",
		Component: "auth",
		LogPatterns: []LogPattern{
			{
				Format:  LogFormatJSON,
				Pattern: `{"level":"%s","ts":"%s","msg":"Auth event","action":"%s","user":"%s","success":%t,"source_ip":"%s"}`,
				Args: []ArgGenerator{
					func(r *rand.Rand) interface{} { return authActions[r.Intn(len(authActions))] },
					func(r *rand.Rand) interface{} { return fmt.Sprintf("user-%d", r.Intn(1000)) },
					func(r *rand.Rand) interface{} { return r.Float32() > 0.1 }, // 90% success rate
					func(r *rand.Rand) interface{} { return randomIP(r) },
				},
			},
		},
		ErrorPatterns: []LogPattern{
			{
				Format:  LogFormatJSON,
				Pattern: `{"level":"error","ts":"%s","msg":"Authentication failed","user":"%s","reason":"%s","source_ip":"%s"}`,
				Args: []ArgGenerator{
					func(r *rand.Rand) interface{} { return fmt.Sprintf("user-%d", r.Intn(1000)) },
					func(r *rand.Rand) interface{} { return authErrors[r.Intn(len(authErrors))] },
					func(r *rand.Rand) interface{} { return randomIP(r) },
				},
			},
		},
		OTELResource: map[string]string{
			"service_name":           "auth-service",
			"service_version":        "1.0.0",
			"service_namespace":      "default",
			"telemetry_sdk_name":     "opentelemetry",
			"telemetry_sdk_language": "go",
			"deployment_environment": "production",
		},
	},
}

var (
	httpMethods = []string{"GET", "POST", "PUT", "DELETE", "PATCH"}
	apiPaths    = []string{
		"/api/v1/users",
		"/api/v1/products",
		"/api/v1/orders",
		"/api/v1/auth/login",
		"/api/v1/auth/logout",
		"/api/v2/metrics",
		"/api/v2/logs",
		"/healthz",
		"/metrics",
	}
	userAgents = []string{
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36",
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36",
		"Mozilla/5.0 (iPhone; CPU iPhone OS 14_7_1 like Mac OS X) AppleWebKit/605.1.15",
		"Mozilla/5.0 (Linux; Android 11; SM-G991B) AppleWebKit/537.36",
		"curl/7.64.1",
		"Apache-HttpClient/4.5.13",
		"python-requests/2.26.0",
	}
	queryTypes  = []string{"SELECT", "INSERT", "UPDATE", "DELETE", "MERGE"}
	dbTables    = []string{"users", "products", "orders", "sessions", "logs", "metrics"}
	cacheOps    = []string{"get", "set", "delete", "expire", "flush"}
	authActions = []string{"login", "logout", "password_reset", "token_refresh", "permission_check"}

	errorMessages = []string{
		"Invalid request parameters",
		"Unauthorized access",
		"Resource not found",
		"Internal server error",
		"Service unavailable",
		"Rate limit exceeded",
		"Invalid content type",
	}
	dbErrors = []string{
		"Connection refused",
		"Deadlock detected",
		"Unique constraint violation",
		"Foreign key constraint violation",
		"Query timeout",
		"Table does not exist",
	}
	cacheErrors = []string{
		"Connection refused",
		"Key not found",
		"Invalid key format",
		"Memory limit exceeded",
		"Serialization error",
	}
	authErrors = []string{
		"Invalid credentials",
		"Account locked",
		"Session expired",
		"Invalid token",
		"Too many attempts",
		"Password expired",
	}

	nginxPaths = []string{
		"/",
		"/api/",
		"/static/",
		"/images/",
		"/css/",
		"/js/",
		"/upload/",
		"/download/",
		"/admin/",
		"/auth/",
	}

	nginxErrorTypes = []string{
		"access forbidden",
		"client closed connection",
		"upstream timed out",
		"file not found",
		"invalid request",
	}

	nginxErrors = []string{
		"access forbidden by rule",
		"client closed connection while reading request headers",
		"upstream timed out (110: Connection timed out)",
		"file not found",
		"client sent invalid request",
	}

	mysqlUsers = []string{
		"app",
		"readonly",
		"admin",
		"repl",
		"backup",
	}

	mysqlErrorCodes = []int{
		1045, // Access denied
		1049, // Unknown database
		1146, // Table doesn't exist
		1213, // Deadlock
		1040, // Too many connections
	}

	mysqlErrors = []string{
		"Access denied for user",
		"Unknown database",
		"Table doesn't exist",
		"Deadlock found when trying to get lock",
		"Too many connections",
	}

	kafkaTopics = []string{
		"users",
		"orders",
		"payments",
		"notifications",
		"logs",
		"metrics",
		"events",
	}

	kafkaEvents = []string{
		"producer_send",
		"consumer_fetch",
		"partition_assignment",
		"replication_completed",
		"leader_election",
	}

	kafkaErrors = []string{
		"Leader not available",
		"Network connection failure",
		"Topic authorization failed",
		"Record too large",
		"Offset out of range",
	}
)

// generateLogLine creates a log line using application-specific patterns
func generateLogLine(format LogFormat, level string, rnd *rand.Rand) string {
	// Select a random application
	app := defaultApplications[rnd.Intn(len(defaultApplications))]

	// Determine if this should be an error log
	isError := level == "error"

	// Filter patterns by format and error status
	var validPatterns []LogPattern
	if isError {
		for _, p := range app.ErrorPatterns {
			if p.Format == format {
				validPatterns = append(validPatterns, p)
			}
		}
	} else {
		for _, p := range app.LogPatterns {
			if p.Format == format {
				validPatterns = append(validPatterns, p)
			}
		}
	}

	// If no matching patterns found, use default format
	if len(validPatterns) == 0 {
		return fmt.Sprintf(`{"level":"%s","ts":"%s","msg":"Default log message","app":"%s"}`,
			level,
			time.Now().Format(time.RFC3339),
			app.Name,
		)
	}

	// Select random pattern from matching ones
	pattern := validPatterns[rnd.Intn(len(validPatterns))]

	// Generate arguments for the pattern
	args := make([]interface{}, 0, len(pattern.Args)+2)
	args = append(args, level, time.Now().Format(time.RFC3339))
	for _, argGen := range pattern.Args {
		args = append(args, argGen(rnd))
	}

	// Format the log line
	return fmt.Sprintf(pattern.Pattern, args...)
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

func generateMySQLQuery(rnd *rand.Rand) string {
	queries := []string{
		"SELECT * FROM users WHERE id = %d",
		"UPDATE orders SET status = 'completed' WHERE order_id = %d",
		"INSERT INTO logs (timestamp, level, message) VALUES (NOW(), 'info', 'test')",
		"DELETE FROM sessions WHERE expires < NOW()",
		"SELECT COUNT(*) FROM products WHERE category = 'electronics'",
	}
	query := queries[rnd.Intn(len(queries))]
	if strings.Contains(query, "%d") {
		query = fmt.Sprintf(query, rnd.Intn(10000))
	}
	return query
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

func (c *GeneratorConfig) buildLabelSelector(matchers []labelMatcher) string {
	var parts []string
	for _, m := range matchers {
		parts = append(parts, fmt.Sprintf(`%s="%s"`, m.name, m.value))
	}
	return "{" + strings.Join(parts, ", ") + "}"
}

func (c *GeneratorConfig) generateLabelCombinations() [][]labelMatcher {
	rnd := c.NewRand()

	// Helper to create a matcher with random value
	randomMatcher := func(name string, values []string) labelMatcher {
		return labelMatcher{name: name, value: values[rnd.Intn(len(values))]}
	}

	combinations := [][]labelMatcher{
		// Single label matchers
		{randomMatcher("component", c.LabelConfig.Components)},
		{randomMatcher("region", c.LabelConfig.Regions)},

		// Two label combinations
		{
			randomMatcher("region", c.LabelConfig.Regions),
			randomMatcher("env", c.LabelConfig.EnvTypes),
		},

		// Three label combinations
		{
			randomMatcher("component", c.LabelConfig.Components),
			randomMatcher("env", c.LabelConfig.EnvTypes),
			randomMatcher("region", c.LabelConfig.Regions),
		},
	}

	return combinations
}

// GenerateTestCases creates a sorted list of test cases using the configuration
func (c *GeneratorConfig) GenerateTestCases() []TestCase {
	var cases []TestCase
	labelCombos := c.generateLabelCombinations()
	end := c.StartTime.Add(c.TimeSpread)

	// Use a map to track unique queries
	uniqueQueries := make(map[string]struct{})

	// Helper to add both forward and backward variants if query is unique
	addBidirectional := func(query string, start, end time.Time) {
		if _, exists := uniqueQueries[query]; exists {
			return // Skip duplicate queries
		}
		uniqueQueries[query] = struct{}{}
		cases = append(cases,
			TestCase{
				Query:     query,
				Start:     start,
				End:       end,
				Direction: logproto.FORWARD,
			},
			TestCase{
				Query:     query,
				Start:     start,
				End:       end,
				Direction: logproto.BACKWARD,
			},
		)
	}

	// Helper to add metric query if unique
	addMetricQuery := func(query string, start, end time.Time, step time.Duration) {
		if _, exists := uniqueQueries[query]; exists {
			return // Skip duplicate queries
		}
		uniqueQueries[query] = struct{}{}
		cases = append(cases,
			TestCase{
				Query:     query,
				Start:     start,
				End:       end,
				Direction: logproto.FORWARD,
				Step:      step,
			},
		)
	}

	// Calculate step size to get ~20 points over the time range
	step := c.TimeSpread / 19

	// Basic label selector queries with line filters and structured metadata
	for _, combo := range labelCombos {
		selector := c.buildLabelSelector(combo)

		// Basic selector
		addBidirectional(selector, c.StartTime, end)

		// With structured metadata filters
		addBidirectional(selector+` | detected_level="error"`, c.StartTime, end)
		addBidirectional(selector+` | detected_level="warn"`, c.StartTime, end)

		// Combined filters
		addBidirectional(selector+` |~ "error|exception" | detected_level="error"`, c.StartTime, end)
		addBidirectional(selector+` | json | duration_seconds > 0.1 | detected_level!="debug"`, c.StartTime, end)
		addBidirectional(selector+` | logfmt | level="error" | detected_level="error"`, c.StartTime, end)

		// Metric queries with structured metadata
		baseMetricQuery := fmt.Sprintf(`rate(%s | detected_level=~"error|warn" [5m])`, selector)

		// Single dimension aggregations
		dimensions := []string{"pod", "namespace", "env"}
		for _, dim := range dimensions {
			query := fmt.Sprintf(`sum by (%s) (%s)`, dim, baseMetricQuery)
			addMetricQuery(query, c.StartTime.Add(5*time.Minute), end, step)
		}

		// Two dimension aggregations
		twoDimCombos := [][]string{
			{"cluster", "namespace"},
			{"env", "component"},
		}
		for _, dims := range twoDimCombos {
			query := fmt.Sprintf(`sum by (%s, %s) (%s)`, dims[0], dims[1], baseMetricQuery)
			addMetricQuery(query, c.StartTime.Add(5*time.Minute), end, step)
		}

		// Error rates by severity
		errorQueries := []string{
			fmt.Sprintf(`sum by (component) (rate(%s | detected_level="error" [5m]))`, selector),
			fmt.Sprintf(`sum by (component) (rate(%s | detected_level="warn" [5m]))`, selector),
			fmt.Sprintf(`sum by (component, detected_level) (rate(%s | detected_level=~"error|warn" [5m]))`, selector),
		}
		for _, query := range errorQueries {
			addMetricQuery(query, c.StartTime.Add(5*time.Minute), end, step)
		}
	}

	// Dense period queries
	for _, interval := range c.DenseIntervals {
		combo := labelCombos[c.NewRand().Intn(len(labelCombos))]
		selector := c.buildLabelSelector(combo)
		addBidirectional(
			selector+` | detected_level=~"error|warn"`,
			interval.Start,
			interval.Start.Add(interval.Duration),
		)

		// Add metric queries for dense periods
		rateQuery := fmt.Sprintf(`sum by (component, detected_level) (rate(%s[1m]))`, selector)
		addMetricQuery(
			rateQuery,
			interval.Start,
			interval.Start.Add(interval.Duration),
			interval.Duration/19,
		)
	}

	// Sort test cases by name for consistent ordering
	sort.Slice(cases, func(i, j int) bool {
		return cases[i].Name() < cases[j].Name()
	})

	return cases
}
