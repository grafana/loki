package log

import (
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
	b.Set(StructuredMetadataLabel, []byte("foo"), []byte("bar"))
	b.Set(ParsedLabel, []byte("bar"), []byte("buzz"))

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
	require.Equal(t, "buzz", string(v))
	require.Equal(t, ParsedLabel, category)
	v, category, ok = b.GetWithCategory("already")
	require.True(t, ok)
	require.Equal(t, "in", string(v))
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
	b.SetErr("err")
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
	b.SetErr("err")
	withErr := labels.FromStrings(append(strs, logqlmodel.ErrorLabel, "err")...)
	assertLabelResult(t, withErr, b.LabelsResult())

	b.Set(StructuredMetadataLabel, []byte("foo"), []byte("bar"))
	b.Set(StreamLabel, []byte("namespace"), []byte("tempo"))
	b.Set(ParsedLabel, []byte("buzz"), []byte("fuzz"))
	b.Set(ParsedLabel, []byte("ToReplace"), []byte("other"))
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
	b.Set(StreamLabel, []byte("namespace"), []byte("tempo"))
	b.Set(StreamLabel, []byte("bazz"), []byte("tazz"))
	b.Set(StructuredMetadataLabel, []byte("bazz"), []byte("sazz"))
	b.Set(ParsedLabel, []byte("ToReplace"), []byte("other"))

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
	b.Set(StructuredMetadataLabel, []byte("stzz"), []byte("stvzz"))
	b.Set(ParsedLabel, []byte("toreplace"), []byte("buzz"))
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
	b.Set(StructuredMetadataLabel, []byte("stzz"), []byte("stvzz"))
	b.Set(StructuredMetadataLabel, []byte("toreplace"), []byte("muzz"))
	b.Set(ParsedLabel, []byte("toreplace"), []byte("buzz"))
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
	b.Set(StructuredMetadataLabel, []byte("toreplace"), []byte("muzz"))
	b.Set(ParsedLabel, []byte("stzz"), []byte("stvzz"))
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
	b.Set(ParsedLabel, []byte("toreplace"), []byte("puzz"))
	b.Set(StructuredMetadataLabel, []byte("stzz"), []byte("stvzzz"))
	b.Set(StructuredMetadataLabel, []byte("toreplace"), []byte("muzz"))
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
	b.Set(ParsedLabel, []byte("stzz"), []byte("stvzzz"))
	b.Set(StructuredMetadataLabel, []byte("toreplace"), []byte("muzz"))
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
	b.add[StructuredMetadataLabel] = newColumnarLabelsFromStrings("toreplace", "buzz", "fzz", "bzz")
	b.add[ParsedLabel] = newColumnarLabelsFromStrings("pzz", "pvzz")
	expected := []labels.Label{{Name: "cluster", Value: "us-central1"}, {Name: "namespace", Value: "loki"}, {Name: "fzz", Value: "bzz"}, {Name: "toreplace", Value: "buzz"}, {Name: "pzz", Value: "pvzz"}}
	actual := b.UnsortedLabels(nil)
	require.ElementsMatch(t, expected, actual)

	b.Reset()
	b.add[StructuredMetadataLabel] = newColumnarLabelsFromStrings("fzz", "bzz")
	b.add[ParsedLabel] = newColumnarLabelsFromStrings("toreplace", "buzz", "pzz", "pvzz")
	expected = []labels.Label{{Name: "cluster", Value: "us-central1"}, {Name: "namespace", Value: "loki"}, {Name: "fzz", Value: "bzz"}, {Name: "toreplace", Value: "buzz"}, {Name: "pzz", Value: "pvzz"}}
	actual = b.UnsortedLabels(nil)
	sortLabelSlice(expected)
	sortLabelSlice(actual)
	assert.Equal(t, expected, actual)

	b.Reset()
	b.add[StructuredMetadataLabel] = newColumnarLabelsFromStrings("fzz", "bzz", "toreplacezz", "test")
	b.add[ParsedLabel] = newColumnarLabelsFromStrings("toreplacezz", "buzz", "pzz", "pvzz")
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
	b.SetErr("err")
	withErr := labels.FromStrings(append(strs, logqlmodel.ErrorLabel, "err")...)
	assertLabelResult(t, withErr, b.GroupedLabels())

	b.Reset()
	b.Set(StructuredMetadataLabel, []byte("foo"), []byte("bar"))
	b.Set(StreamLabel, []byte("namespace"), []byte("tempo"))
	b.Set(ParsedLabel, []byte("buzz"), []byte("fuzz"))
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
	b.Set(StreamLabel, []byte("namespace"), []byte("tempo"))
	assertLabelResult(t, labels.FromStrings("job", "us-central1/loki"), b.GroupedLabels())
	require.False(t, b.referencedStructuredMetadata)

	b = NewBaseLabelsBuilderWithGrouping([]string{"foo"}, nil, false, false).ForLabels(lbs, labels.StableHash(lbs))
	b.Set(StructuredMetadataLabel, []byte("foo"), []byte("bar"))
	assertLabelResult(t, labels.FromStrings("foo", "bar"), b.GroupedLabels())
	require.True(t, b.referencedStructuredMetadata)

	b = NewBaseLabelsBuilderWithGrouping([]string{"job"}, nil, true, false).ForLabels(lbs, labels.StableHash(lbs))
	b.Del("job")
	b.Set(StructuredMetadataLabel, []byte("foo"), []byte("bar"))
	b.Set(StreamLabel, []byte("job"), []byte("something"))
	expected = labels.FromStrings("namespace", "loki",
		"cluster", "us-central1",
		"foo", "bar",
	)
	assertLabelResult(t, expected, b.GroupedLabels())
	require.False(t, b.referencedStructuredMetadata)

	b = NewBaseLabelsBuilderWithGrouping([]string{"foo"}, nil, true, false).ForLabels(lbs, labels.StableHash(lbs))
	b.Set(StructuredMetadataLabel, []byte("foo"), []byte("bar"))
	expected = labels.FromStrings("namespace", "loki",
		"job", "us-central1/loki",
		"cluster", "us-central1",
	)
	assertLabelResult(t, expected, b.GroupedLabels())
	require.True(t, b.referencedStructuredMetadata)

	b = NewBaseLabelsBuilderWithGrouping(nil, nil, false, false).ForLabels(lbs, labels.StableHash(lbs))
	b.Set(StructuredMetadataLabel, []byte("foo"), []byte("bar"))
	b.Set(StreamLabel, []byte("job"), []byte("something"))
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
