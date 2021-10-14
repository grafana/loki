package querytee

import (
	"encoding/json"
	"fmt"
	"math"

	"github.com/go-kit/log/level"
	"github.com/pkg/errors"
	"github.com/prometheus/common/model"

	util_log "github.com/cortexproject/cortex/pkg/util/log"
)

// SamplesComparatorFunc helps with comparing different types of samples coming from /api/v1/query and /api/v1/query_range routes.
type SamplesComparatorFunc func(expected, actual json.RawMessage, tolerance float64) error

type SamplesResponse struct {
	Status string
	Data   struct {
		ResultType string
		Result     json.RawMessage
	}
}

func NewSamplesComparator(tolerance float64) *SamplesComparator {
	return &SamplesComparator{
		tolerance: tolerance,
		sampleTypesComparator: map[string]SamplesComparatorFunc{
			"matrix": compareMatrix,
			"vector": compareVector,
			"scalar": compareScalar,
		},
	}
}

type SamplesComparator struct {
	tolerance             float64
	sampleTypesComparator map[string]SamplesComparatorFunc
}

// RegisterSamplesComparator helps with registering custom sample types
func (s *SamplesComparator) RegisterSamplesType(samplesType string, comparator SamplesComparatorFunc) {
	s.sampleTypesComparator[samplesType] = comparator
}

func (s *SamplesComparator) Compare(expectedResponse, actualResponse []byte) error {
	var expected, actual SamplesResponse

	err := json.Unmarshal(expectedResponse, &expected)
	if err != nil {
		return errors.Wrap(err, "unable to unmarshal expected response")
	}

	err = json.Unmarshal(actualResponse, &actual)
	if err != nil {
		return errors.Wrap(err, "unable to unmarshal actual response")
	}

	if expected.Status != actual.Status {
		return fmt.Errorf("expected status %s but got %s", expected.Status, actual.Status)
	}

	if expected.Data.ResultType != actual.Data.ResultType {
		return fmt.Errorf("expected resultType %s but got %s", expected.Data.ResultType, actual.Data.ResultType)
	}

	comparator, ok := s.sampleTypesComparator[expected.Data.ResultType]
	if !ok {
		return fmt.Errorf("resultType %s not registered for comparison", expected.Data.ResultType)
	}

	return comparator(expected.Data.Result, actual.Data.Result, s.tolerance)
}

func compareMatrix(expectedRaw, actualRaw json.RawMessage, tolerance float64) error {
	var expected, actual model.Matrix

	err := json.Unmarshal(expectedRaw, &expected)
	if err != nil {
		return err
	}
	err = json.Unmarshal(actualRaw, &actual)
	if err != nil {
		return err
	}

	if len(expected) != len(actual) {
		return fmt.Errorf("expected %d metrics but got %d", len(expected),
			len(actual))
	}

	metricFingerprintToIndexMap := make(map[model.Fingerprint]int, len(expected))
	for i, actualMetric := range actual {
		metricFingerprintToIndexMap[actualMetric.Metric.Fingerprint()] = i
	}

	for _, expectedMetric := range expected {
		actualMetricIndex, ok := metricFingerprintToIndexMap[expectedMetric.Metric.Fingerprint()]
		if !ok {
			return fmt.Errorf("expected metric %s missing from actual response", expectedMetric.Metric)
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
			return err
		}

		for i, expectedSamplePair := range expectedMetric.Values {
			actualSamplePair := actualMetric.Values[i]
			err := compareSamplePair(expectedSamplePair, actualSamplePair, tolerance)
			if err != nil {
				return errors.Wrapf(err, "sample pair not matching for metric %s", expectedMetric.Metric)
			}
		}
	}

	return nil
}

func compareVector(expectedRaw, actualRaw json.RawMessage, tolerance float64) error {
	var expected, actual model.Vector

	err := json.Unmarshal(expectedRaw, &expected)
	if err != nil {
		return err
	}

	err = json.Unmarshal(actualRaw, &actual)
	if err != nil {
		return err
	}

	if len(expected) != len(actual) {
		return fmt.Errorf("expected %d metrics but got %d", len(expected),
			len(actual))
	}

	metricFingerprintToIndexMap := make(map[model.Fingerprint]int, len(expected))
	for i, actualMetric := range actual {
		metricFingerprintToIndexMap[actualMetric.Metric.Fingerprint()] = i
	}

	for _, expectedMetric := range expected {
		actualMetricIndex, ok := metricFingerprintToIndexMap[expectedMetric.Metric.Fingerprint()]
		if !ok {
			return fmt.Errorf("expected metric %s missing from actual response", expectedMetric.Metric)
		}

		actualMetric := actual[actualMetricIndex]
		err := compareSamplePair(model.SamplePair{
			Timestamp: expectedMetric.Timestamp,
			Value:     expectedMetric.Value,
		}, model.SamplePair{
			Timestamp: actualMetric.Timestamp,
			Value:     actualMetric.Value,
		}, tolerance)
		if err != nil {
			return errors.Wrapf(err, "sample pair not matching for metric %s", expectedMetric.Metric)
		}
	}

	return nil
}

func compareScalar(expectedRaw, actualRaw json.RawMessage, tolerance float64) error {
	var expected, actual model.Scalar
	err := json.Unmarshal(expectedRaw, &expected)
	if err != nil {
		return err
	}

	err = json.Unmarshal(actualRaw, &actual)
	if err != nil {
		return err
	}

	return compareSamplePair(model.SamplePair{
		Timestamp: expected.Timestamp,
		Value:     expected.Value,
	}, model.SamplePair{
		Timestamp: actual.Timestamp,
		Value:     actual.Value,
	}, tolerance)
}

func compareSamplePair(expected, actual model.SamplePair, tolerance float64) error {
	if expected.Timestamp != actual.Timestamp {
		return fmt.Errorf("expected timestamp %v but got %v", expected.Timestamp, actual.Timestamp)
	}
	if !compareSampleValue(expected.Value, actual.Value, tolerance) {
		return fmt.Errorf("expected value %s for timestamp %v but got %s", expected.Value, expected.Timestamp, actual.Value)
	}

	return nil
}

func compareSampleValue(first, second model.SampleValue, tolerance float64) bool {
	f := float64(first)
	s := float64(second)

	if math.IsNaN(f) && math.IsNaN(s) {
		return true
	} else if tolerance <= 0 {
		return math.Float64bits(f) == math.Float64bits(s)
	}

	return math.Abs(f-s) <= tolerance
}
