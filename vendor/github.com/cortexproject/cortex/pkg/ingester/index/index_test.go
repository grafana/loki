package index

import (
	"testing"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/promql"
	"github.com/stretchr/testify/assert"

	"github.com/cortexproject/cortex/pkg/ingester/client"
)

func TestIndex(t *testing.T) {
	index := New()

	for _, entry := range []struct {
		m  model.Metric
		fp model.Fingerprint
	}{
		{model.Metric{"foo": "bar", "flip": "flop"}, 3},
		{model.Metric{"foo": "bar", "flip": "flap"}, 2},
		{model.Metric{"foo": "baz", "flip": "flop"}, 1},
		{model.Metric{"foo": "baz", "flip": "flap"}, 0},
	} {
		index.Add(client.ToLabelPairs(entry.m), entry.fp)
	}

	for _, tc := range []struct {
		matchers []*labels.Matcher
		fps      []model.Fingerprint
	}{
		{nil, nil},
		{mustParseMatcher(`{fizz="buzz"}`), []model.Fingerprint{}},

		{mustParseMatcher(`{foo="bar"}`), []model.Fingerprint{2, 3}},
		{mustParseMatcher(`{foo="baz"}`), []model.Fingerprint{0, 1}},
		{mustParseMatcher(`{flip="flop"}`), []model.Fingerprint{1, 3}},
		{mustParseMatcher(`{flip="flap"}`), []model.Fingerprint{0, 2}},

		{mustParseMatcher(`{foo="bar", flip="flop"}`), []model.Fingerprint{3}},
		{mustParseMatcher(`{foo="bar", flip="flap"}`), []model.Fingerprint{2}},
		{mustParseMatcher(`{foo="baz", flip="flop"}`), []model.Fingerprint{1}},
		{mustParseMatcher(`{foo="baz", flip="flap"}`), []model.Fingerprint{0}},
	} {
		assert.Equal(t, tc.fps, index.Lookup(tc.matchers))
	}

	assert.Equal(t, model.LabelNames{"flip", "foo"}, index.LabelNames())
	assert.Equal(t, model.LabelValues{"bar", "baz"}, index.LabelValues("foo"))
	assert.Equal(t, model.LabelValues{"flap", "flop"}, index.LabelValues("flip"))
}

func mustParseMatcher(s string) []*labels.Matcher {
	ms, err := promql.ParseMetricSelector(s)
	if err != nil {
		panic(err)
	}
	return ms
}
