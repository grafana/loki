package querytee

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/go-kit/log/level"
	jsoniter "github.com/json-iterator/go"
	"github.com/pkg/errors"
	"github.com/prometheus/common/model"

	"github.com/grafana/loki/v3/pkg/loghttp"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

// SamplesComparatorFunc helps with comparing different types of samples coming from /api/v1/query and /api/v1/query_range routes.
type SamplesComparatorFunc func(expected, actual json.RawMessage, opts SampleComparisonOptions) (*ComparisonSummary, error)

type SamplesResponse struct {
	Status string
	Data   struct {
		ResultType string
		Result     json.RawMessage
	}
}

type SampleComparisonOptions struct {
	Tolerance         float64
	UseRelativeError  bool
	SkipRecentSamples time.Duration
}

func NewSamplesComparator(opts SampleComparisonOptions) *SamplesComparator {
	return &SamplesComparator{
		opts: opts,
		sampleTypesComparator: map[string]SamplesComparatorFunc{
			"matrix":                 compareMatrix,
			"vector":                 compareVector,
			"scalar":                 compareScalar,
			loghttp.ResultTypeStream: compareStreams, // TODO: this makes it more than a samples compator
		},
	}
}

type SamplesComparator struct {
	opts                  SampleComparisonOptions
	sampleTypesComparator map[string]SamplesComparatorFunc
}

// RegisterSamplesType helps with registering custom sample types
func (s *SamplesComparator) RegisterSamplesType(samplesType string, comparator SamplesComparatorFunc) {
	s.sampleTypesComparator[samplesType] = comparator
}

func (s *SamplesComparator) Compare(expectedResponse, actualResponse []byte) (*ComparisonSummary, error) {
	var expected, actual SamplesResponse

	err := json.Unmarshal(expectedResponse, &expected)
	if err != nil {
		return nil, errors.Wrap(err, "unable to unmarshal expected response")
	}

	err = json.Unmarshal(actualResponse, &actual)
	if err != nil {
		return nil, errors.Wrap(err, "unable to unmarshal actual response")
	}

	if expected.Status != actual.Status {
		return nil, fmt.Errorf("expected status %s but got %s", expected.Status, actual.Status)
	}

	if expected.Data.ResultType != actual.Data.ResultType {
		return nil, fmt.Errorf("expected resultType %s but got %s", expected.Data.ResultType, actual.Data.ResultType)
	}

	comparator, ok := s.sampleTypesComparator[expected.Data.ResultType]
	if !ok {
		return nil, fmt.Errorf("resultType %s not registered for comparison", expected.Data.ResultType)
	}

	return comparator(expected.Data.Result, actual.Data.Result, s.opts)
}

func compareMatrix(expectedRaw, actualRaw json.RawMessage, opts SampleComparisonOptions) (*ComparisonSummary, error) {
	var expected, actual model.Matrix

	err := json.Unmarshal(expectedRaw, &expected)
	if err != nil {
		return nil, errors.Wrap(err, "unable to unmarshal expected matrix")
	}
	err = json.Unmarshal(actualRaw, &actual)
	if err != nil {
		return nil, errors.Wrap(err, "unable to unmarshal actual matrix")
	}

	if len(expected) != len(actual) {
		return nil, fmt.Errorf("expected %d metrics but got %d", len(expected),
			len(actual))
	}

	metricFingerprintToIndexMap := make(map[model.Fingerprint]int, len(expected))
	for i, actualMetric := range actual {
		metricFingerprintToIndexMap[actualMetric.Metric.Fingerprint()] = i
	}

	for _, expectedMetric := range expected {
		actualMetricIndex, ok := metricFingerprintToIndexMap[expectedMetric.Metric.Fingerprint()]
		if !ok {
			return nil, fmt.Errorf("expected metric %s missing from actual response", expectedMetric.Metric)
		}

		actualMetric := actual[actualMetricIndex]
		expectedMetricLen := len(expectedMetric.Values)
		actualMetricLen := len(actualMetric.Values)

		if expectedMetricLen != actualMetricLen {
			err := fmt.Errorf("expected %d samples for metric %s but got %d", expectedMetricLen,
				expectedMetric.Metric, actualMetricLen)
			if expectedMetricLen > 0 && actualMetricLen > 0 {
				level.Error(util_log.Logger).Log("msg", err.Error(), "oldest-expected-ts", expectedMetric.Values[0].Timestamp,
					"newest-expected-ts", expectedMetric.Values[expectedMetricLen-1].Timestamp,
					"oldest-actual-ts", actualMetric.Values[0].Timestamp, "newest-actual-ts", actualMetric.Values[actualMetricLen-1].Timestamp)
			}
			return nil, err
		}

		for i, expectedSamplePair := range expectedMetric.Values {
			actualSamplePair := actualMetric.Values[i]
			err := compareSamplePair(expectedSamplePair, actualSamplePair, opts)
			if err != nil {
				return nil, errors.Wrapf(err, "sample pair not matching for metric %s", expectedMetric.Metric)
			}
		}
	}

	return nil, nil
}

func compareVector(expectedRaw, actualRaw json.RawMessage, opts SampleComparisonOptions) (*ComparisonSummary, error) {
	var expected, actual model.Vector

	err := json.Unmarshal(expectedRaw, &expected)
	if err != nil {
		return nil, errors.Wrap(err, "unable to unmarshal expected vector")
	}

	err = json.Unmarshal(actualRaw, &actual)
	if err != nil {
		return nil, errors.Wrap(err, "unable to unmarshal actual vector")
	}

	if len(expected) != len(actual) {
		return nil, fmt.Errorf("expected %d metrics but got %d", len(expected),
			len(actual))
	}

	metricFingerprintToIndexMap := make(map[model.Fingerprint]int, len(expected))
	for i, actualMetric := range actual {
		metricFingerprintToIndexMap[actualMetric.Metric.Fingerprint()] = i
	}

	missingMetrics := make([]model.Metric, 0)
	for _, expectedMetric := range expected {
		actualMetricIndex, ok := metricFingerprintToIndexMap[expectedMetric.Metric.Fingerprint()]
		if !ok {
			missingMetrics = append(missingMetrics, expectedMetric.Metric)
			continue
		}

		// TODO: collect errors instead of returning.
		actualMetric := actual[actualMetricIndex]
		err := compareSamplePair(model.SamplePair{
			Timestamp: expectedMetric.Timestamp,
			Value:     expectedMetric.Value,
		}, model.SamplePair{
			Timestamp: actualMetric.Timestamp,
			Value:     actualMetric.Value,
		}, opts)
		if err != nil {
			return nil, errors.Wrapf(err, "sample pair not matching for metric %s", expectedMetric.Metric)
		}
	}

	if len(missingMetrics) > 0 {
		var b strings.Builder
		for i, m := range missingMetrics {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(m.String())
		}
		err = fmt.Errorf("expected metric(s) [%s] missing from actual response", b.String())
	}

	return &ComparisonSummary{missingMetrics: len(missingMetrics)}, err
}

func compareScalar(expectedRaw, actualRaw json.RawMessage, opts SampleComparisonOptions) (*ComparisonSummary, error) {
	var expected, actual model.Scalar
	err := json.Unmarshal(expectedRaw, &expected)
	if err != nil {
		return nil, errors.Wrap(err, "unable to unmarshal expected scalar")
	}

	err = json.Unmarshal(actualRaw, &actual)
	if err != nil {
		return nil, errors.Wrap(err, "unable to actual expected scalar")
	}

	return nil, compareSamplePair(model.SamplePair{
		Timestamp: expected.Timestamp,
		Value:     expected.Value,
	}, model.SamplePair{
		Timestamp: actual.Timestamp,
		Value:     actual.Value,
	}, opts)
}

func compareSamplePair(expected, actual model.SamplePair, opts SampleComparisonOptions) error {
	if expected.Timestamp != actual.Timestamp {
		return fmt.Errorf("expected timestamp %v but got %v", expected.Timestamp, actual.Timestamp)
	}
	if opts.SkipRecentSamples > 0 && time.Since(expected.Timestamp.Time()) < opts.SkipRecentSamples {
		return nil
	}
	if !compareSampleValue(expected.Value, actual.Value, opts) {
		return fmt.Errorf("expected value %s for timestamp %v but got %s", expected.Value, expected.Timestamp, actual.Value)
	}

	return nil
}

func compareSampleValue(first, second model.SampleValue, opts SampleComparisonOptions) bool {
	f := float64(first)
	s := float64(second)

	if (math.IsNaN(f) && math.IsNaN(s)) ||
		(math.IsInf(f, 1) && math.IsInf(s, 1)) ||
		(math.IsInf(f, -1) && math.IsInf(s, -1)) {
		return true
	} else if opts.Tolerance <= 0 {
		return math.Float64bits(f) == math.Float64bits(s)
	}
	if opts.UseRelativeError && s != 0 {
		return math.Abs(f-s)/math.Abs(s) <= opts.Tolerance
	}
	return math.Abs(f-s) <= opts.Tolerance
}

func compareStreams(expectedRaw, actualRaw json.RawMessage, _ SampleComparisonOptions) (*ComparisonSummary, error) {
	var expected, actual loghttp.Streams

	err := jsoniter.Unmarshal(expectedRaw, &expected)
	if err != nil {
		return nil, errors.Wrap(err, "unable to unmarshal expected streams")
	}
	err = jsoniter.Unmarshal(actualRaw, &actual)
	if err != nil {
		return nil, errors.Wrap(err, "unable to unmarshal actual streams")
	}

	if len(expected) != len(actual) {
		return nil, fmt.Errorf("expected %d streams but got %d", len(expected), len(actual))
	}

	streamLabelsToIndexMap := make(map[string]int, len(expected))
	for i, actualStream := range actual {
		streamLabelsToIndexMap[actualStream.Labels.String()] = i
	}

	for _, expectedStream := range expected {
		actualStreamIndex, ok := streamLabelsToIndexMap[expectedStream.Labels.String()]
		if !ok {
			return nil, fmt.Errorf("expected stream %s missing from actual response", expectedStream.Labels)
		}

		actualStream := actual[actualStreamIndex]
		expectedValuesLen := len(expectedStream.Entries)
		actualValuesLen := len(actualStream.Entries)

		if expectedValuesLen != actualValuesLen {
			err := fmt.Errorf("expected %d values for stream %s but got %d", expectedValuesLen,
				expectedStream.Labels, actualValuesLen)
			if expectedValuesLen > 0 && actualValuesLen > 0 {
				level.Error(util_log.Logger).Log("msg", err.Error(), "oldest-expected-ts", expectedStream.Entries[0].Timestamp.UnixNano(),
					"newest-expected-ts", expectedStream.Entries[expectedValuesLen-1].Timestamp.UnixNano(),
					"oldest-actual-ts", actualStream.Entries[0].Timestamp.UnixNano(), "newest-actual-ts", actualStream.Entries[actualValuesLen-1].Timestamp.UnixNano())
			}
			return nil, err
		}

		for i, expectedSamplePair := range expectedStream.Entries {
			actualSamplePair := actualStream.Entries[i]
			if !expectedSamplePair.Timestamp.Equal(actualSamplePair.Timestamp) {
				return nil, fmt.Errorf("expected timestamp %v but got %v for stream %s", expectedSamplePair.Timestamp.UnixNano(),
					actualSamplePair.Timestamp.UnixNano(), expectedStream.Labels)
			}
			if expectedSamplePair.Line != actualSamplePair.Line {
				return nil, fmt.Errorf("expected line %s for timestamp %v but got %s for stream %s", expectedSamplePair.Line,
					expectedSamplePair.Timestamp.UnixNano(), actualSamplePair.Line, expectedStream.Labels)
			}
		}
	}

	return nil, nil
}
