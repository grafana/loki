package distributor

import (
	"testing"
)

func TestParseSegmentationRule(t *testing.T) {
	tests := []struct {
		name        string
		ruleStr     string
		expected    *SegmentationRule
		expectError bool
	}{
		{
			name:    "simple key rule",
			ruleStr: "service_name",
			expected: &SegmentationRule{
				Keys: []KeyValue{
					{Key: "service_name", Value: nil},
				},
			},
			expectError: false,
		},
		{
			name:    "key=value rule with additional keys",
			ruleStr: "service_name=querier,tenant,cluster",
			expected: &SegmentationRule{
				Keys: []KeyValue{
					{Key: "service_name", Value: stringPtr("querier")},
					{Key: "tenant", Value: nil},
					{Key: "cluster", Value: nil},
				},
			},
			expectError: false,
		},
		{
			name:    "key=value rule with single additional key",
			ruleStr: "service_name=ingester,tenant",
			expected: &SegmentationRule{
				Keys: []KeyValue{
					{Key: "service_name", Value: stringPtr("ingester")},
					{Key: "tenant", Value: nil},
				},
			},
			expectError: false,
		},
		{
			name:    "key=value rule with specific additional key values",
			ruleStr: "service_name=querier,tenant=94018,level",
			expected: &SegmentationRule{
				Keys: []KeyValue{
					{Key: "service_name", Value: stringPtr("querier")},
					{Key: "tenant", Value: stringPtr("94018")},
					{Key: "level", Value: nil},
				},
			},
			expectError: false,
		},
		{
			name:        "empty rule",
			ruleStr:     "",
			expected:    nil,
			expectError: true,
		},
		{
			name:        "invalid key=value format",
			ruleStr:     "service_name=",
			expected:    nil,
			expectError: true,
		},
		{
			name:        "empty key in key=value pair",
			ruleStr:     "=querier,tenant",
			expected:    nil,
			expectError: true,
		},
		{
			name:    "key=value rule without additional keys",
			ruleStr: "service_name=querier",
			expected: &SegmentationRule{
				Keys: []KeyValue{
					{Key: "service_name", Value: stringPtr("querier")},
				},
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParseSegmentationRule(tt.ruleStr)
			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if result == nil {
				t.Errorf("expected result but got nil")
				return
			}

			if len(result.Keys) != len(tt.expected.Keys) {
				t.Errorf("expected %d keys, got %d", len(tt.expected.Keys), len(result.Keys))
				return
			}

			for i, expectedKV := range tt.expected.Keys {
				actualKV := result.Keys[i]
				if actualKV.Key != expectedKV.Key {
					t.Errorf("key %d: expected %s, got %s", i, expectedKV.Key, actualKV.Key)
				}
				if !stringPtrEqual(actualKV.Value, expectedKV.Value) {
					t.Errorf("key %d value: expected %v, got %v", i, expectedKV.Value, actualKV.Value)
				}
			}
		})
	}
}

func TestSegmentationRule_Matches(t *testing.T) {
	rule := &SegmentationRule{
		Keys: []KeyValue{
			{Key: "service_name", Value: stringPtr("querier")},
			{Key: "tenant", Value: stringPtr("94018")},
			{Key: "level", Value: nil},
		},
	}

	tests := []struct {
		name               string
		labels             map[string]string
		structuredMetadata map[string]string
		expected           bool
	}{
		{
			name: "exact match",
			labels: map[string]string{
				"service_name": "querier",
				"tenant":       "94018",
				"level":        "info",
			},
			structuredMetadata: map[string]string{},
			expected:           true,
		},
		{
			name: "match with structured metadata",
			labels: map[string]string{
				"service_name": "querier",
			},
			structuredMetadata: map[string]string{
				"tenant": "94018",
				"level":  "info",
			},
			expected: true,
		},
		{
			name: "no match - wrong service_name",
			labels: map[string]string{
				"service_name": "ingester",
				"tenant":       "94018",
				"level":        "info",
			},
			structuredMetadata: map[string]string{},
			expected:           false,
		},
		{
			name: "no match - wrong tenant",
			labels: map[string]string{
				"service_name": "querier",
				"tenant":       "12345",
				"level":        "info",
			},
			structuredMetadata: map[string]string{},
			expected:           false,
		},
		{
			name: "no match - missing tenant",
			labels: map[string]string{
				"service_name": "querier",
				"level":        "info",
			},
			structuredMetadata: map[string]string{},
			expected:           false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := rule.Matches(tt.labels, tt.structuredMetadata)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestSegmentationConfig_GetSegmentationKeys(t *testing.T) {
	config := &SegmentationConfig{
		Rules: []SegmentationRule{
			{
				Keys: []KeyValue{
					{Key: "service_name", Value: stringPtr("querier")},
					{Key: "tenant", Value: stringPtr("94018")},
					{Key: "level", Value: nil},
				},
			},
			{
				Keys: []KeyValue{
					{Key: "service_name", Value: stringPtr("ingester")},
					{Key: "tenant", Value: nil},
				},
			},
			{
				Keys: []KeyValue{
					{Key: "service_name", Value: nil},
				},
			},
		},
	}

	tests := []struct {
		name               string
		labels             map[string]string
		structuredMetadata map[string]string
		expectedKeys       []string
	}{
		{
			name: "match first rule",
			labels: map[string]string{
				"service_name": "querier",
				"tenant":       "94018",
				"level":        "info",
			},
			structuredMetadata: map[string]string{},
			expectedKeys:       []string{"level", "service_name", "tenant"},
		},
		{
			name: "match second rule",
			labels: map[string]string{
				"service_name": "ingester",
				"tenant":       "12",
			},
			structuredMetadata: map[string]string{},
			expectedKeys:       []string{"service_name", "tenant"},
		},
		{
			name: "match third rule",
			labels: map[string]string{
				"service_name": "unknown",
			},
			structuredMetadata: map[string]string{},
			expectedKeys:       []string{"service_name"},
		},
		{
			name: "no match",
			labels: map[string]string{
				"foo": "bar",
			},
			structuredMetadata: map[string]string{},
			expectedKeys:       nil,
		},
		{
			name: "match with structured metadata",
			labels: map[string]string{
				"service_name": "querier",
			},
			structuredMetadata: map[string]string{
				"tenant": "94018",
				"level":  "info",
			},
			expectedKeys: []string{"level", "service_name", "tenant"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := config.GetSegmentationKeys(tt.labels, tt.structuredMetadata)
			if !stringSlicesEqual(result, tt.expectedKeys) {
				t.Errorf("expected %v, got %v", tt.expectedKeys, result)
			}
		})
	}
}

func TestSegmentationRule_String(t *testing.T) {
	tests := []struct {
		name     string
		rule     *SegmentationRule
		expected string
	}{
		{
			name: "simple key rule",
			rule: &SegmentationRule{
				Keys: []KeyValue{
					{Key: "service_name", Value: nil},
				},
			},
			expected: "service_name",
		},
		{
			name: "key=value rule with additional keys",
			rule: &SegmentationRule{
				Keys: []KeyValue{
					{Key: "service_name", Value: stringPtr("querier")},
					{Key: "tenant", Value: nil},
					{Key: "level", Value: nil},
				},
			},
			expected: "service_name=querier,tenant,level",
		},
		{
			name: "key=value rule with specific additional key values",
			rule: &SegmentationRule{
				Keys: []KeyValue{
					{Key: "service_name", Value: stringPtr("querier")},
					{Key: "tenant", Value: stringPtr("94018")},
					{Key: "level", Value: nil},
				},
			},
			expected: "service_name=querier,tenant=94018,level",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.rule.String()
			if result != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, result)
			}
		})
	}
}

// Helper functions
func stringPtr(s string) *string {
	return &s
}

func stringPtrEqual(a, b *string) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
