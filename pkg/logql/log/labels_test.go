package log

import (
	"testing"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logqlmodel"
)

func TestLabelsBuilder_Get(t *testing.T) {
	lbs := labels.FromStrings("already", "in")
	b := NewBaseLabelsBuilder().ForLabels(lbs, lbs.Hash())
	b.Reset()
	b.Set(StructuredMetadataLabel, "foo", "bar")
	b.Set(ParsedLabel, "bar", "buzz")

	_, category, ok := b.GetWithCategory("bar")
	require.Equal(t, ParsedLabel, category)
	require.True(t, ok)
	require.False(t, b.referencedStructuredMetadata)

	_, category, ok = b.GetWithCategory("foo")
	require.Equal(t, StructuredMetadataLabel, category)
	require.True(t, ok)
	require.True(t, b.referencedStructuredMetadata)

	b.Del("foo")
	_, _, ok = b.GetWithCategory("foo")
	require.False(t, ok)
	v, category, ok := b.GetWithCategory("bar")
	require.True(t, ok)
	require.Equal(t, "buzz", v)
	require.Equal(t, ParsedLabel, category)
	v, category, ok = b.GetWithCategory("already")
	require.True(t, ok)
	require.Equal(t, "in", v)
	require.Equal(t, StreamLabel, category)
	b.Del("bar")
	_, _, ok = b.GetWithCategory("bar")
	require.False(t, ok)
	b.Del("already")
	_, _, ok = b.GetWithCategory("already")
	require.False(t, ok)
}

func TestLabelsBuilder_LabelsError(t *testing.T) {
	lbs := labels.FromStrings("already", "in")
	b := NewBaseLabelsBuilder().ForLabels(lbs, lbs.Hash())
	b.Reset()
	b.SetErr("err")
	lbsWithErr := b.LabelsResult()

	expectedLbs := labels.FromStrings(
		logqlmodel.ErrorLabel, "err",
		"already", "in",
	)
	require.Equal(t, expectedLbs, lbsWithErr.Labels())
	require.Equal(t, expectedLbs.String(), lbsWithErr.String())
	require.Equal(t, expectedLbs.Hash(), lbsWithErr.Hash())
	require.Equal(t, labels.FromStrings("already", "in"), lbsWithErr.Stream())
	require.Nil(t, lbsWithErr.StructuredMetadata())
	require.Equal(t, labels.FromStrings(logqlmodel.ErrorLabel, "err"), lbsWithErr.Parsed())

	// make sure the original labels is unchanged.
	require.Equal(t, labels.FromStrings("already", "in"), lbs)
}

func TestLabelsBuilder_LabelsErrorFromAdd(t *testing.T) {
	lbs := labels.FromStrings("already", "in")
	b := NewBaseLabelsBuilder().ForLabels(lbs, lbs.Hash())
	b.Reset()

	// This works for any category
	b.Add(StructuredMetadataLabel, labels.FromStrings(logqlmodel.ErrorLabel, "test error", logqlmodel.ErrorDetailsLabel, "test details")...)
	lbsWithErr := b.LabelsResult()

	expectedLbs := labels.FromStrings(
		logqlmodel.ErrorLabel, "test error",
		logqlmodel.ErrorDetailsLabel, "test details",
		"already", "in",
	)
	require.Equal(t, expectedLbs, lbsWithErr.Labels())
	require.Equal(t, expectedLbs.String(), lbsWithErr.String())
	require.Equal(t, expectedLbs.Hash(), lbsWithErr.Hash())
	require.Equal(t, labels.FromStrings("already", "in"), lbsWithErr.Stream())
	require.Nil(t, lbsWithErr.StructuredMetadata())
	require.Equal(t, labels.FromStrings(logqlmodel.ErrorLabel, "test error", logqlmodel.ErrorDetailsLabel, "test details"), lbsWithErr.Parsed())

	// make sure the original labels is unchanged.
	require.Equal(t, labels.FromStrings("already", "in"), lbs)
}

func TestLabelsBuilder_IntoMap(t *testing.T) {
	strs := []string{
		"namespace", "loki",
		"job", "us-central1/loki",
		"cluster", "us-central1",
		"ToReplace", "text",
	}
	lbs := labels.FromStrings(strs...)

	t.Run("it still copies the map after a Reset", func(t *testing.T) {
		b := NewBaseLabelsBuilder().ForLabels(lbs, lbs.Hash())

		m := map[string]string{}
		b.IntoMap(m)

		require.Equal(t, map[string]string{
			"namespace": "loki",
			"job":       "us-central1/loki",
			"cluster":   "us-central1",
			"ToReplace": "text",
		}, m)

		b.Reset()

		m2 := map[string]string{}
		b.IntoMap(m2)
		require.Equal(t, map[string]string{
			"namespace": "loki",
			"job":       "us-central1/loki",
			"cluster":   "us-central1",
			"ToReplace": "text",
		}, m2)
	})

	t.Run("it can copy the map several times", func(t *testing.T) {
		b := NewBaseLabelsBuilder().ForLabels(lbs, lbs.Hash())

		m := map[string]string{}
		b.IntoMap(m)

		require.Equal(t, map[string]string{
			"namespace": "loki",
			"job":       "us-central1/loki",
			"cluster":   "us-central1",
			"ToReplace": "text",
		}, m)

		m2 := map[string]string{}
		b.IntoMap(m2)
		require.Equal(t, map[string]string{
			"namespace": "loki",
			"job":       "us-central1/loki",
			"cluster":   "us-central1",
			"ToReplace": "text",
		}, m2)
	})
}

func TestLabelsBuilder_LabelsResult(t *testing.T) {
	strs := []string{
		"namespace", "loki",
		"job", "us-central1/loki",
		"cluster", "us-central1",
		"ToReplace", "text",
	}
	lbs := labels.FromStrings(strs...)
	b := NewBaseLabelsBuilder().ForLabels(lbs, lbs.Hash())
	b.Reset()
	assertLabelResult(t, lbs, b.LabelsResult())
	b.SetErr("err")
	withErr := labels.FromStrings(append(strs, logqlmodel.ErrorLabel, "err")...)
	assertLabelResult(t, withErr, b.LabelsResult())

	b.Set(StructuredMetadataLabel, "foo", "bar")
	b.Set(StreamLabel, "namespace", "tempo")
	b.Set(ParsedLabel, "buzz", "fuzz")
	b.Set(ParsedLabel, "ToReplace", "other")
	b.Del("job")

	expectedStreamLbls := labels.FromStrings(
		"namespace", "tempo",
		"cluster", "us-central1",
	)
	expectedStucturedMetadataLbls := labels.FromStrings(
		"foo", "bar",
	)
	expectedParsedLbls := labels.FromStrings(
		logqlmodel.ErrorLabel, "err",
		"buzz", "fuzz",
		"ToReplace", "other",
	)
	expected := make(labels.Labels, 0, len(expectedStreamLbls)+len(expectedStucturedMetadataLbls)+len(expectedParsedLbls))
	expected = append(expected, expectedStreamLbls...)
	expected = append(expected, expectedStucturedMetadataLbls...)
	expected = append(expected, expectedParsedLbls...)
	expected = labels.New(expected...)

	assertLabelResult(t, expected, b.LabelsResult())
	// cached.
	assertLabelResult(t, expected, b.LabelsResult())

	actual := b.LabelsResult()
	assert.Equal(t, expectedStreamLbls, actual.Stream())
	assert.Equal(t, expectedStucturedMetadataLbls, actual.StructuredMetadata())
	assert.Equal(t, expectedParsedLbls, actual.Parsed())
}

func TestLabelsBuilder_GroupedLabelsResult(t *testing.T) {
	strs := []string{"namespace", "loki",
		"job", "us-central1/loki",
		"cluster", "us-central1"}
	lbs := labels.FromStrings(strs...)
	b := NewBaseLabelsBuilderWithGrouping([]string{"namespace"}, nil, false, false).ForLabels(lbs, lbs.Hash())
	b.Reset()
	assertLabelResult(t, labels.FromStrings("namespace", "loki"), b.GroupedLabels())
	b.SetErr("err")
	withErr := labels.FromStrings(append(strs, logqlmodel.ErrorLabel, "err")...)
	assertLabelResult(t, withErr, b.GroupedLabels())

	b.Reset()
	b.Set(StructuredMetadataLabel, "foo", "bar")
	b.Set(StreamLabel, "namespace", "tempo")
	b.Set(ParsedLabel, "buzz", "fuzz")
	b.Del("job")
	expected := labels.FromStrings("namespace", "tempo")
	assertLabelResult(t, expected, b.GroupedLabels())
	// cached.
	assertLabelResult(t, expected, b.GroupedLabels())

	b = NewBaseLabelsBuilderWithGrouping([]string{"job"}, nil, false, false).ForLabels(lbs, lbs.Hash())
	assertLabelResult(t, labels.FromStrings("job", "us-central1/loki"), b.GroupedLabels())
	assertLabelResult(t, labels.FromStrings("job", "us-central1/loki"), b.GroupedLabels())
	b.Del("job")
	assertLabelResult(t, labels.EmptyLabels(), b.GroupedLabels())
	b.Reset()
	b.Set(StreamLabel, "namespace", "tempo")
	assertLabelResult(t, labels.FromStrings("job", "us-central1/loki"), b.GroupedLabels())
	require.False(t, b.referencedStructuredMetadata)

	b = NewBaseLabelsBuilderWithGrouping([]string{"foo"}, nil, false, false).ForLabels(lbs, lbs.Hash())
	b.Set(StructuredMetadataLabel, "foo", "bar")
	assertLabelResult(t, labels.FromStrings("foo", "bar"), b.GroupedLabels())
	require.True(t, b.referencedStructuredMetadata)

	b = NewBaseLabelsBuilderWithGrouping([]string{"job"}, nil, true, false).ForLabels(lbs, lbs.Hash())
	b.Del("job")
	b.Set(StructuredMetadataLabel, "foo", "bar")
	b.Set(StreamLabel, "job", "something")
	expected = labels.FromStrings("namespace", "loki",
		"cluster", "us-central1",
		"foo", "bar",
	)
	assertLabelResult(t, expected, b.GroupedLabels())
	require.False(t, b.referencedStructuredMetadata)

	b = NewBaseLabelsBuilderWithGrouping([]string{"foo"}, nil, true, false).ForLabels(lbs, lbs.Hash())
	b.Set(StructuredMetadataLabel, "foo", "bar")
	expected = labels.FromStrings("namespace", "loki",
		"job", "us-central1/loki",
		"cluster", "us-central1",
	)
	assertLabelResult(t, expected, b.GroupedLabels())
	require.True(t, b.referencedStructuredMetadata)

	b = NewBaseLabelsBuilderWithGrouping(nil, nil, false, false).ForLabels(lbs, lbs.Hash())
	b.Set(StructuredMetadataLabel, "foo", "bar")
	b.Set(StreamLabel, "job", "something")
	expected = labels.FromStrings("namespace", "loki",
		"job", "something",
		"cluster", "us-central1",
		"foo", "bar",
	)
	assertLabelResult(t, expected, b.GroupedLabels())
}

func assertLabelResult(t *testing.T, lbs labels.Labels, res LabelsResult) {
	t.Helper()
	require.Equal(t,
		lbs,
		res.Labels(),
	)
	require.Equal(t,
		lbs.Hash(),
		res.Hash(),
	)
	require.Equal(t,
		lbs.String(),
		res.String(),
	)
}
