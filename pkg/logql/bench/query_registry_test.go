package bench

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestQueryRegistry_ValidateRequirements(t *testing.T) {
	tests := []struct {
		name        string
		yamlContent string
		expectError string
	}{
		{
			name: "valid unwrappable field",
			yamlContent: `
queries:
  - description: Valid query
    query: '{service_name="database"} | json | unwrap duration [5m]'
    kind: metric
    time_range:
      length: 24h
      step: 1m
    requires:
      log_format: json
      unwrappable_fields:
        - duration
`,
			expectError: "",
		},
		{
			name: "invalid unwrappable field",
			yamlContent: `
queries:
  - description: Invalid unwrappable field
    query: '{service_name="test"} | json | unwrap invalid_field [5m]'
    kind: metric
    time_range:
      length: 24h
      step: 1m
    requires:
      log_format: json
      unwrappable_fields:
        - invalid_field
`,
			expectError: "unwrappable field \"invalid_field\" not in bounded set",
		},
		{
			name: "invalid label",
			yamlContent: `
queries:
  - description: Invalid label
    query: 'sum by (invalid_label) (count_over_time({service_name="test"}[5m]))'
    kind: metric
    time_range:
      length: 24h
      step: 1m
    requires:
      labels:
        - invalid_label
`,
			expectError: "label \"invalid_label\" not in bounded set",
		},
		{
			name: "invalid keyword",
			yamlContent: `
queries:
  - description: Invalid keyword
    query: '{service_name="test"} |= "invalid_keyword"'
    kind: log
    time_range:
      length: 24h
    requires:
      keywords:
        - invalid_keyword
`,
			expectError: "keyword \"invalid_keyword\" not in bounded set",
		},
		{
			name: "invalid structured metadata",
			yamlContent: `
queries:
  - description: Invalid structured metadata
    query: '{service_name="test"} | invalid_key="value"'
    kind: log
    time_range:
      length: 24h
    requires:
      structured_metadata:
        - invalid_key
`,
			expectError: "structured metadata key \"invalid_key\" not in bounded set",
		},
		{
			name: "valid container label",
			yamlContent: `
queries:
  - description: Valid container label
    query: 'sum by (container) (count_over_time({service_name="database"}[5m]))'
    kind: metric
    time_range:
      length: 24h
      step: 1m
    requires:
      log_format: json
      labels:
        - container
`,
			expectError: "",
		},
		{
			name: "valid pod label",
			yamlContent: `
queries:
  - description: Valid pod label
    query: 'sum by (pod) (count_over_time({service_name="web-server"}[5m]))'
    kind: metric
    time_range:
      length: 24h
      step: 1m
    requires:
      log_format: json
      labels:
        - pod
`,
			expectError: "",
		},
		{
			name: "valid level keyword",
			yamlContent: `
queries:
  - description: Valid level keyword
    query: '{service_name="database"} |= "level"'
    kind: log
    time_range:
      length: 24h
    requires:
      keywords:
        - level
`,
			expectError: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a temporary directory for the test
			tmpDir := t.TempDir()
			testFile := filepath.Join(tmpDir, "test.yaml")

			// Write the YAML content to the temp file
			err := os.WriteFile(testFile, []byte(tt.yamlContent), 0644)
			require.NoError(t, err)

			// Try to load the file
			registry := NewQueryRegistry(tmpDir)
			_, err = registry.loadFile(testFile, SuiteFast, "test.yaml")

			if tt.expectError != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.expectError)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
