package bench

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/grafana/loki/v3/pkg/logproto"
)

// QueryDirection specifies which direction(s) to run a log query
type QueryDirection string

const (
	DirectionForward  QueryDirection = "forward"
	DirectionBackward QueryDirection = "backward"
	DirectionBoth     QueryDirection = "both"
)

// TimeRangeConfig defines the time range parameters for a query
type TimeRangeConfig struct {
	// Length is the duration the query should cover (end - start)
	Length string `yaml:"length"`
	// Step is the step size for metric queries
	Step string `yaml:"step,omitempty"`
}

// ParseLength parses the length string into a time.Duration
func (t *TimeRangeConfig) ParseLength() (time.Duration, error) {
	return time.ParseDuration(t.Length)
}

// ParseStep parses the step string into a time.Duration
func (t *TimeRangeConfig) ParseStep() (time.Duration, error) {
	if t.Step == "" {
		return 0, nil
	}
	return time.ParseDuration(t.Step)
}

// QueryRequirements specifies what characteristics a stream must have for a query to work
type QueryRequirements struct {
	LogFormat          string   `yaml:"log_format,omitempty"`          // "json", "logfmt", or "unstructured"
	UnwrappableFields  []string `yaml:"unwrappable_fields,omitempty"`  // Numeric fields needed for | unwrap
	StructuredMetadata []string `yaml:"structured_metadata,omitempty"` // Structured metadata keys needed
	DetectedFields     []string `yaml:"detected_fields,omitempty"`     // Fields that must be parsable from log content (for aggregations/filters)
	Labels             []string `yaml:"labels,omitempty"`              // Stream label keys needed (for selectors, filters, or grouping)
	Keywords           []string `yaml:"keywords,omitempty"`            // Strings that must appear in log content
}

// QueryDefinition represents a single query definition from the registry
type QueryDefinition struct {
	Description string            `yaml:"description"`
	Query       string            `yaml:"query"`
	Kind        string            `yaml:"kind,omitempty"` // "log" or "metric"
	Skip        bool              `yaml:"skip,omitempty"`
	TimeRange   TimeRangeConfig   `yaml:"time_range"`
	Directions  QueryDirection    `yaml:"directions,omitempty"`
	Requires    QueryRequirements `yaml:"requires,omitempty"` // Requirements for stream selection
	Tags        []string          `yaml:"tags,omitempty"`
	Notes       string            `yaml:"notes,omitempty"`
	Source      string            `yaml:"-"` // Source location (suite/file.yaml:line), populated during load
}

// QueryFile represents a YAML file containing query definitions
type QueryFile struct {
	Queries []QueryDefinition `yaml:"queries"`
}

// Suite represents a test suite level
type Suite string

const (
	SuiteFast       Suite = "fast"
	SuiteRegression Suite = "regression"
	SuiteExhaustive Suite = "exhaustive"
)

// QueryRegistry manages loading and accessing query definitions
type QueryRegistry struct {
	baseDir string
	queries map[Suite][]QueryDefinition
}

// NewQueryRegistry creates a new query registry
func NewQueryRegistry(baseDir string) *QueryRegistry {
	return &QueryRegistry{
		baseDir: baseDir,
		queries: make(map[Suite][]QueryDefinition),
	}
}

// Load loads all query definitions for the specified suites
func (r *QueryRegistry) Load(suites ...Suite) error {
	for _, suite := range suites {
		suiteDir := filepath.Join(r.baseDir, string(suite))

		// Check if directory exists
		if _, err := os.Stat(suiteDir); os.IsNotExist(err) {
			return fmt.Errorf("suite directory does not exist: %s", suiteDir)
		}

		// Read all YAML files in the suite directory
		entries, err := os.ReadDir(suiteDir)
		if err != nil {
			return fmt.Errorf("failed to read suite directory %s: %w", suiteDir, err)
		}

		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}

			// Only process YAML files
			if !strings.HasSuffix(entry.Name(), ".yaml") && !strings.HasSuffix(entry.Name(), ".yml") {
				continue
			}

			filePath := filepath.Join(suiteDir, entry.Name())
			queries, err := r.loadFile(filePath, suite, entry.Name())
			if err != nil {
				return fmt.Errorf("failed to load file %s: %w", filePath, err)
			}

			for _, query := range queries {
				if query.Skip {
					continue
				}

				r.queries[suite] = append(r.queries[suite], query)
			}
		}
	}

	return nil
}

// validateRequirements validates query requirements against bounded sets defined in metadata.go
func validateRequirements(req QueryRequirements, queryDesc string) error {
	// Validate unwrappable fields
	for _, field := range req.UnwrappableFields {
		if !slices.Contains(unwrappableFields, field) {
			return fmt.Errorf("query %q: unwrappable field %q not in bounded set %v", queryDesc, field, unwrappableFields)
		}
	}

	// Validate labels
	for _, label := range req.Labels {
		if !slices.Contains(labelKeys, label) {
			return fmt.Errorf("query %q: label %q not in bounded set %v", queryDesc, label, labelKeys)
		}
	}

	// Validate keywords
	for _, keyword := range req.Keywords {
		if !slices.Contains(filterableKeywords, keyword) {
			return fmt.Errorf("query %q: keyword %q not in bounded set %v", queryDesc, keyword, filterableKeywords)
		}
	}

	// Validate structured metadata
	for _, key := range req.StructuredMetadata {
		if !slices.Contains(structuredMetadataKeys, key) {
			return fmt.Errorf("query %q: structured metadata key %q not in bounded set %v", queryDesc, key, structuredMetadataKeys)
		}
	}

	return nil
}

// loadFile loads query definitions from a single YAML file
func (r *QueryRegistry) loadFile(filePath string, suite Suite, fileName string) ([]QueryDefinition, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	// First parse to get line numbers
	var rootNode yaml.Node
	if err := yaml.Unmarshal(data, &rootNode); err != nil {
		return nil, fmt.Errorf("failed to parse YAML for line numbers: %w", err)
	}

	// Then parse normally to get the data
	var queryFile QueryFile
	if err := yaml.Unmarshal(data, &queryFile); err != nil {
		return nil, fmt.Errorf("failed to parse YAML: %w", err)
	}

	// Extract line numbers from the YAML node tree
	// The structure is: Document -> Mapping -> "queries" key -> Sequence -> Query items
	lineNumbers := extractQueryLineNumbers(&rootNode)

	// Set defaults and validate
	for i := range queryFile.Queries {
		q := &queryFile.Queries[i]

		// Set source location
		lineNum := 0
		if i < len(lineNumbers) {
			lineNum = lineNumbers[i]
		}
		q.Source = fmt.Sprintf("%s/%s:%d", suite, fileName, lineNum)

		// Default directions to "both" for log queries
		if q.Directions == "" && q.Kind == "log" {
			q.Directions = DirectionBoth
		}

		if q.Kind == "" || (q.Kind != "metric" && q.Kind != "log") {
			q.Kind = "log"
		}

		// Validate time range
		if q.TimeRange.Length == "" {
			return nil, fmt.Errorf("time_range.length is required for query %q", q.Description)
		}

		if _, err := q.TimeRange.ParseLength(); err != nil {
			return nil, fmt.Errorf("invalid time_range.length %q for query %q: %w", q.TimeRange.Length, q.Description, err)
		}

		if q.TimeRange.Step != "" {
			if _, err := q.TimeRange.ParseStep(); err != nil {
				return nil, fmt.Errorf("invalid time_range.step %q for query %q: %w", q.TimeRange.Step, q.Description, err)
			}
		}

		// Validate requirements against bounded sets
		if err := validateRequirements(q.Requires, q.Description); err != nil {
			return nil, err
		}
	}

	return queryFile.Queries, nil
}

// extractQueryLineNumbers extracts line numbers for each query in the YAML
func extractQueryLineNumbers(rootNode *yaml.Node) []int {
	var lineNumbers []int

	// Navigate the YAML tree: Document -> Mapping -> find "queries" key
	if rootNode.Kind != yaml.DocumentNode || len(rootNode.Content) == 0 {
		return lineNumbers
	}

	docNode := rootNode.Content[0]
	if docNode.Kind != yaml.MappingNode {
		return lineNumbers
	}

	// Find the "queries" key in the mapping
	for i := 0; i < len(docNode.Content); i += 2 {
		keyNode := docNode.Content[i]
		if keyNode.Value == "queries" && i+1 < len(docNode.Content) {
			valueNode := docNode.Content[i+1]
			if valueNode.Kind == yaml.SequenceNode {
				// Each item in the sequence is a query
				for _, queryNode := range valueNode.Content {
					lineNumbers = append(lineNumbers, queryNode.Line)
				}
			}
			break
		}
	}

	return lineNumbers
}

// GetQueries returns all loaded queries for the specified suites
// If suites is empty, returns all queries
func (r *QueryRegistry) GetQueries(suites ...Suite) []QueryDefinition {
	if len(suites) == 0 {
		// Return all queries
		var all []QueryDefinition
		for _, queries := range r.queries {
			all = append(all, queries...)
		}
		return all
	}

	var result []QueryDefinition
	for _, suite := range suites {
		result = append(result, r.queries[suite]...)
	}
	return result
}

// ExpandQuery expands a query definition into one or more TestCase instances
// by resolving variables and creating cases for each direction
func (r *QueryRegistry) ExpandQuery(def QueryDefinition, resolver VariableResolver, isInstant bool) ([]TestCase, error) {
	resolvedQuery, err := resolver.ResolveQuery(def.Query, def.Requires, isInstant)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve query: %w", err)
	}

	length, err := def.TimeRange.ParseLength()
	if err != nil {
		return nil, fmt.Errorf("failed to parse length: %w", err)
	}

	step, err := def.TimeRange.ParseStep()
	if err != nil {
		return nil, fmt.Errorf("failed to parse step: %w", err)
	}

	start, end, err := resolver.GetTimeRange(length)
	if err != nil {
		return nil, fmt.Errorf("failed to get time range: %w", err)
	}

	// Create test cases based on query kind and directions
	var cases []TestCase

	if def.Kind == "metric" {
		// Metric queries only run in forward direction
		tc := TestCase{
			Query:     resolvedQuery,
			Start:     start,
			End:       end,
			Direction: logproto.FORWARD,
			Step:      step,
			Source:    def.Source,
		}
		cases = append(cases, tc)
	} else {
		// Log queries can run in both directions
		switch def.Directions {
		case DirectionForward:
			cases = append(cases, TestCase{
				Query:     resolvedQuery,
				Start:     start,
				End:       end,
				Direction: logproto.FORWARD,
				Source:    def.Source,
			})
		case DirectionBackward:
			cases = append(cases, TestCase{
				Query:     resolvedQuery,
				Start:     start,
				End:       end,
				Direction: logproto.BACKWARD,
				Source:    def.Source,
			})
		case DirectionBoth:
			cases = append(cases,
				TestCase{
					Query:     resolvedQuery,
					Start:     start,
					End:       end,
					Direction: logproto.FORWARD,
					Source:    def.Source,
				},
				TestCase{
					Query:     resolvedQuery,
					Start:     start,
					End:       end,
					Direction: logproto.BACKWARD,
					Source:    def.Source,
				},
			)
		}
	}

	return cases, nil
}

// VariableResolver is responsible for resolving query variables
type VariableResolver interface {
	// ResolveQuery resolves variables in a query based on requirements
	// The isInstant parameter determines whether to use MinInstantRange (true) or MinRange (false) for ${RANGE}
	ResolveQuery(query string, requirements QueryRequirements, isInstant bool) (string, error)

	// GetTimeRange returns the start and end time for a query based on its time range config
	GetTimeRange(length time.Duration) (start, end time.Time, err error)
}
