package bench

import (
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/logql/syntax"
)

const (
	// Variable placeholders
	placeholderSelector   = "${SELECTOR}"
	placeholderLabelName  = "${LABEL_NAME}"
	placeholderLabelValue = "${LABEL_VALUE}"
	placeholderRange      = "${RANGE}"
)

// MetadataVariableResolver resolves variables based on dataset metadata
// It uses multi-dimensional filtering to ensure queries match compatible streams
type MetadataVariableResolver struct {
	metadata *DatasetMetadata
	rnd      *rand.Rand
}

// NewMetadataVariableResolver creates a new resolver from dataset metadata
func NewMetadataVariableResolver(metadata *DatasetMetadata, seed int64) *MetadataVariableResolver {
	return &MetadataVariableResolver{
		metadata: metadata,
		rnd:      rand.New(rand.NewSource(seed)),
	}
}

// ResolveQuery resolves variables in a query based on requirements
// Supports: ${SELECTOR}, ${LABEL_NAME}, ${LABEL_VALUE}, ${RANGE}
// The isInstant parameter determines whether to use MinInstantRange (true) or MinRange (false) for ${RANGE}
func (r *MetadataVariableResolver) ResolveQuery(query string, requirements QueryRequirements, isInstant bool) (string, error) {
	result := query
	var selector string

	// Resolve ${SELECTOR} if present
	if strings.Contains(result, placeholderSelector) {
		var err error
		selector, err = r.resolveLabelSelector(requirements)
		if err != nil {
			return "", fmt.Errorf("failed to resolve ${SELECTOR}: %w", err)
		}
		result = strings.ReplaceAll(result, placeholderSelector, selector)
	}

	// Resolve ${LABEL_NAME} and ${LABEL_VALUE} if present
	if strings.Contains(result, placeholderLabelName) || strings.Contains(result, placeholderLabelValue) {
		// ${LABEL_NAME} and ${LABEL_VALUE} require ${SELECTOR} to be present
		// because we need to extract a label from the resolved selector
		if selector == "" {
			return "", fmt.Errorf("${LABEL_NAME} and ${LABEL_VALUE} require ${SELECTOR} to be present in the query")
		}

		labelName, labelValue, err := r.extractLabelFromSelector(selector, requirements)
		if err != nil {
			return "", fmt.Errorf("failed to extract label from selector: %w", err)
		}

		result = strings.ReplaceAll(result, placeholderLabelName, labelName)
		result = strings.ReplaceAll(result, placeholderLabelValue, labelValue)
	}

	// Resolve ${RANGE} if present
	if strings.Contains(result, placeholderRange) {
		rangeValue, err := r.resolveRange(selector, isInstant)
		if err != nil {
			return "", fmt.Errorf("failed to resolve ${RANGE}: %w", err)
		}
		result = strings.ReplaceAll(result, placeholderRange, rangeValue)
	}

	return result, nil
}

// resolveLabelSelector filters streams by all requirements and returns a random selector
func (r *MetadataVariableResolver) resolveLabelSelector(req QueryRequirements) (string, error) {
	candidates := r.metadata.AllSelectors

	if req.LogFormat != "" {
		format := LogFormat(req.LogFormat)
		candidates = intersect(candidates, r.metadata.ByFormat[format])
		if len(candidates) == 0 {
			return "", fmt.Errorf("no streams with log format: %s", req.LogFormat)
		}
	}

	for _, field := range req.UnwrappableFields {
		candidates = intersect(candidates, r.metadata.ByUnwrappableField[field])
		if len(candidates) == 0 {
			return "", fmt.Errorf("no streams with unwrappable field: %s", field)
		}
	}

	for _, field := range req.DetectedFields {
		candidates = intersect(candidates, r.metadata.ByDetectedField[field])
		if len(candidates) == 0 {
			return "", fmt.Errorf("no streams with detected field: %s", field)
		}
	}

	for _, key := range req.StructuredMetadata {
		candidates = intersect(candidates, r.metadata.ByStructuredMetadata[key])
		if len(candidates) == 0 {
			return "", fmt.Errorf("no streams with structured metadata: %s", key)
		}
	}

	for _, label := range req.Labels {
		candidates = intersect(candidates, r.metadata.ByLabelKey[label])
		if len(candidates) == 0 {
			return "", fmt.Errorf("no streams with label: %s", label)
		}
	}

	for _, keyword := range req.Keywords {
		candidates = intersect(candidates, r.metadata.ByKeyword[keyword])
		if len(candidates) == 0 {
			return "", fmt.Errorf("no streams with keyword: %s", keyword)
		}
	}

	if len(candidates) == 0 {
		return "", fmt.Errorf("no streams match all requirements")
	}

	return candidates[r.rnd.Intn(len(candidates))], nil
}

// extractLabelFromSelector parses a label selector and extracts a random label name and value
// For a selector like {foo="bar", baz="qux"}, it returns one of: (foo, bar) or (baz, qux)
// If labels are specified in requirements, it prioritizes those labels
func (r *MetadataVariableResolver) extractLabelFromSelector(selector string, req QueryRequirements) (labelName, labelValue string, err error) {
	matchers, err := syntax.ParseMatchers(selector, true)
	if err != nil {
		return "", "", fmt.Errorf("failed to parse selector %q: %w", selector, err)
	}

	type labelPair struct {
		name  string
		value string
	}
	var allPairs []labelPair
	var requiredPairs []labelPair

	for _, matcher := range matchers {
		// We only use equality matchers since we need concrete values
		if matcher.Type != labels.MatchEqual {
			continue
		}

		pair := labelPair{name: matcher.Name, value: matcher.Value}
		allPairs = append(allPairs, pair)

		for _, requiredLabel := range req.Labels {
			if matcher.Name == requiredLabel {
				requiredPairs = append(requiredPairs, pair)
				break
			}
		}
	}

	if len(allPairs) == 0 {
		return "", "", fmt.Errorf("no label pairs found in selector: %s", selector)
	}

	// Prefer required labels if specified, otherwise use any label
	var chosenPair labelPair
	if len(requiredPairs) > 0 {
		chosenPair = requiredPairs[r.rnd.Intn(len(requiredPairs))]
	} else {
		chosenPair = allPairs[r.rnd.Intn(len(allPairs))]
	}

	return chosenPair.name, chosenPair.value, nil
}

// GetTimeRange returns the start and end time for a query based on the metadata time range
func (r *MetadataVariableResolver) GetTimeRange(length time.Duration) (start, end time.Time, err error) {
	datasetStart := r.metadata.TimeRange.Start
	datasetEnd := r.metadata.TimeRange.End
	datasetDuration := datasetEnd.Sub(datasetStart)

	// Validate that the dataset is long enough
	if datasetDuration < length {
		return time.Time{}, time.Time{}, fmt.Errorf("dataset duration %v is shorter than requested length %v", datasetDuration, length)
	}

	// Pick a random start point that allows the full length to fit
	var randomOffset time.Duration
	if datasetDuration > length {
		maxOffset := datasetDuration - length
		randomOffset = time.Duration(r.rnd.Int63n(int64(maxOffset)))
	}

	start = datasetStart.Add(randomOffset)
	end = start.Add(length)

	// Ensure we don't exceed the dataset bounds
	if end.After(datasetEnd) {
		end = datasetEnd
		start = end.Add(-length)
	}

	return start, end, nil
}

// resolveRange resolves the ${RANGE} variable based on query type and stream metadata
func (r *MetadataVariableResolver) resolveRange(selector string, isInstant bool) (string, error) {
	if selector == "" {
		return "", fmt.Errorf("resolveRange requires a resolved selector")
	}

	// Look up stream metadata for this selector
	streamMeta, ok := r.metadata.MetadataBySelector[selector]
	if !ok {
		return "", fmt.Errorf("no stream metadata found for selector: %s", selector)
	}

	// Select appropriate range based on query type
	var rangeDuration time.Duration
	if isInstant {
		rangeDuration = streamMeta.MinInstantRange
	} else {
		rangeDuration = streamMeta.MinRange
	}

	if rangeDuration == 0 {
		return "", fmt.Errorf("stream has zero range duration (instant=%v): %s", isInstant, selector)
	}

	return formatDuration(rangeDuration), nil
}

// formatDuration formats a duration as a clean string for LogQL queries
// Examples: 5m, 2h, 30s, 1h30m
func formatDuration(d time.Duration) string {
	// Use time.Duration.String() but clean up redundant zeros
	s := d.String()
	// Remove trailing 0s and 0m0s patterns
	if strings.HasSuffix(s, "m0s") {
		s = s[:len(s)-2]
	} else if strings.HasSuffix(s, "h0m0s") {
		s = s[:len(s)-4]
	}
	return s
}

// intersect returns the intersection of two sorted string slices
// Both input slices must be sorted for this to work correctly
func intersect(a, b []string) []string {
	if len(a) == 0 || len(b) == 0 {
		return []string{}
	}

	result := make([]string, 0, minInt(len(a), len(b)))
	i, j := 0, 0

	for i < len(a) && j < len(b) {
		if a[i] == b[j] {
			result = append(result, a[i])
			i++
			j++
		} else if a[i] < b[j] {
			i++
		} else {
			j++
		}
	}

	return result
}

// minInt returns the minimum of two integers
func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
