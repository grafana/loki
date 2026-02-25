package bench

import (
	"math/rand"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// extractServiceName is a helper to extract service_name from a query for testing
func extractServiceName(query string) string {
	if strings.Contains(query, `service_name="loki"`) {
		return "loki"
	}
	if strings.Contains(query, `service_name="database"`) {
		return "database"
	}
	if strings.Contains(query, `service_name="web-server"`) {
		return "web-server"
	}
	return ""
}

func TestMetadataVariableResolver_ResolveQuery(t *testing.T) {
	// Create test metadata with known selectors and capabilities
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)

	metadata := &DatasetMetadata{
		TimeRange: TimeRange{
			Start: start,
			End:   end,
		},
		AllSelectors: []string{
			`{service_name="database"}`,
			`{service_name="loki"}`,
			`{service_name="web-server"}`,
			`{region="us-west-2"}`,
		},
		ByFormat: map[LogFormat][]string{
			LogFormatJSON: {
				`{service_name="database"}`,
				`{service_name="web-server"}`,
			},
			LogFormatLogfmt: {
				`{service_name="loki"}`,
			},
		},
		ByUnwrappableField: map[string][]string{
			"rows_affected": {`{service_name="database"}`},
			"duration":      {`{service_name="loki"}`, `{service_name="web-server"}`},
			"status":        {`{service_name="web-server"}`},
		},
		ByLabelKey: map[string][]string{
			"pod":     {`{service_name="loki"}`, `{service_name="database"}`, `{service_name="web-server"}`},
			"cluster": {`{service_name="database"}`, `{service_name="web-server"}`},
			"level":   {`{service_name="loki"}`},
		},
		ByStructuredMetadata: map[string][]string{
			"trace_id": {`{service_name="loki"}`, `{service_name="database"}`},
			"span_id":  {`{service_name="loki"}`},
		},
		ByKeyword: map[string][]string{
			"error": {`{service_name="loki"}`, `{service_name="web-server"}`},
			"level": {`{service_name="loki"}`, `{service_name="database"}`},
		},
	}

	resolver := NewMetadataVariableResolver(metadata, 42)

	tests := []struct {
		name         string
		query        string
		requirements QueryRequirements
		wantErr      bool
		validate     func(t *testing.T, result string)
	}{
		{
			name:         "no selector variable - returns unchanged",
			query:        `{service_name="database"} | json`,
			requirements: QueryRequirements{},
			wantErr:      false,
			validate: func(t *testing.T, result string) {
				require.Equal(t, `{service_name="database"} | json`, result)
			},
		},
		{
			name:  "log format requirement only",
			query: `${SELECTOR} | json`,
			requirements: QueryRequirements{
				LogFormat: "json",
			},
			wantErr: false,
			validate: func(t *testing.T, result string) {
				// Should match one of the JSON format selectors
				require.Contains(t, []string{
					`{service_name="database"} | json`,
					`{service_name="web-server"} | json`,
				}, result)
			},
		},
		{
			name:  "unwrappable field requirement",
			query: `${SELECTOR} | json | unwrap rows_affected`,
			requirements: QueryRequirements{
				LogFormat:         "json",
				UnwrappableFields: []string{"rows_affected"},
			},
			wantErr: false,
			validate: func(t *testing.T, result string) {
				require.Equal(t, `{service_name="database"} | json | unwrap rows_affected`, result)
			},
		},
		{
			name:  "label requirement",
			query: `sum by (level) (${SELECTOR})`,
			requirements: QueryRequirements{
				LogFormat: "logfmt",
				Labels:    []string{"level"},
			},
			wantErr: false,
			validate: func(t *testing.T, result string) {
				require.Equal(t, `sum by (level) ({service_name="loki"})`, result)
			},
		},
		{
			name:  "multiple requirements - intersection",
			query: `${SELECTOR} | json | unwrap duration | pod != ""`,
			requirements: QueryRequirements{
				LogFormat:         "json",
				UnwrappableFields: []string{"duration"},
				Labels:            []string{"pod"},
			},
			wantErr: false,
			validate: func(t *testing.T, result string) {
				// web-server has json + duration + pod
				require.Contains(t, result, `{service_name="web-server"}`)
			},
		},
		{
			name:  "structured metadata requirement",
			query: `${SELECTOR} | trace_id != ""`,
			requirements: QueryRequirements{
				StructuredMetadata: []string{"trace_id"},
			},
			wantErr: false,
			validate: func(t *testing.T, result string) {
				require.Contains(t, []string{
					`{service_name="loki"} | trace_id != ""`,
					`{service_name="database"} | trace_id != ""`,
				}, result)
			},
		},
		{
			name:  "keyword requirement",
			query: `${SELECTOR} |= "error"`,
			requirements: QueryRequirements{
				Keywords: []string{"error"},
			},
			wantErr: false,
			validate: func(t *testing.T, result string) {
				require.Contains(t, []string{
					`{service_name="loki"} |= "error"`,
					`{service_name="web-server"} |= "error"`,
				}, result)
			},
		},
		{
			name:  "impossible requirements - no match",
			query: `${SELECTOR} | json | unwrap rows_affected`,
			requirements: QueryRequirements{
				LogFormat:         "logfmt", // database is JSON, not logfmt
				UnwrappableFields: []string{"rows_affected"},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := resolver.ResolveQuery(tt.query, tt.requirements, false)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			if tt.validate != nil {
				tt.validate(t, result)
			}
		})
	}
}

func TestMetadataVariableResolver_RangeVariable(t *testing.T) {
	// Create test metadata with known selectors and range values
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)

	metadata := &DatasetMetadata{
		TimeRange: TimeRange{
			Start: start,
			End:   end,
		},
		AllSelectors: []string{
			`{service_name="database"}`,
			`{service_name="loki"}`,
		},
		MetadataBySelector: map[string]*SerializableStreamMetadata{
			`{service_name="database"}`: {
				MinRange:        2 * time.Minute,
				MinInstantRange: 5 * time.Minute,
			},
			`{service_name="loki"}`: {
				MinRange:        3 * time.Minute,
				MinInstantRange: 7 * time.Minute,
			},
		},
		ByFormat: map[LogFormat][]string{
			LogFormatJSON: {
				`{service_name="database"}`,
			},
			LogFormatLogfmt: {
				`{service_name="loki"}`,
			},
		},
	}

	resolver := NewMetadataVariableResolver(metadata, 42)

	tests := []struct {
		name         string
		query        string
		requirements QueryRequirements
		isInstant    bool
		wantErr      bool
		validate     func(t *testing.T, result string)
	}{
		{
			name:  "range query with ${RANGE} - uses MinRange",
			query: `rate(${SELECTOR} [${RANGE}])`,
			requirements: QueryRequirements{
				LogFormat: "json",
			},
			isInstant: false,
			wantErr:   false,
			validate: func(t *testing.T, result string) {
				require.Equal(t, `rate({service_name="database"} [2m])`, result)
			},
		},
		{
			name:  "instant query with ${RANGE} - uses MinInstantRange",
			query: `rate(${SELECTOR} [${RANGE}])`,
			requirements: QueryRequirements{
				LogFormat: "json",
			},
			isInstant: true,
			wantErr:   false,
			validate: func(t *testing.T, result string) {
				require.Equal(t, `rate({service_name="database"} [5m])`, result)
			},
		},
		{
			name:  "range query with ${SELECTOR} and ${RANGE}",
			query: `rate(${SELECTOR} [${RANGE}])`,
			requirements: QueryRequirements{
				LogFormat: "json",
			},
			isInstant: false,
			wantErr:   false,
			validate: func(t *testing.T, result string) {
				require.Equal(t, `rate({service_name="database"} [2m])`, result)
			},
		},
		{
			name:  "instant query with ${SELECTOR} and ${RANGE}",
			query: `rate(${SELECTOR} [${RANGE}])`,
			requirements: QueryRequirements{
				LogFormat: "json",
			},
			isInstant: true,
			wantErr:   false,
			validate: func(t *testing.T, result string) {
				require.Equal(t, `rate({service_name="database"} [5m])`, result)
			},
		},
		{
			name:  "multiple ${RANGE} occurrences - all replaced",
			query: `sum_over_time(${SELECTOR} | json | unwrap duration [${RANGE}]) + rate(${SELECTOR} [${RANGE}])`,
			requirements: QueryRequirements{
				LogFormat: "logfmt",
			},
			isInstant: false,
			wantErr:   false,
			validate: func(t *testing.T, result string) {
				require.Equal(t, `sum_over_time({service_name="loki"} | json | unwrap duration [3m]) + rate({service_name="loki"} [3m])`, result)
			},
		},
		{
			name:      "query without ${RANGE} - unchanged",
			query:     `rate({service_name="database"} [5m])`,
			isInstant: false,
			wantErr:   false,
			validate: func(t *testing.T, result string) {
				require.Equal(t, `rate({service_name="database"} [5m])`, result)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := resolver.ResolveQuery(tt.query, tt.requirements, tt.isInstant)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			if tt.validate != nil {
				tt.validate(t, result)
			}
		})
	}
}

func TestMetadataVariableResolver_GetTimeRange(t *testing.T) {
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)

	metadata := &DatasetMetadata{
		TimeRange: TimeRange{
			Start: start,
			End:   end,
		},
		AllSelectors: []string{`{service_name="test"}`},
	}
	resolver := NewMetadataVariableResolver(metadata, 42)

	tests := []struct {
		name     string
		length   time.Duration
		wantErr  bool
		validate func(t *testing.T, gotStart, gotEnd time.Time)
	}{
		{
			name:    "1 hour window",
			length:  time.Hour,
			wantErr: false,
			validate: func(t *testing.T, gotStart, gotEnd time.Time) {
				// Should be within dataset bounds
				require.True(t, gotStart.After(start) || gotStart.Equal(start))
				require.True(t, gotEnd.Before(end) || gotEnd.Equal(end))
				// Duration should be exactly 1 hour
				require.Equal(t, time.Hour, gotEnd.Sub(gotStart))
			},
		},
		{
			name:    "4 hours window",
			length:  4 * time.Hour,
			wantErr: false,
			validate: func(t *testing.T, gotStart, gotEnd time.Time) {
				require.True(t, gotStart.After(start) || gotStart.Equal(start))
				require.True(t, gotEnd.Before(end) || gotEnd.Equal(end))
				require.Equal(t, 4*time.Hour, gotEnd.Sub(gotStart))
			},
		},
		{
			name:    "full dataset length",
			length:  24 * time.Hour,
			wantErr: false,
			validate: func(t *testing.T, gotStart, gotEnd time.Time) {
				// Should use full dataset
				require.Equal(t, start, gotStart)
				require.Equal(t, end, gotEnd)
			},
		},
		{
			name:    "longer than dataset - should fail",
			length:  48 * time.Hour,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotStart, gotEnd, err := resolver.GetTimeRange(tt.length)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			if tt.validate != nil {
				tt.validate(t, gotStart, gotEnd)
			}
		})
	}
}

func TestMetadataVariableResolver_LabelFilter(t *testing.T) {
	metadata := &DatasetMetadata{
		TimeRange: TimeRange{
			Start: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			End:   time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
		},
		AllSelectors: []string{
			`{service_name="database", region="us-west-2", env="prod", cluster="cluster-1"}`,
			`{service_name="loki", region="us-east-1", env="dev"}`,
		},
		ByLabelKey: map[string][]string{
			"cluster": {`{service_name="database", region="us-west-2", env="prod", cluster="cluster-1"}`},
			"region":  {`{service_name="database", region="us-west-2", env="prod", cluster="cluster-1"}`, `{service_name="loki", region="us-east-1", env="dev"}`},
			"env":     {`{service_name="database", region="us-west-2", env="prod", cluster="cluster-1"}`, `{service_name="loki", region="us-east-1", env="dev"}`},
		},
	}

	resolver := NewMetadataVariableResolver(metadata, 42)

	tests := []struct {
		name         string
		query        string
		requirements QueryRequirements
		wantQuery    string
	}{
		{
			name:  "label with cluster - picks database",
			query: `${SELECTOR} | ${LABEL_NAME} = "${LABEL_VALUE}"`,
			requirements: QueryRequirements{
				Labels: []string{"cluster"},
			},
			wantQuery: `{service_name="database", region="us-west-2", env="prod", cluster="cluster-1"} | cluster = "cluster-1"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := resolver.ResolveQuery(tt.query, tt.requirements, false)
			require.NoError(t, err)

			require.Equal(t, tt.wantQuery, result)
		})
	}
}

func TestExtractLabelFromSelector(t *testing.T) {
	resolver := &MetadataVariableResolver{
		rnd: rand.New(rand.NewSource(42)),
	}

	tests := []struct {
		name         string
		selector     string
		requirements QueryRequirements
		wantErr      bool
		validate     func(t *testing.T, labelName, labelValue string)
	}{
		{
			name:     "simple selector with one label",
			selector: `{service_name="database"}`,
			wantErr:  false,
			validate: func(t *testing.T, labelName, labelValue string) {
				require.Equal(t, "service_name", labelName)
				require.Equal(t, "database", labelValue)
			},
		},
		{
			name:     "selector with multiple labels",
			selector: `{region="us-west-2", env="prod"}`,
			wantErr:  false,
			validate: func(t *testing.T, labelName, labelValue string) {
				// Should pick one of the labels
				require.Contains(t, []string{"region", "env"}, labelName)
				if labelName == "region" {
					require.Equal(t, "us-west-2", labelValue)
				} else {
					require.Equal(t, "prod", labelValue)
				}
			},
		},
		{
			name:     "selector with regex matcher",
			selector: `{service_name=~"(?i)grafana", env="prod"}`,
			wantErr:  false,
			validate: func(t *testing.T, labelName, labelValue string) {
				// Should extract the equality matcher (env="prod"), not the regex matcher
				require.Equal(t, "env", labelName)
				require.Equal(t, "prod", labelValue)
			},
		},
		{
			name:     "prioritize required label",
			selector: `{region="us-west-2", env="prod", cluster="cluster1"}`,
			requirements: QueryRequirements{
				Labels: []string{"cluster"},
			},
			wantErr: false,
			validate: func(t *testing.T, labelName, labelValue string) {
				// Should prefer the indexed label
				require.Equal(t, "cluster", labelName)
				require.Equal(t, "cluster1", labelValue)
			},
		},
		{
			name:     "empty selector",
			selector: `{}`,
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			labelName, labelValue, err := resolver.extractLabelFromSelector(tt.selector, tt.requirements)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			if tt.validate != nil {
				tt.validate(t, labelName, labelValue)
			}
		})
	}
}

func TestIntersect(t *testing.T) {
	tests := []struct {
		name string
		a    []string
		b    []string
		want []string
	}{
		{
			name: "common elements",
			a:    []string{"a", "b", "c"},
			b:    []string{"b", "c", "d"},
			want: []string{"b", "c"},
		},
		{
			name: "no common elements",
			a:    []string{"a", "b"},
			b:    []string{"c", "d"},
			want: []string{},
		},
		{
			name: "empty first slice",
			a:    []string{},
			b:    []string{"a", "b"},
			want: []string{},
		},
		{
			name: "empty second slice",
			a:    []string{"a", "b"},
			b:    []string{},
			want: []string{},
		},
		{
			name: "identical slices",
			a:    []string{"a", "b", "c"},
			b:    []string{"a", "b", "c"},
			want: []string{"a", "b", "c"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := intersect(tt.a, tt.b)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestMetadataVariableResolver_DetectedFields(t *testing.T) {
	// Create test metadata
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)

	metadata := &DatasetMetadata{
		TimeRange: TimeRange{
			Start: start,
			End:   end,
		},
		AllSelectors: []string{
			`{service_name="database"}`,
			`{service_name="loki"}`,
			`{service_name="nginx"}`,
		},
		ByFormat: map[LogFormat][]string{
			LogFormatJSON: {
				`{service_name="database"}`,
			},
			LogFormatLogfmt: {
				`{service_name="loki"}`,
			},
			LogFormatUnstructured: {
				`{service_name="nginx"}`,
			},
		},
		ByDetectedField: map[string][]string{
			"level": {
				`{service_name="database"}`, // JSON apps have level
				`{service_name="loki"}`,     // Logfmt apps have level
			},
			"query_type": {
				`{service_name="loki"}`, // Only logfmt apps have query_type
			},
		},
		MetadataBySelector: map[string]*SerializableStreamMetadata{
			`{service_name="database"}`: {
				MinRange:        5 * time.Minute,
				MinInstantRange: 10 * time.Minute,
			},
			`{service_name="loki"}`: {
				MinRange:        5 * time.Minute,
				MinInstantRange: 10 * time.Minute,
			},
			`{service_name="nginx"}`: {
				MinRange:        5 * time.Minute,
				MinInstantRange: 10 * time.Minute,
			},
		},
	}

	resolver := NewMetadataVariableResolver(metadata, 42)

	t.Run("level field filters correctly", func(t *testing.T) {
		requirements := QueryRequirements{
			DetectedFields: []string{"level"},
		}

		selector, err := resolver.resolveLabelSelector(requirements)
		require.NoError(t, err)

		// Should match database or loki, not nginx
		require.True(t,
			selector == `{service_name="database"}` || selector == `{service_name="loki"}`,
			"selector should be database or loki, got: %s", selector)
	})

	t.Run("query_type field filters to logfmt only", func(t *testing.T) {
		requirements := QueryRequirements{
			DetectedFields: []string{"query_type"},
		}

		selector, err := resolver.resolveLabelSelector(requirements)
		require.NoError(t, err)
		require.Equal(t, `{service_name="loki"}`, selector)
	})

	t.Run("combining format and detected_field requirements", func(t *testing.T) {
		requirements := QueryRequirements{
			LogFormat:      "logfmt",
			DetectedFields: []string{"level"},
		}

		selector, err := resolver.resolveLabelSelector(requirements)
		require.NoError(t, err)
		require.Equal(t, `{service_name="loki"}`, selector)
	})

	t.Run("unstructured format has no detected fields", func(t *testing.T) {
		requirements := QueryRequirements{
			LogFormat:      "unstructured",
			DetectedFields: []string{"level"},
		}

		_, err := resolver.resolveLabelSelector(requirements)
		require.Error(t, err)
		require.Contains(t, err.Error(), "no streams with detected field")
	})

	t.Run("multiple detected fields with intersection", func(t *testing.T) {
		requirements := QueryRequirements{
			DetectedFields: []string{"level", "query_type"},
		}

		selector, err := resolver.resolveLabelSelector(requirements)
		require.NoError(t, err)
		// Only loki has both level and query_type
		require.Equal(t, `{service_name="loki"}`, selector)
	})
}
