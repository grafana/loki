package querytee

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/go-kit/log"
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

	// Filter out series that only contain recent samples
	if opts.SkipRecentSamples > 0 {
		expected = filterRecentOnlySeries(expected, opts.SkipRecentSamples)
		actual = filterRecentOnlySeries(actual, opts.SkipRecentSamples)
	}

	// If both matrices are empty after filtering, we can skip comparison
	if len(expected) == 0 && len(actual) == 0 {
		return &ComparisonSummary{skipped: true}, nil
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

		err := compareMatrixSamples(expectedMetric, actualMetric, opts)
		if err != nil {
			return nil, fmt.Errorf("%w\nExpected result for series:\n%v\n\nActual result for series:\n%v", err, expectedMetric, actualMetric)
		}
	}

	return nil, nil
}

func compareMatrixSamples(expected, actual *model.SampleStream, opts SampleComparisonOptions) error {
	expectedSamplesTail, actualSamplesTail, err := comparePairs(expected.Values, actual.Values, func(p1 model.SamplePair, p2 model.SamplePair) error {
		err := compareSamplePair(p1, p2, opts)
		if err != nil {
			return fmt.Errorf("float sample pair does not match for metric %s: %w", expected.Metric, err)
		}
		return nil
	})
	if err != nil {
		return err
	}

	expectedFloatSamplesCount := len(expected.Values)
	actualFloatSamplesCount := len(actual.Values)

	if expectedFloatSamplesCount == actualFloatSamplesCount {
		return nil
	}

	skipAllFloatSamples := canSkipAllSamples(func(p model.SamplePair) bool {
		return time.Since(p.Timestamp.Time())-opts.SkipRecentSamples < 0
	}, expectedSamplesTail, actualSamplesTail)

	if skipAllFloatSamples {
		return nil
	}

	err = fmt.Errorf(
		"expected %d float sample(s) for metric %s but got %d float sample(s) ",
		len(expected.Values),
		expected.Metric,
		len(actual.Values),
	)

	shouldLog := false
	logger := util_log.Logger

	if expectedFloatSamplesCount > 0 && actualFloatSamplesCount > 0 {
		logger = log.With(logger,
			"oldest-expected-float-ts", expected.Values[0].Timestamp,
			"newest-expected-float-ts", expected.Values[expectedFloatSamplesCount-1].Timestamp,
			"oldest-actual-float-ts", actual.Values[0].Timestamp,
			"newest-actual-float-ts", actual.Values[actualFloatSamplesCount-1].Timestamp,
		)
		shouldLog = true
	}

	if shouldLog {
		level.Error(logger).Log("msg", err.Error())
	}
	return err
}

// comparePairs runs compare for pairs in s1 and s2. It stops on the first error the compare returns.
// If len(s1) != len(s2) it compares only elements inside the longest prefix of both. If all elements within the prefix match,
// it returns the tail of s1 and s2, and a nil error.
func comparePairs[S ~[]M, M any](s1, s2 S, compare func(M, M) error) (S, S, error) {
	var i int
	for ; i < len(s1) && i < len(s2); i++ {
		err := compare(s1[i], s2[i])
		if err != nil {
			return s1, s2, err
		}
	}
	return s1[i:], s2[i:], nil
}

// trimBeginning returns s without the prefix that satisfies skip().
func trimBeginning[S ~[]M, M any](s S, skip func(M) bool) S {
	var i int
	for ; i < len(s); i++ {
		if !skip(s[i]) {
			break
		}
	}
	return s[i:]
}

// filterSlice returns a new slice with elements from s removed that satisfy skip().
func filterSlice[S ~[]M, M any](s S, skip func(M) bool) S {
	res := make(S, 0, len(s))
	for i := 0; i < len(s); i++ {
		if !skip(s[i]) {
			res = append(res, s[i])
		}
	}
	return res
}

func canSkipAllSamples[S ~[]M, M any](skip func(M) bool, ss ...S) bool {
	for _, s := range ss {
		for _, p := range s {
			if !skip(p) {
				return false
			}
		}
	}
	return true
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

	if allVectorSamplesWithinRecentSampleWindow(expected, opts) && allVectorSamplesWithinRecentSampleWindow(actual, opts) {
		return &ComparisonSummary{skipped: true}, nil
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

func allVectorSamplesWithinRecentSampleWindow(v model.Vector, opts SampleComparisonOptions) bool {
	if opts.SkipRecentSamples == 0 {
		return false
	}

	for _, sample := range v {
		if time.Since(sample.Timestamp.Time()) > opts.SkipRecentSamples {
			return false
		}
	}

	return true
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
	if opts.SkipRecentSamples > 0 && time.Since(expected.Timestamp.Time()) < opts.SkipRecentSamples {
		return nil
	}
	if expected.Timestamp != actual.Timestamp {
		return fmt.Errorf("expected timestamp %v but got %v", expected.Timestamp, actual.Timestamp)
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

func compareStreams(expectedRaw, actualRaw json.RawMessage, opts SampleComparisonOptions) (*ComparisonSummary, error) {
	var expected, actual loghttp.Streams

	err := jsoniter.Unmarshal(expectedRaw, &expected)
	if err != nil {
		return nil, errors.Wrap(err, "unable to unmarshal expected streams")
	}
	err = jsoniter.Unmarshal(actualRaw, &actual)
	if err != nil {
		return nil, errors.Wrap(err, "unable to unmarshal actual streams")
	}

	// Filter out streams that only contain recent entries
	if opts.SkipRecentSamples > 0 {
		expected = filterRecentOnlyStreams(expected, opts.SkipRecentSamples)
		actual = filterRecentOnlyStreams(actual, opts.SkipRecentSamples)
	}

	// If both streams are empty after filtering, we can skip comparison
	if len(expected) == 0 && len(actual) == 0 {
		return &ComparisonSummary{skipped: true}, nil
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

		expectedEntriesTail, actualEntriesTail, err := comparePairs(expectedStream.Entries, actualStream.Entries, func(e1, e2 loghttp.Entry) error {
			if opts.SkipRecentSamples > 0 && time.Since(e1.Timestamp) < opts.SkipRecentSamples {
				return nil
			}
			if !e1.Timestamp.Equal(e2.Timestamp) {
				return fmt.Errorf("expected timestamp %v but got %v for stream %s", e1.Timestamp.UnixNano(),
					e2.Timestamp.UnixNano(), expectedStream.Labels)
			}
			if e1.Line != e2.Line {
				return fmt.Errorf("expected line %s for timestamp %v but got %s for stream %s", e1.Line,
					e1.Timestamp.UnixNano(), e2.Line, expectedStream.Labels)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}

		expectedEntriesCount := len(expectedStream.Entries)
		actualEntriesCount := len(actualStream.Entries)

		if expectedEntriesCount == actualEntriesCount {
			continue
		}

		skipAllEntries := canSkipAllSamples(func(e loghttp.Entry) bool {
			return time.Since(e.Timestamp)-opts.SkipRecentSamples < 0
		}, expectedEntriesTail, actualEntriesTail)

		if skipAllEntries {
			continue
		}

		err = fmt.Errorf("expected %d entries for stream %s but got %d", expectedEntriesCount,
			expectedStream.Labels, actualEntriesCount)

		if expectedEntriesCount > 0 && actualEntriesCount > 0 {
			level.Error(util_log.Logger).Log("msg", err.Error(),
				"oldest-expected-ts", expectedStream.Entries[0].Timestamp.UnixNano(),
				"newest-expected-ts", expectedStream.Entries[expectedEntriesCount-1].Timestamp.UnixNano(),
				"oldest-actual-ts", actualStream.Entries[0].Timestamp.UnixNano(),
				"newest-actual-ts", actualStream.Entries[actualEntriesCount-1].Timestamp.UnixNano())
		}
		return nil, err
	}

	return nil, nil
}

// filterRecentOnlySeries removes series that only contain samples within the recent window
func filterRecentOnlySeries(matrix model.Matrix, recentWindow time.Duration) model.Matrix {
	result := make(model.Matrix, 0, len(matrix))

	for _, series := range matrix {
		skipAllSamples := canSkipAllSamples(func(p model.SamplePair) bool {
			return time.Since(p.Timestamp.Time())-recentWindow < 0
		}, series.Values)

		// Only keep series that have at least one old sample
		if !skipAllSamples {
			result = append(result, series)
		}
	}

	return result
}

// filterRecentOnlyStreams removes streams that only contain entries within the recent window
func filterRecentOnlyStreams(streams loghttp.Streams, recentWindow time.Duration) loghttp.Streams {
	result := make(loghttp.Streams, 0, len(streams))

	for _, stream := range streams {
		skipAllEntries := canSkipAllSamples(func(e loghttp.Entry) bool {
			return time.Since(e.Timestamp)-recentWindow < 0
		}, stream.Entries)

		// Only keep streams that have at least one old entry
		if !skipAllEntries {
			result = append(result, stream)
		}
	}

	return result
}
