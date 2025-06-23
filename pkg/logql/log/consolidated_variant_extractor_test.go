package log

import (
	"testing"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/util/constants"
)

// Test_ConsolidatedMultiVariantExtractor tests that the consolidated extractor
// only calls the common pipeline once per log line.
func Test_ConsolidatedMultiVariantExtractor(t *testing.T) {
	t.Run("common pipeline should be called once per log line", func(t *testing.T) {
		// Create a counter to track how many times the pipeline is called
		counter := &MockProcessingCounter{}

		// Create a mock common pipeline
		commonPipeline := &MockCommonPipeline{
			counter:       counter,
			shouldPass:    true,
			processedLine: "processed line",
		}

		// Create mock variant-specific extractors
		variants := []SampleExtractor{
			&MockVariantSpecificExtractor{valueToExtract: 10.0, shouldExtract: true},
			&MockVariantSpecificExtractor{valueToExtract: 5.0, shouldExtract: true},
			&MockVariantSpecificExtractor{valueToExtract: 5.0, shouldExtract: false},
		}

		// Attempt to create a consolidated multi-variant extractor
		// This will fail because the function doesn't exist yet
		extractor := NewConsolidatedMultiVariantExtractor(commonPipeline, variants)
		streamExtractor := extractor.ForStream(labels.FromStrings("foo", "bar"))

		// Process a log line
		samples, ok := streamExtractor.ProcessString(1000, "test log line", labels.EmptyLabels())
		require.True(t, ok)
		require.Len(t, samples, 2)

		val := samples[0].Value
		lbls := samples[0].Labels

		// Verify the extractor returns the correct value
		assert.Equal(t, 10.0, val)
		assert.Contains(t, lbls.String(), constants.VariantLabel+"=\"0\"")

		val = samples[1].Value
		lbls = samples[1].Labels

		// Verify the extractor returns the correct value
		assert.Equal(t, 5.0, val)
		assert.Contains(t, lbls.String(), constants.VariantLabel+"=\"1\"")

		// Most importantly, verify the common pipeline was called exactly once
		assert.Equal(t, 1, counter.ProcessCount)
	})
}

// MockProcessingCounter tracks how many times Process is called
type MockProcessingCounter struct {
	ProcessCount int
}

// MockCommonPipeline is a Pipeline that counts how many times it's called
type MockCommonPipeline struct {
	counter *MockProcessingCounter
	// Whether pipeline should filter out the log line
	shouldPass bool
	// What the pipeline returns as the processed line
	processedLine string
}

func (m *MockCommonPipeline) Reset() {
	panic("not implemented") // TODO: Implement
}

func (m *MockCommonPipeline) ForStream(lbls labels.Labels) StreamPipeline {
	lblsResult := NewLabelsResult(lbls.String(), lbls.Hash(), lbls, labels.EmptyLabels(), labels.EmptyLabels())
	return &MockCommonStreamPipeline{
		counter:       m.counter,
		shouldPass:    m.shouldPass,
		processedLine: m.processedLine,
		labels:        lblsResult,
	}
}

// MockCommonStreamPipeline is the stream-specific part of MockCommonPipeline
type MockCommonStreamPipeline struct {
	counter       *MockProcessingCounter
	shouldPass    bool
	processedLine string
	labels        LabelsResult
}

func (m *MockCommonStreamPipeline) BaseLabels() LabelsResult {
	return m.labels
}

func (m *MockCommonStreamPipeline) ReferencedStructuredMetadata() bool {
	return false
}

func (m *MockCommonStreamPipeline) Process(_ int64, _ []byte, _ labels.Labels) ([]byte, LabelsResult, bool) {
	m.counter.ProcessCount++
	return []byte(m.processedLine), m.labels, m.shouldPass
}

func (m *MockCommonStreamPipeline) ProcessString(_ int64, _ string, _ labels.Labels) (string, LabelsResult, bool) {
	m.counter.ProcessCount++
	return m.processedLine, m.labels, m.shouldPass
}

// MockVariantSpecificExtractor is a VariantSpecificExtractor for testing
type MockVariantSpecificExtractor struct {
	valueToExtract float64
	shouldExtract  bool
}

func (m *MockVariantSpecificExtractor) ForStream(lbls labels.Labels) StreamSampleExtractor {
	lblsResult := NewLabelsResult(lbls.String(), lbls.Hash(), lbls, labels.EmptyLabels(), labels.EmptyLabels())
	return &mockVariantSpecificStreamExtractor{
		valueToExtract: m.valueToExtract,
		shouldExtract:  m.shouldExtract,
		labels:         lblsResult,
	}
}

type mockVariantSpecificStreamExtractor struct {
	variantIndex   int
	valueToExtract float64
	shouldExtract  bool
	labels         LabelsResult
}

func (m *mockVariantSpecificStreamExtractor) BaseLabels() LabelsResult {
	panic("not implemented") // TODO: Implement
}

func (m *mockVariantSpecificStreamExtractor) Process(_ int64, _ []byte, _ labels.Labels) ([]ExtractedSample, bool) {
	result := []ExtractedSample{
		{
			Value:  m.valueToExtract,
			Labels: m.labels,
		},
	}

	return result, m.shouldExtract
}

func (m *mockVariantSpecificStreamExtractor) ProcessString(ts int64, line string, lbls labels.Labels) ([]ExtractedSample, bool) {
	return m.Process(ts, []byte(line), lbls)
}

func (m *mockVariantSpecificStreamExtractor) ReferencedStructuredMetadata() bool {
	return false
}
