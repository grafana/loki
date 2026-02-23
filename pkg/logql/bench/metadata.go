package bench

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	promql_parser "github.com/prometheus/prometheus/promql/parser"
)

const (
	metadataFileName = "dataset_metadata.json"
	metadataVersion  = "1.0"

	// Application names used in metadata processing
	appDatabase = "database"
	appLoki     = "loki"
)

// Bounded sets: characteristics common to all datasets
// These define the set of possible queries
var (
	// unwrappableFields are numeric fields that can be used with | unwrap
	// Mapped from applications:
	unwrappableFields = []string{
		"bytes",
		"duration",
		"rows_affected",
		"size",
		"spans",
		"status",
		"streams",
		"ttl",
	}

	// filterableKeywords are strings that commonly appear in log content
	// Used for line filter queries like |= "level" or |~ "error"
	filterableKeywords = []string{
		"DEBUG",
		"ERROR",
		"INFO",
		"WARN",
		"debug",
		"duration",
		"error",
		"failed",
		"info",
		"level",
		"query",
		"refused",
		"status",
		"success",
		"warn",
	}

	// structuredMetadataKeys are keys used in structured metadata
	// These appear as structured_metadata in log entries
	structuredMetadataKeys = []string{
		"detected_level",
		"pod",
		"span_id",
		"trace_id",
	}

	// labelKeys are indexed or parsed labels
	// Used for label selectors and/or grouping (by/without)
	labelKeys = []string{
		"cluster",
		"component",
		"detected_level",
		"env",
		"level",
		"namespace",
		"pod",
		"query_type",
		"region",
		"service_name",
		"status",
	}
)

// SerializableStreamMetadata contains only the fields needed for query resolution
// This is a subset of StreamMetadata that can be safely serialized to JSON
type SerializableStreamMetadata struct {
	MinRange        time.Duration `json:"min_range"`
	MinInstantRange time.Duration `json:"min_instant_range"`
}

// DatasetMetadata contains queryable information about a generated dataset
// It maps query properties to stream/label patterns
type DatasetMetadata struct {
	AllSelectors  []string               `json:"all_selectors"`
	ByFormat      map[LogFormat][]string `json:"by_format"`
	ByServiceName map[string][]string    `json:"by_service_name"`
	Statistics    DatasetStatistics      `json:"statistics"`
	TimeRange     TimeRange              `json:"time_range"`
	Version       string                 `json:"version"`

	ByUnwrappableField   map[string][]string                    `json:"by_unwrappable_field"`   // field name -> selectors with that field
	ByDetectedField      map[string][]string                    `json:"by_detected_field"`      // parsable field -> selectors with that field
	ByStructuredMetadata map[string][]string                    `json:"by_structured_metadata"` // metadata key -> selectors with that key
	ByLabelKey           map[string][]string                    `json:"by_label_key"`           // label name -> selectors with that label
	ByKeyword            map[string][]string                    `json:"by_keyword"`             // keyword -> selectors containing that keyword
	MetadataBySelector   map[string]*SerializableStreamMetadata `json:"metadata_by_selector"`   // selector -> stream metadata for range resolution
}

// TimeRange represents the temporal bounds of a dataset
type TimeRange struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

// DatasetStatistics provides aggregate information about the dataset
type DatasetStatistics struct {
	Generated        time.Time         `json:"generated"`
	StreamsByFormat  map[LogFormat]int `json:"streams_by_format"`
	StreamsByService map[string]int    `json:"streams_by_service"`
	TotalStreams     int               `json:"total_streams"`
	UniqueLabels     map[string]int    `json:"unique_labels"` // label name -> unique value count
}

// CalculateMinRanges computes the minimum range durations needed for range and instant queries
// based on the log generation rate for a stream.
//
// MinRange: For range queries that sample multiple points over time
// - Formula: time needed for 5 samples (with 10-100 logs per sample)
//
// MinInstantRange: For instant queries that sample at a single point in time
// - Formula: time needed for 10 logs (to ensure capture at single sampling point)
//
// TODO: This function is specific to the generated data.
// We will need to expand on this concept when parsing real datasets.
// For the generated data, the algorithm is based on the following:
// - baseEntries: 10-100 entries per stream (average ~55)
// - timeSpread: configured time span over which logs are distributed
// - logRate: baseEntries / timeSpread (logs per second)
func CalculateMinRanges(config *GeneratorConfig) (minRange, minInstantRange time.Duration) {
	// Calculate average log generation rate
	// baseEntries averages to ~55 logs per stream over the timeSpread
	avgBaseEntries := 55.0
	logRatePerSecond := avgBaseEntries / config.TimeSpread.Seconds()

	// MinRange: time needed for 5 logs (conservative for range queries with multiple samples)
	if logRatePerSecond > 0 {
		minRange = time.Duration(5.0 / logRatePerSecond * float64(time.Second))
		minRange = minRange.Round(time.Second) // Round to whole seconds for valid LogQL ranges
	}

	// Apply minimum and maximum thresholds for MinRange
	if minRange < 1*time.Minute {
		minRange = 1 * time.Minute
	}
	if minRange > 15*time.Minute {
		minRange = 15 * time.Minute
	}

	// MinInstantRange: time needed for 10 logs (instant query needs more lookback)
	if logRatePerSecond > 0 {
		minInstantRange = time.Duration(10.0 / logRatePerSecond * float64(time.Second))
		minInstantRange = minInstantRange.Round(time.Second) // Round to whole seconds for valid LogQL ranges
	}

	// Apply minimum and maximum thresholds for MinInstantRange
	if minInstantRange < 1*time.Minute {
		minInstantRange = 1 * time.Minute
	}
	if minInstantRange > 30*time.Minute {
		minInstantRange = 30 * time.Minute
	}

	return minRange, minInstantRange
}

// BuildMetadata constructs dataset metadata from stream metadata
// This is called during data generation to create the metadata file
//
// NOTE: Currently uses helper functions (getUnwrappableFields, etc.) that hardcode
// application knowledge. This is a temporary approach for synthetic datasets.
// See TODO comments on helper functions for refactoring plan to support real datasets.
func BuildMetadata(config *GeneratorConfig, streamsMeta []StreamMetadata) *DatasetMetadata {
	metadata := &DatasetMetadata{
		AllSelectors:  make([]string, 0, len(streamsMeta)),
		ByFormat:      make(map[LogFormat][]string),
		ByServiceName: make(map[string][]string),
		Version:       metadataVersion,

		ByUnwrappableField:   make(map[string][]string),
		ByDetectedField:      make(map[string][]string),
		ByStructuredMetadata: make(map[string][]string),
		ByLabelKey:           make(map[string][]string),
		ByKeyword:            make(map[string][]string),
		MetadataBySelector:   make(map[string]*SerializableStreamMetadata),

		Statistics: DatasetStatistics{
			TotalStreams:     len(streamsMeta),
			StreamsByFormat:  make(map[LogFormat]int),
			StreamsByService: make(map[string]int),
			UniqueLabels:     make(map[string]int),
			Generated:        time.Now(),
		},
	}

	metadata.TimeRange = TimeRange{
		Start: config.StartTime,
		End:   config.StartTime.Add(config.TimeSpread),
	}

	// Track unique values per label for statistics
	uniqueValues := make(map[string]map[string]struct{})

	// Build indexes from stream metadata
	for _, stream := range streamsMeta {
		selector := stream.Labels

		metadata.AllSelectors = append(metadata.AllSelectors, selector)
		metadata.ByFormat[stream.Format] = append(metadata.ByFormat[stream.Format], selector)
		metadata.ByServiceName[stream.Service.Name] = append(metadata.ByServiceName[stream.Service.Name], selector)
		metadata.Statistics.StreamsByFormat[stream.Format]++
		metadata.Statistics.StreamsByService[stream.Service.Name]++

		// Store stream metadata by selector for range resolution
		metadata.MetadataBySelector[selector] = &SerializableStreamMetadata{
			MinRange:        stream.MinRange,
			MinInstantRange: stream.MinInstantRange,
		}

		fields := getUnwrappableFields(stream.Service.Name)
		for _, field := range fields {
			metadata.ByUnwrappableField[field] = append(metadata.ByUnwrappableField[field], selector)
		}

		detectedFields := getDetectedFields(stream.Service.Name, stream.Format)
		for _, field := range detectedFields {
			metadata.ByDetectedField[field] = append(metadata.ByDetectedField[field], selector)
		}

		structuredMetadata := getStructuredMetadataKeys(stream.Service.Name)
		for _, key := range structuredMetadata {
			metadata.ByStructuredMetadata[key] = append(metadata.ByStructuredMetadata[key], selector)
		}

		keywords := getContentKeywords(stream.Service.Name)
		for _, keyword := range keywords {
			metadata.ByKeyword[keyword] = append(metadata.ByKeyword[keyword], selector)
		}

		lbls, err := promql_parser.NewParser(promql_parser.Options{}).ParseMetric(selector)
		if err != nil {
			continue // Skip this stream if we can't parse labels
		}

		lbls.Range(func(label labels.Label) {
			if uniqueValues[label.Name] == nil {
				uniqueValues[label.Name] = make(map[string]struct{})
			}
			uniqueValues[label.Name][label.Value] = struct{}{}

			metadata.ByLabelKey[label.Name] = append(metadata.ByLabelKey[label.Name], selector)
		})
	}

	// Calculate unique label cardinalities
	for name, values := range uniqueValues {
		metadata.Statistics.UniqueLabels[name] = len(values)
	}

	// Sort all selector arrays for determinism
	sort.Strings(metadata.AllSelectors)
	for format := range metadata.ByFormat {
		sort.Strings(metadata.ByFormat[format])
	}
	for app := range metadata.ByServiceName {
		sort.Strings(metadata.ByServiceName[app])
	}

	for field := range metadata.ByUnwrappableField {
		sort.Strings(metadata.ByUnwrappableField[field])
	}
	for field := range metadata.ByDetectedField {
		sort.Strings(metadata.ByDetectedField[field])
	}
	for key := range metadata.ByStructuredMetadata {
		sort.Strings(metadata.ByStructuredMetadata[key])
	}
	for label := range metadata.ByLabelKey {
		sort.Strings(metadata.ByLabelKey[label])
	}
	for keyword := range metadata.ByKeyword {
		sort.Strings(metadata.ByKeyword[keyword])
	}

	return metadata
}

// TODO: These helper functions violate separation of concerns.
// The generator knows about applications, not the metadata builder.
// For synthetic datasets, this information should be passed from generator to BuildMetadata.
// For real datasets (future use case), this would need to be computed by analyzing actual log data.
// Consider refactoring to:
//   1. Add fields to StreamMetadata: UnwrappableFields, StructuredMetadataKeys, Keywords
//   2. Generator populates these fields when creating StreamMetadata
//   3. BuildMetadata just indexes what's provided, doesn't need application knowledge
//   4. For real datasets, create a separate analysis tool that populates these fields

// getUnwrappableFields returns the numeric fields available for unwrap based on application name
func getUnwrappableFields(appName string) []string {
	switch appName {
	case appDatabase:
		return []string{"rows_affected", "duration"}
	case "web-server":
		return []string{"status", "duration"}
	case "cache":
		return []string{"size", "ttl"}
	case "kafka":
		return []string{"size"}
	case appLoki, "mimir":
		return []string{"duration", "streams", "bytes"}
	case "tempo":
		return []string{"duration", "spans", "bytes"}
	case "grafana":
		return []string{"duration", "status"}
	default:
		// Unknown application - return empty set
		return []string{}
	}
}

// getStructuredMetadataKeys returns the structured metadata keys for an application
func getStructuredMetadataKeys(appName string) []string {
	// For now, all applications have detected_level as structured metadata
	keys := []string{"detected_level"}

	switch appName {
	case appLoki, "mimir", "tempo", "grafana":
		keys = append(keys, "trace_id", "span_id")
	}

	return keys
}

// getDetectedFields returns fields that can be parsed/extracted from log content
// These fields are used in aggregations (sum by (level)) or filters (| level="error")
func getDetectedFields(appName string, format LogFormat) []string {
	switch format {
	case LogFormatJSON:
		// JSON format - fields are parsed via | json
		switch appName {
		case "web-server", appDatabase, "cache", "auth-service", "kafka", "prometheus":
			return []string{"level", "error", "msg"}
		case "kubernetes":
			// Kubernetes uses plain text format, not JSON
			return []string{}
		default:
			return []string{}
		}
	case LogFormatLogfmt:
		// Logfmt format - fields are parsed via | logfmt
		switch appName {
		case appLoki, "mimir", "tempo", "grafana":
			return []string{"level", "error", "msg", "query_type"}
		default:
			return []string{}
		}
	case LogFormatUnstructured:
		// Unstructured format - can't reliably parse structured fields
		return []string{}
	default:
		return []string{}
	}
}

// getContentKeywords returns keywords that appear in log content for an application
// These are literal strings that appear in the log text and can be used with line filters (|=, |~)
// Only returns keywords that are in the filterableKeywords bounded set
func getContentKeywords(appName string) []string {
	switch appName {
	case "web-server":
		// JSON: {"level":"...","error":"...","status":...,"duration":...,"msg":"HTTP request",...}
		return []string{"level", "error", "status", "duration"}
	case appDatabase:
		// JSON: {"level":"...","error":"...","msg":"Query executed","duration":...,"rows_affected":...,...}
		// Error messages include "Connection refused"
		return []string{"level", "error", "duration", "query", "refused"}
	case "cache":
		// JSON: {"level":"...","error":"...","msg":"Cache operation","size":...,"ttl":...,...}
		// Error messages include "Connection refused"
		return []string{"level", "error", "duration", "refused"}
	case "auth-service":
		// JSON: {"level":"...","error":"...","msg":"Authentication request","duration":"...","success":...,...}
		return []string{"level", "error", "duration", "success"}
	case "kafka":
		// JSON: {"level":"...","error":"...","msg":"Kafka event","topic":"...","size":...,...}
		// Error messages include "Topic authorization failed"
		return []string{"level", "error", "failed"}
	case "prometheus":
		// JSON: {"level":"...","error":"...","component":"...","msg":"...","duration":"...",...}
		return []string{"level", "error", "duration"}
	case "kubernetes":
		// Plain text: "2006-01-02T15:04:05.000000Z info [component] prefix: message"
		// Note: The word "level" does NOT appear - only the level values (info, error, warn, debug)
		return []string{"INFO", "ERROR", "WARN", "DEBUG", "info", "error", "warn", "debug"}
	case "mimir":
		// Logfmt: level=... msg=... error=... duration=...
		// Error format: error="failed to METHOD: message"
		return []string{"level", "error", "duration", "failed"}
	case appLoki:
		// Logfmt: level=... msg=... error=... duration=... streams=... bytes=...
		// Error format: error="failed to process request: message"
		return []string{"level", "error", "duration", "failed"}
	case "tempo":
		// Logfmt: level=... msg=... error=... duration=... spans=... bytes=...
		// Error format: error="failed to process trace: message"
		return []string{"level", "error", "duration", "failed"}
	case "grafana":
		// Logfmt: level=... msg=... error=... duration=... status=... method=...
		return []string{"level", "error", "duration", "status"}
	case "nginx":
		// Unstructured: "IP - user [timestamp] "METHOD /path HTTP/1.1" status size "referer" "user_agent""
		return []string{"status"}
	case "syslog":
		// Unstructured: "<priority>hostname systemd[pid]: message"
		return []string{}
	default:
		// Unknown application - return empty set to be conservative
		return []string{}
	}
}

// SaveMetadata writes metadata to a JSON file in the specified directory
func SaveMetadata(dataDir string, metadata *DatasetMetadata) error {
	metadataPath := filepath.Join(dataDir, metadataFileName)
	data, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	if err := os.WriteFile(metadataPath, data, 0o644); err != nil {
		return fmt.Errorf("failed to write metadata file: %w", err)
	}

	return nil
}

// LoadMetadata reads metadata from a JSON file in the specified directory
func LoadMetadata(dataDir string) (*DatasetMetadata, error) {
	metadataPath := filepath.Join(dataDir, metadataFileName)
	data, err := os.ReadFile(metadataPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read metadata file: %w", err)
	}

	var metadata DatasetMetadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		return nil, fmt.Errorf("failed to parse metadata: %w", err)
	}

	// Validate version
	if metadata.Version != metadataVersion {
		return nil, fmt.Errorf("unsupported metadata version %q (expected %q)", metadata.Version, metadataVersion)
	}

	return &metadata, nil
}

// GenerateInMemoryMetadata creates metadata in memory without file I/O
// This is useful for tests that need metadata but don't want to generate a full dataset
func GenerateInMemoryMetadata(config *GeneratorConfig) *DatasetMetadata {
	opt := DefaultOpt().
		WithStartTime(config.StartTime).
		WithTimeSpread(config.TimeSpread).
		WithLabelConfig(config.LabelConfig).
		WithNumStreams(config.NumStreams).
		WithSeed(config.Seed)

	gen := NewGenerator(opt)
	gen.generateStreamMetadata() // Generate stream metadata in memory
	return BuildMetadata(config, gen.StreamsMeta)
}
