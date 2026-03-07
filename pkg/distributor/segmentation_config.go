package distributor

import (
	"fmt"
	"sort"
	"strings"
)

// KeyValue represents a key-value pair for segmentation rules
type KeyValue struct {
	Key   string
	Value *string // nil means "any value", non-nil means "specific value"
}

// SegmentationRule represents a single segmentation rule
type SegmentationRule struct {
	Keys []KeyValue
}

// SegmentationConfig represents a collection of segmentation rules
type SegmentationConfig struct {
	Rules []SegmentationRule
}

// ParseSegmentationRule parses a single segmentation rule string
// Format: "key" or "key=value,additional_key1,additional_key2=value2"
func ParseSegmentationRule(ruleStr string) (*SegmentationRule, error) {
	ruleStr = strings.TrimSpace(ruleStr)
	if ruleStr == "" {
		return nil, fmt.Errorf("empty rule")
	}

	// Split by comma to get individual key parts
	parts := strings.Split(ruleStr, ",")
	if len(parts) == 0 {
		return nil, fmt.Errorf("invalid rule format: %s", ruleStr)
	}

	rule := &SegmentationRule{
		Keys: make([]KeyValue, 0, len(parts)),
	}

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		// Check if this is a key=value pair
		if strings.Contains(part, "=") {
			keyValue := strings.SplitN(part, "=", 2)
			if len(keyValue) != 2 {
				return nil, fmt.Errorf("invalid key=value format: %s", part)
			}
			key := strings.TrimSpace(keyValue[0])
			value := strings.TrimSpace(keyValue[1])
			if key == "" {
				return nil, fmt.Errorf("empty key in key=value pair: %s", part)
			}
			if value == "" {
				return nil, fmt.Errorf("empty value in key=value pair: %s", part)
			}
			rule.Keys = append(rule.Keys, KeyValue{Key: key, Value: &value})
		} else {
			// Simple key
			rule.Keys = append(rule.Keys, KeyValue{Key: part, Value: nil})
		}
	}

	if len(rule.Keys) == 0 {
		return nil, fmt.Errorf("no valid keys found in rule: %s", ruleStr)
	}

	return rule, nil
}

// ParseSegmentationConfig parses a list of segmentation rule strings
func ParseSegmentationConfig(rules []string) (*SegmentationConfig, error) {
	config := &SegmentationConfig{
		Rules: make([]SegmentationRule, 0, len(rules)),
	}

	for i, ruleStr := range rules {
		rule, err := ParseSegmentationRule(ruleStr)
		if err != nil {
			return nil, fmt.Errorf("rule %d: %w", i, err)
		}
		config.Rules = append(config.Rules, *rule)
	}

	return config, nil
}

// String returns a string representation of the rule
func (r *SegmentationRule) String() string {
	var parts []string
	for _, kv := range r.Keys {
		if kv.Value != nil {
			parts = append(parts, fmt.Sprintf("%s=%s", kv.Key, *kv.Value))
		} else {
			parts = append(parts, kv.Key)
		}
	}
	return strings.Join(parts, ",")
}

// Matches checks if this rule matches the given labels and structured metadata
func (r *SegmentationRule) Matches(labels map[string]string, structuredMetadata map[string]string) bool {
	if len(r.Keys) == 0 {
		return false
	}

	// All keys must match their values (if specified)
	for _, kv := range r.Keys {
		var value string
		var exists bool

		// Check labels first, then structured metadata
		if value, exists = labels[kv.Key]; !exists {
			if value, exists = structuredMetadata[kv.Key]; !exists {
				return false
			}
		}

		// If a specific value is required, check it matches
		if kv.Value != nil && value != *kv.Value {
			return false
		}
	}

	return true
}

// GetSegmentationKeys returns the segmentation keys for this rule
func (r *SegmentationRule) GetSegmentationKeys(labels map[string]string, structuredMetadata map[string]string) []string {
	if !r.Matches(labels, structuredMetadata) {
		return nil
	}

	var keys []string
	for _, kv := range r.Keys {
		// Only include keys that exist in the data
		if _, exists := labels[kv.Key]; exists {
			keys = append(keys, kv.Key)
		} else if _, exists := structuredMetadata[kv.Key]; exists {
			keys = append(keys, kv.Key)
		}
	}

	// Sort for consistent output
	sort.Strings(keys)
	return keys
}

// FindMatchingRule finds the first rule that matches the given labels and structured metadata
func (c *SegmentationConfig) FindMatchingRule(labels map[string]string, structuredMetadata map[string]string) *SegmentationRule {
	for i := range c.Rules {
		if c.Rules[i].Matches(labels, structuredMetadata) {
			return &c.Rules[i]
		}
	}
	return nil
}

// GetSegmentationKeys returns the segmentation keys for the first matching rule
func (c *SegmentationConfig) GetSegmentationKeys(labels map[string]string, structuredMetadata map[string]string) []string {
	rule := c.FindMatchingRule(labels, structuredMetadata)
	if rule == nil {
		return nil
	}
	return rule.GetSegmentationKeys(labels, structuredMetadata)
}
