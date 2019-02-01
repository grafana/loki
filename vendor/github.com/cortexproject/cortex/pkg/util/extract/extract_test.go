package extract

import (
	"reflect"
	"testing"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/stretchr/testify/assert"
)

func TestExtractMetricNameMatcherFromMatchers(t *testing.T) {
	metricMatcher, err := labels.NewMatcher(labels.MatchEqual, model.MetricNameLabel, "testmetric")
	if err != nil {
		t.Fatal(err)
	}
	labelMatcher1, err := labels.NewMatcher(labels.MatchEqual, "label", "value1")
	if err != nil {
		t.Fatal(err)
	}
	labelMatcher2, err := labels.NewMatcher(labels.MatchEqual, "label", "value2")
	if err != nil {
		t.Fatal(err)
	}
	nonEqualityMetricMatcher, err := labels.NewMatcher(labels.MatchNotEqual, model.MetricNameLabel, "testmetric")
	if err != nil {
		t.Fatal(err)
	}
	jobMatcher, err := labels.NewMatcher(labels.MatchEqual, model.JobLabel, "testjob")
	if err != nil {
		t.Fatal(err)
	}

	for i, tc := range []struct {
		matchers         []*labels.Matcher
		expMetricMatcher *labels.Matcher
		expOutMatchers   []*labels.Matcher
		expOk            bool
	}{
		{
			matchers:         []*labels.Matcher{metricMatcher},
			expMetricMatcher: metricMatcher,
			expOutMatchers:   []*labels.Matcher{},
			expOk:            true,
		}, {
			matchers:         []*labels.Matcher{metricMatcher, labelMatcher1, labelMatcher2},
			expMetricMatcher: metricMatcher,
			expOutMatchers:   []*labels.Matcher{labelMatcher1, labelMatcher2},
			expOk:            true,
		}, {
			matchers:         []*labels.Matcher{labelMatcher1, metricMatcher, labelMatcher2},
			expMetricMatcher: metricMatcher,
			expOutMatchers:   []*labels.Matcher{labelMatcher1, labelMatcher2},
			expOk:            true,
		}, {
			matchers:         []*labels.Matcher{labelMatcher1, labelMatcher2, metricMatcher},
			expMetricMatcher: metricMatcher,
			expOutMatchers:   []*labels.Matcher{labelMatcher1, labelMatcher2},
			expOk:            true,
		}, {
			matchers:         []*labels.Matcher{nonEqualityMetricMatcher},
			expMetricMatcher: nonEqualityMetricMatcher,
			expOutMatchers:   []*labels.Matcher{},
			expOk:            true,
		}, {
			matchers:         []*labels.Matcher{jobMatcher},
			expMetricMatcher: nil,
			expOutMatchers:   []*labels.Matcher{jobMatcher},
			expOk:            false,
		}, {
			matchers:         []*labels.Matcher{},
			expMetricMatcher: nil,
			expOutMatchers:   []*labels.Matcher{},
			expOk:            false,
		},
	} {
		matchersCopy := make([]*labels.Matcher, len(tc.matchers))
		copy(matchersCopy, tc.matchers)

		nameMatcher, outMatchers, ok := MetricNameMatcherFromMatchers(tc.matchers)

		if !reflect.DeepEqual(tc.matchers, matchersCopy) {
			t.Fatalf("%d. Matchers got mutated; want %v, got %v", i, matchersCopy, tc.matchers)
		}

		if !reflect.DeepEqual(tc.expMetricMatcher, nameMatcher) {
			t.Fatalf("%d. Wrong metric matcher; want '%v', got %v", i, tc.expMetricMatcher, nameMatcher)
		}

		assert.Equal(t, tc.expOutMatchers, outMatchers, "unexpected outMatchers for test case %d", i)

		assert.Equal(t, tc.expOk, ok, "unexpected ok for test case %d", i)
	}
}
