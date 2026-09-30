package log

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logqlmodel"
)

func TestLabelsBuilder_Get(t *testing.T) {
	lbs := labels.FromStrings("already", "in")
	b := NewBaseLabelsBuilder().ForLabels(lbs, labels.StableHash(lbs))
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
	b := NewBaseLabelsBuilder().ForLabels(lbs, labels.StableHash(lbs))
	b.Reset()
	b.SetErr("err", nil)
	lbsWithErr := b.LabelsResult()

	expectedLbs := labels.FromStrings(
		logqlmodel.ErrorLabel, "err",
		"already", "in",
	)
	require.Equal(t, expectedLbs, lbsWithErr.Labels())
	require.Equal(t, expectedLbs.String(), lbsWithErr.String())
	require.Equal(t, labels.StableHash(expectedLbs), lbsWithErr.Hash())
	require.Equal(t, labels.FromStrings("already", "in"), lbsWithErr.Stream())
	require.Equal(t, labels.EmptyLabels(), lbsWithErr.StructuredMetadata())
	require.Equal(t, labels.FromStrings(logqlmodel.ErrorLabel, "err"), lbsWithErr.Parsed())

	// make sure the original labels is unchanged.
	require.Equal(t, labels.FromStrings("already", "in"), lbs)
}

func TestLabelsBuilder_LabelsErrorFromAdd(t *testing.T) {
	lbs := labels.FromStrings("already", "in")
	b := NewBaseLabelsBuilder().ForLabels(lbs, labels.StableHash(lbs))
	b.Reset()

	// This works for any category
	b.Add(StructuredMetadataLabel, labels.FromStrings(logqlmodel.ErrorLabel, "test error", logqlmodel.ErrorDetailsLabel, "test details"))
	lbsWithErr := b.LabelsResult()

	expectedLbs := labels.FromStrings(
		logqlmodel.ErrorLabel, "test error",
		logqlmodel.ErrorDetailsLabel, "test details",
		"already", "in",
	)
	require.Equal(t, expectedLbs, lbsWithErr.Labels())
	require.Equal(t, expectedLbs.String(), lbsWithErr.String())
	require.Equal(t, labels.StableHash(expectedLbs), lbsWithErr.Hash())
	require.Equal(t, labels.FromStrings("already", "in"), lbsWithErr.Stream())
	require.Equal(t, labels.EmptyLabels(), lbsWithErr.StructuredMetadata())
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
		b := NewBaseLabelsBuilder().ForLabels(lbs, labels.StableHash(lbs))

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
		b := NewBaseLabelsBuilder().ForLabels(lbs, labels.StableHash(lbs))

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
	b := NewBaseLabelsBuilder().ForLabels(lbs, labels.StableHash(lbs))
	b.Reset()
	assertLabelResult(t, lbs, b.LabelsResult())
	b.SetErr("err", nil)
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

	expected := mergeLabels(expectedStreamLbls, expectedStucturedMetadataLbls, expectedParsedLbls)

	assertLabelResult(t, expected, b.LabelsResult())
	// cached.
	assertLabelResult(t, expected, b.LabelsResult())

	actual := b.LabelsResult()
	assert.Equal(t, expectedStreamLbls, actual.Stream())
	assert.Equal(t, expectedStucturedMetadataLbls, actual.StructuredMetadata())
	assert.Equal(t, expectedParsedLbls, actual.Parsed())

	b.Reset()
	b.Set(StreamLabel, "namespace", "tempo")
	b.Set(StreamLabel, "bazz", "tazz")
	b.Set(StructuredMetadataLabel, "bazz", "sazz")
	b.Set(ParsedLabel, "ToReplace", "other")

	expectedStreamLbls = labels.FromStrings(
		"namespace", "tempo",
		"cluster", "us-central1",
		"job", "us-central1/loki",
	)
	expectedStucturedMetadataLbls = labels.FromStrings(
		"bazz", "sazz",
	)
	expectedParsedLbls = labels.FromStrings(
		"ToReplace", "other",
	)

	expected = mergeLabels(expectedStreamLbls, expectedStucturedMetadataLbls, expectedParsedLbls)
	assertLabelResult(t, expected, b.LabelsResult())
	// cached.
	assertLabelResult(t, expected, b.LabelsResult())
	actual = b.LabelsResult()
	assert.Equal(t, expectedStreamLbls, actual.Stream())
	assert.Equal(t, expectedStucturedMetadataLbls, actual.StructuredMetadata())
	assert.Equal(t, expectedParsedLbls, actual.Parsed())
}

func TestLabelsBuilder_Set(t *testing.T) {
	strs := []string{
		"namespace", "loki",
		"cluster", "us-central1",
		"toreplace", "fuzz",
	}
	lbs := labels.FromStrings(strs...)
	b := NewBaseLabelsBuilder().ForLabels(lbs, labels.StableHash(lbs))

	// test duplicating stream label with parsed label
	b.Set(StructuredMetadataLabel, "stzz", "stvzz")
	b.Set(ParsedLabel, "toreplace", "buzz")
	expectedStreamLbls := labels.FromStrings("namespace", "loki", "cluster", "us-central1")
	expectedStucturedMetadataLbls := labels.FromStrings("stzz", "stvzz")
	expectedParsedLbls := labels.FromStrings("toreplace", "buzz")

	expected := mergeLabels(expectedStreamLbls, expectedStucturedMetadataLbls, expectedParsedLbls)

	actual := b.LabelsResult()
	assertLabelResult(t, expected, actual)
	assert.Equal(t, expectedStreamLbls, actual.Stream())
	assert.Equal(t, expectedStucturedMetadataLbls, actual.StructuredMetadata())
	assert.Equal(t, expectedParsedLbls, actual.Parsed())

	b.Reset()

	// test duplicating structured metadata label with parsed label
	b.Set(StructuredMetadataLabel, "stzz", "stvzz")
	b.Set(StructuredMetadataLabel, "toreplace", "muzz")
	b.Set(ParsedLabel, "toreplace", "buzz")
	expectedStreamLbls = labels.FromStrings("namespace", "loki", "cluster", "us-central1")
	expectedStucturedMetadataLbls = labels.FromStrings("stzz", "stvzz")
	expectedParsedLbls = labels.FromStrings("toreplace", "buzz")

	expected = mergeLabels(expectedStreamLbls, expectedStucturedMetadataLbls, expectedParsedLbls)

	actual = b.LabelsResult()
	assertLabelResult(t, expected, actual)
	assert.Equal(t, expectedStreamLbls, actual.Stream())
	assert.Equal(t, expectedStucturedMetadataLbls, actual.StructuredMetadata())
	assert.Equal(t, expectedParsedLbls, actual.Parsed())

	b.Reset()

	// test duplicating stream label with structured meta data label
	b.Set(StructuredMetadataLabel, "toreplace", "muzz")
	b.Set(ParsedLabel, "stzz", "stvzz")
	expectedStreamLbls = labels.FromStrings("namespace", "loki", "cluster", "us-central1")
	expectedStucturedMetadataLbls = labels.FromStrings("toreplace", "muzz")
	expectedParsedLbls = labels.FromStrings("stzz", "stvzz")

	expected = mergeLabels(expectedStreamLbls, expectedStucturedMetadataLbls, expectedParsedLbls)

	actual = b.LabelsResult()
	assertLabelResult(t, expected, actual)
	assert.Equal(t, expectedStreamLbls, actual.Stream())
	assert.Equal(t, expectedStucturedMetadataLbls, actual.StructuredMetadata())
	assert.Equal(t, expectedParsedLbls, actual.Parsed())

	b.Reset()

	// test duplicating parsed label with structured meta data label
	b.Set(ParsedLabel, "toreplace", "puzz")
	b.Set(StructuredMetadataLabel, "stzz", "stvzzz")
	b.Set(StructuredMetadataLabel, "toreplace", "muzz")
	expectedStreamLbls = labels.FromStrings("namespace", "loki", "cluster", "us-central1")
	expectedStucturedMetadataLbls = labels.FromStrings("stzz", "stvzzz")
	expectedParsedLbls = labels.FromStrings("toreplace", "puzz")

	expected = mergeLabels(expectedStreamLbls, expectedStucturedMetadataLbls, expectedParsedLbls)

	actual = b.LabelsResult()
	assertLabelResult(t, expected, actual)
	assert.Equal(t, expectedStreamLbls, actual.Stream())
	assert.Equal(t, expectedStucturedMetadataLbls, actual.StructuredMetadata())
	assert.Equal(t, expectedParsedLbls, actual.Parsed())

	b.Reset()

	// test duplicating structured meta data label with stream label
	b.Set(ParsedLabel, "stzz", "stvzzz")
	b.Set(StructuredMetadataLabel, "toreplace", "muzz")
	expectedStreamLbls = labels.FromStrings("namespace", "loki", "cluster", "us-central1")
	expectedStucturedMetadataLbls = labels.FromStrings("toreplace", "muzz")
	expectedParsedLbls = labels.FromStrings("stzz", "stvzzz")

	expected = mergeLabels(expectedStreamLbls, expectedStucturedMetadataLbls, expectedParsedLbls)

	actual = b.LabelsResult()
	assertLabelResult(t, expected, actual)
	assert.Equal(t, expectedStreamLbls, actual.Stream())
	assert.Equal(t, expectedStucturedMetadataLbls, actual.StructuredMetadata())
	assert.Equal(t, expectedParsedLbls, actual.Parsed())
}

func TestLabelsBuilder_UnsortedLabels(t *testing.T) {
	strs := []string{
		"namespace", "loki",
		"cluster", "us-central1",
		"toreplace", "fuzz",
	}
	lbs := labels.FromStrings(strs...)
	b := NewBaseLabelsBuilder().ForLabels(lbs, labels.StableHash(lbs))
	b.add[StructuredMetadataLabel] = []labels.Label{{Name: "toreplace", Value: "buzz"}, {Name: "fzz", Value: "bzz"}}
	b.add[ParsedLabel] = []labels.Label{{Name: "pzz", Value: "pvzz"}}
	expected := []labels.Label{{Name: "cluster", Value: "us-central1"}, {Name: "namespace", Value: "loki"}, {Name: "fzz", Value: "bzz"}, {Name: "toreplace", Value: "buzz"}, {Name: "pzz", Value: "pvzz"}}
	actual := b.UnsortedLabels(nil)
	require.ElementsMatch(t, expected, actual)

	b.Reset()
	b.add[StructuredMetadataLabel] = []labels.Label{{Name: "fzz", Value: "bzz"}}
	b.add[ParsedLabel] = []labels.Label{{Name: "toreplace", Value: "buzz"}, {Name: "pzz", Value: "pvzz"}}
	expected = []labels.Label{{Name: "cluster", Value: "us-central1"}, {Name: "namespace", Value: "loki"}, {Name: "fzz", Value: "bzz"}, {Name: "toreplace", Value: "buzz"}, {Name: "pzz", Value: "pvzz"}}
	actual = b.UnsortedLabels(nil)
	sortLabelSlice(expected)
	sortLabelSlice(actual)
	assert.Equal(t, expected, actual)

	b.Reset()
	b.add[StructuredMetadataLabel] = []labels.Label{{Name: "fzz", Value: "bzz"}, {Name: "toreplacezz", Value: "test"}}
	b.add[ParsedLabel] = []labels.Label{{Name: "toreplacezz", Value: "buzz"}, {Name: "pzz", Value: "pvzz"}}
	expected = []labels.Label{{Name: "cluster", Value: "us-central1"}, {Name: "namespace", Value: "loki"}, {Name: "fzz", Value: "bzz"}, {Name: "toreplace", Value: "fuzz"}, {Name: "pzz", Value: "pvzz"}, {Name: "toreplacezz", Value: "buzz"}}
	actual = b.UnsortedLabels(nil)
	sortLabelSlice(expected)
	sortLabelSlice(actual)
	assert.Equal(t, expected, actual)
}

func sortLabelSlice(l []labels.Label) {
	slices.SortFunc(l, func(a, b labels.Label) int {
		return strings.Compare(a.Name, b.Name)
	})
}

func TestLabelsBuilder_GroupedLabelsResult(t *testing.T) {
	strs := []string{"namespace", "loki",
		"job", "us-central1/loki",
		"cluster", "us-central1"}
	lbs := labels.FromStrings(strs...)
	b := NewBaseLabelsBuilderWithGrouping([]string{"namespace"}, nil, false, false).ForLabels(lbs, labels.StableHash(lbs))
	b.Reset()
	assertLabelResult(t, labels.FromStrings("namespace", "loki"), b.GroupedLabels())
	b.SetErr("err", nil)
	assertLabelResult(t, labels.FromStrings("namespace", "loki", logqlmodel.ErrorLabel, "err"), b.GroupedLabels())

	b.Reset()
	b.Set(StructuredMetadataLabel, "foo", "bar")
	b.Set(StreamLabel, "namespace", "tempo")
	b.Set(ParsedLabel, "buzz", "fuzz")
	b.Del("job")
	expected := labels.FromStrings("namespace", "tempo")
	assertLabelResult(t, expected, b.GroupedLabels())
	// cached.
	assertLabelResult(t, expected, b.GroupedLabels())

	b = NewBaseLabelsBuilderWithGrouping([]string{"job"}, nil, false, false).ForLabels(lbs, labels.StableHash(lbs))
	assertLabelResult(t, labels.FromStrings("job", "us-central1/loki"), b.GroupedLabels())
	assertLabelResult(t, labels.FromStrings("job", "us-central1/loki"), b.GroupedLabels())
	b.Del("job")
	assertLabelResult(t, labels.EmptyLabels(), b.GroupedLabels())
	b.Reset()
	b.Set(StreamLabel, "namespace", "tempo")
	assertLabelResult(t, labels.FromStrings("job", "us-central1/loki"), b.GroupedLabels())
	require.False(t, b.referencedStructuredMetadata)

	b = NewBaseLabelsBuilderWithGrouping([]string{"foo"}, nil, false, false).ForLabels(lbs, labels.StableHash(lbs))
	b.Set(StructuredMetadataLabel, "foo", "bar")
	assertLabelResult(t, labels.FromStrings("foo", "bar"), b.GroupedLabels())
	require.True(t, b.referencedStructuredMetadata)

	b = NewBaseLabelsBuilderWithGrouping([]string{"job"}, nil, true, false).ForLabels(lbs, labels.StableHash(lbs))
	b.Del("job")
	b.Set(StructuredMetadataLabel, "foo", "bar")
	b.Set(StreamLabel, "job", "something")
	expected = labels.FromStrings("namespace", "loki",
		"cluster", "us-central1",
		"foo", "bar",
	)
	assertLabelResult(t, expected, b.GroupedLabels())
	require.False(t, b.referencedStructuredMetadata)

	b = NewBaseLabelsBuilderWithGrouping([]string{"foo"}, nil, true, false).ForLabels(lbs, labels.StableHash(lbs))
	b.Set(StructuredMetadataLabel, "foo", "bar")
	expected = labels.FromStrings("namespace", "loki",
		"job", "us-central1/loki",
		"cluster", "us-central1",
	)
	assertLabelResult(t, expected, b.GroupedLabels())
	require.True(t, b.referencedStructuredMetadata)

	b = NewBaseLabelsBuilderWithGrouping(nil, nil, false, false).ForLabels(lbs, labels.StableHash(lbs))
	b.Set(StructuredMetadataLabel, "foo", "bar")
	b.Set(StreamLabel, "job", "something")
	expected = labels.FromStrings("namespace", "loki",
		"job", "something",
		"cluster", "us-central1",
		"foo", "bar",
	)
	assertLabelResult(t, expected, b.GroupedLabels())
}

func TestLabelsBuilder_GroupedLabelsResult_PipelineError(t *testing.T) {
	lbs := labels.FromStrings("namespace", "loki", "pod", "p1")

	t.Run("details set without an error keep the grouped labels and report no error label", func(t *testing.T) {
		b := NewBaseLabelsBuilderWithGrouping([]string{"pod"}, nil, false, false).ForLabels(lbs, labels.StableHash(lbs))
		b.Reset()
		b.SetErrorDetails("Malformed JSON error")

		assertLabelResult(t, labels.FromStrings("pod", "p1"), b.GroupedLabels())
	})

	t.Run("noLabels reports the error instead of an empty result", func(t *testing.T) {
		b := NewBaseLabelsBuilderWithGrouping(nil, nil, false, true).ForLabels(lbs, labels.StableHash(lbs))
		b.Reset()
		b.SetErr("JSONParserErr", errors.New("Malformed JSON error"))

		assertLabelResult(t, labels.FromStrings(
			logqlmodel.ErrorLabel, "JSONParserErr",
			logqlmodel.ErrorDetailsLabel, "Malformed JSON error",
		), b.GroupedLabels())
	})

	t.Run("by() reports __preserve_error__ although it is not a group key", func(t *testing.T) {
		b := NewBaseLabelsBuilderWithGrouping([]string{"pod"}, &Hints{shouldPreserveError: true}, false, false).ForLabels(lbs, labels.StableHash(lbs))
		b.Reset()
		b.SetErr("JSONParserErr", nil)

		assertLabelResult(t, labels.FromStrings(
			"pod", "p1",
			logqlmodel.ErrorLabel, "JSONParserErr",
			logqlmodel.PreserveErrorLabel, "true",
		), b.GroupedLabels())
	})

	t.Run("noLabels reports __preserve_error__ next to the error", func(t *testing.T) {
		b := NewBaseLabelsBuilderWithGrouping(nil, &Hints{shouldPreserveError: true}, false, true).ForLabels(lbs, labels.StableHash(lbs))
		b.Reset()
		b.SetErr("JSONParserErr", nil)

		assertLabelResult(t, labels.FromStrings(
			logqlmodel.ErrorLabel, "JSONParserErr",
			logqlmodel.PreserveErrorLabel, "true",
		), b.GroupedLabels())
	})

	t.Run("by() ignores a __preserve_error__ stream label, so a stream cannot switch off the failure", func(t *testing.T) {
		base := labels.FromStrings(logqlmodel.PreserveErrorLabel, "true", "pod", "p1")
		b := NewBaseLabelsBuilderWithGrouping([]string{"pod"}, nil, false, false).ForLabels(base, labels.StableHash(base))
		b.Reset()
		b.SetErr("JSONParserErr", nil)

		assertLabelResult(t, labels.FromStrings(
			"pod", "p1",
			logqlmodel.ErrorLabel, "JSONParserErr",
		), b.GroupedLabels())
	})

	t.Run("without() reports each error label once", func(t *testing.T) {
		b := NewBaseLabelsBuilderWithGrouping([]string{"pod"}, &Hints{shouldPreserveError: true}, true, false).ForLabels(lbs, labels.StableHash(lbs))
		b.Reset()
		b.SetErr("JSONParserErr", nil)

		assertLabelResult(t, labels.FromStrings(
			"namespace", "loki",
			logqlmodel.ErrorLabel, "JSONParserErr",
			logqlmodel.PreserveErrorLabel, "true",
		), b.GroupedLabels())
	})

	t.Run("the builder's details replace a parsed __error_details__", func(t *testing.T) {
		b := NewBaseLabelsBuilderWithGrouping([]string{"pod"}, nil, true, false).ForLabels(lbs, labels.StableHash(lbs))
		b.Reset()
		b.Set(ParsedLabel, logqlmodel.ErrorDetailsLabel, "from the line")
		b.SetErr("SampleExtractionErr", errors.New("bad number"))

		assertLabelResult(t, labels.FromStrings(
			"namespace", "loki",
			logqlmodel.ErrorLabel, "SampleExtractionErr",
			logqlmodel.ErrorDetailsLabel, "bad number",
		), b.GroupedLabels())
	})

	t.Run("an error with no details drops a parsed __error_details__", func(t *testing.T) {
		b := NewBaseLabelsBuilderWithGrouping([]string{"pod"}, nil, true, false).ForLabels(lbs, labels.StableHash(lbs))
		b.Reset()
		b.Set(ParsedLabel, logqlmodel.ErrorDetailsLabel, "from the line")
		b.SetErr("SampleExtractionErr", nil)

		assertLabelResult(t, labels.FromStrings(
			"namespace", "loki",
			logqlmodel.ErrorLabel, "SampleExtractionErr",
		), b.GroupedLabels())
	})

	t.Run("grouping by __error__ reports the builder's value once", func(t *testing.T) {
		b := NewBaseLabelsBuilderWithGrouping([]string{logqlmodel.ErrorLabel}, nil, false, false).ForLabels(lbs, labels.StableHash(lbs))
		b.Reset()
		b.SetErr("JSONParserErr", nil)

		assertLabelResult(t, labels.FromStrings(logqlmodel.ErrorLabel, "JSONParserErr"), b.GroupedLabels())
	})

	t.Run("the builder's error replaces a stream label named __error__", func(t *testing.T) {
		base := labels.FromStrings(logqlmodel.ErrorLabel, "frombase", "pod", "p1")
		b := NewBaseLabelsBuilderWithGrouping([]string{logqlmodel.ErrorLabel}, nil, false, false).ForLabels(base, labels.StableHash(base))
		b.Reset()
		b.SetErr("JSONParserErr", nil)

		assertLabelResult(t, labels.FromStrings(logqlmodel.ErrorLabel, "JSONParserErr"), b.GroupedLabels())
	})
}

func TestLabelsBuilder_SetErr(t *testing.T) {
	lbs := labels.FromStrings("pod", "p1")

	newBuilder := func(hints ParserHint) *LabelsBuilder {
		b := NewBaseLabelsBuilderWithGrouping(nil, hints, false, false).ForLabels(lbs, labels.StableHash(lbs))
		b.Reset()
		return b
	}

	t.Run("sets the error and its details together", func(t *testing.T) {
		b := newBuilder(nil)
		b.SetErr("JSONParserErr", errors.New("Malformed JSON error"))

		require.Equal(t, "JSONParserErr", b.GetErr())
		require.Equal(t, "Malformed JSON error", b.GetErrorDetails())
	})

	t.Run("leaves no details when passed a nil error", func(t *testing.T) {
		b := newBuilder(nil)
		b.SetErr("JSONParserErr", nil)

		require.False(t, b.HasErrorDetails())
	})

	t.Run("reports __preserve_error__ when the query asked to keep the errored lines", func(t *testing.T) {
		b := newBuilder(&Hints{shouldPreserveError: true})
		b.SetErr("JSONParserErr", nil)

		assertLabelResult(t, labels.FromStrings(
			"pod", "p1",
			logqlmodel.ErrorLabel, "JSONParserErr",
			logqlmodel.PreserveErrorLabel, "true",
		), b.LabelsResult())
	})

	t.Run("reports no __preserve_error__ when the query did not ask for the errored lines", func(t *testing.T) {
		b := newBuilder(nil)
		b.SetErr("JSONParserErr", nil)

		require.False(t, b.LabelsResult().Labels().Has(logqlmodel.PreserveErrorLabel))
	})

	t.Run("a replacing error drops the details of the one before it", func(t *testing.T) {
		b := newBuilder(nil)
		b.SetErr("JSONParserErr", errors.New("Malformed JSON error"))
		b.SetErr("SampleExtractionErr", nil)

		require.Equal(t, "SampleExtractionErr", b.GetErr())
		require.False(t, b.HasErrorDetails())
	})

	t.Run("drops a __preserve_error__ label the line carried, whichever category it came in, when the query doesn't filter on __error__", func(t *testing.T) {
		for _, category := range []LabelCategory{StreamLabel, StructuredMetadataLabel, ParsedLabel} {
			b := newBuilder(nil)
			b.Set(category, logqlmodel.PreserveErrorLabel, "true")
			b.SetErr("SampleExtractionErr", nil)

			require.False(t, b.LabelsResult().Labels().Has(logqlmodel.PreserveErrorLabel))
			require.False(t, b.GroupedLabels().Labels().Has(logqlmodel.PreserveErrorLabel))
		}
	})

	t.Run("drops a __preserve_error__ label the line carried under a grouping, when the query doesn't filter on __error__", func(t *testing.T) {
		for _, category := range []LabelCategory{StreamLabel, StructuredMetadataLabel, ParsedLabel} {
			for _, without := range []bool{true, false} {
				b := NewBaseLabelsBuilderWithGrouping([]string{"pod"}, nil, without, false).ForLabels(lbs, labels.StableHash(lbs))
				b.Reset()
				b.Set(category, logqlmodel.PreserveErrorLabel, "true")
				b.SetErr("SampleExtractionErr", nil)

				require.False(t, b.GroupedLabels().Labels().Has(logqlmodel.PreserveErrorLabel))
			}
		}
	})

	t.Run("ResetError() drops the __preserve__error__", func(t *testing.T) {
		b := newBuilder(&Hints{shouldPreserveError: true})
		// The details outlive ResetError, and the parsed label defeats the memoized fast path, so
		// the result is built rather than returned whole.
		b.Set(ParsedLabel, "ok", "1")
		b.SetErr("JSONParserErr", errors.New("boom"))
		b.ResetError()

		require.False(t, b.LabelsResult().Labels().Has(logqlmodel.PreserveErrorLabel))
	})
}

func assertLabelResult(t *testing.T, lbs labels.Labels, res LabelsResult) {
	t.Helper()
	require.Equal(t,
		lbs,
		res.Labels(),
	)
	require.Equal(t,
		labels.StableHash(lbs),
		res.Hash(),
	)
	require.Equal(t,
		lbs.String(),
		res.String(),
	)
}

func mergeLabels(streamLabels, structuredMetadataLabels, parsedLabels labels.Labels) labels.Labels {
	builder := labels.NewBuilder(streamLabels)

	structuredMetadataLabels.Range(func(l labels.Label) {
		builder.Set(l.Name, l.Value)
	})

	parsedLabels.Range(func(l labels.Label) {
		builder.Set(l.Name, l.Value)
	})

	return builder.Labels()
}

// benchmark streamLineSampleExtractor.Process method
func BenchmarkStreamLineSampleExtractor_Process(b *testing.B) {
	// Setup some test data
	baseLabels := labels.FromStrings(
		"namespace", "prod",
		"cluster", "us-east-1",
		"pod", "my-pod-123",
		"container", "main",
		"stream", "stdout",
	)

	structuredMeta := labels.FromStrings(
		"level", "info",
		"caller", "http.go:42",
		"user", "john",
		"trace_id", "abc123",
	)

	testLine := []byte(`{"timestamp":"2024-01-01T00:00:00Z","level":"info","message":"test message","duration_ms":150}`)

	// JSON parsing + filtering + label extraction
	matcher := labels.MustNewMatcher(labels.MatchEqual, "level", "info")
	filter := NewStringLabelFilter(matcher)
	stages := []Stage{
		NewJSONParser(false),
		filter,
	}
	ex, err := NewLineSampleExtractor(CountExtractor, stages, []string{}, false, false)
	require.NoError(b, err)
	streamEx := ex.ForStream(baseLabels)
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, _ = streamEx.Process(time.Now().UnixNano(), testLine, structuredMeta)
	}
}

func BenchmarkLabelsBuilder_Add(b *testing.B) {
	sizes := []int{10, 100, 1000, 10000}

	for _, size := range sizes {
		b.Run(fmt.Sprintf("size_%d", size), func(b *testing.B) {
			// Pre-generate labels that should be added
			newB := labels.NewScratchBuilder(size)
			for i := 0; i < size; i++ {
				newB.Add(fmt.Sprintf("label_%d", i), fmt.Sprintf("value_%d", i))
			}
			newLabels := newB.Labels()

			lbs := labels.FromStrings("already", "in")
			builder := NewBaseLabelsBuilder().ForLabels(lbs, labels.StableHash(lbs))

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				builder.Reset()
				builder.Add(StructuredMetadataLabel, newLabels)
			}
		})
	}
}

func TestBaseLabelsBuilder_ForLabels_HashCollisionKeepsResultsDistinct(t *testing.T) {
	a, b := collidingLabelPair(t)
	bb := NewBaseLabelsBuilder()

	ra := bb.ForLabels(a, bb.Hash(a)).currentResult
	rb := bb.ForLabels(b, bb.Hash(b)).currentResult
	require.True(t, labels.Equal(a, ra.Stream()))
	require.True(t, labels.Equal(b, rb.Stream()))
}

// collidingLabelPair returns two distinct label sets that collide on labels.StableHash.
func collidingLabelPair(t *testing.T) (labels.Labels, labels.Labels) {
	t.Helper()
	a := labels.FromStrings("cluster", "prod", "namespace", "team", "pod", "39ae2fcfd732c147")
	b := labels.FromStrings("cluster", "prod", "namespace", "team", "pod", "f35246e8ca75a99b")
	require.NotEqual(t, a.String(), b.String())
	require.Equal(t, labels.StableHash(a), labels.StableHash(b), "collision fixture no longer collides on StableHash")
	return a, b
}
