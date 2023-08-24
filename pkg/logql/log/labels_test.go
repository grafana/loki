package log

import (
	"testing"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/logqlmodel"
)

func TestLabelsBuilder_Get(t *testing.T) {
	lbs := labels.FromStrings("already", "in")
	b := NewBaseLabelsBuilder().ForLabels(lbs, lbs.Hash())
	b.Reset()
	b.Set(StructuredMetadataLabel, "foo", "bar")
	b.Set(ParsedLabel, "bar", "buzz")
	b.Del("foo")
	_, _, ok := b.GetWithCategory("foo")
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
	lbsWithErr := b.LabelsResult().Labels()
	lbsCatWithErr := b.CategorizedLabelsResult()

	expectedLbs := labels.FromStrings(
		logqlmodel.ErrorLabel, "err",
		"already", "in",
	)
	require.Equal(t, expectedLbs, lbsWithErr)
	require.Equal(t, lbsCatWithErr.Stream().Labels(), labels.FromStrings("already", "in"))
	require.Equal(t, lbsCatWithErr.StructuredMetadata().Labels(), labels.EmptyLabels())
	require.Equal(t, lbsCatWithErr.Parsed().Labels(), labels.FromStrings(logqlmodel.ErrorLabel, "err"))

	// make sure the original labels is unchanged.
	require.Equal(t, labels.FromStrings("already", "in"), lbs)
}

func TestLabelsBuilder_LabelsResult(t *testing.T) {
	strs := []string{"namespace", "loki",
		"job", "us-central1/loki",
		"cluster", "us-central1"}
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
	b.Del("job")
	expected := labels.FromStrings(logqlmodel.ErrorLabel, "err",
		"namespace", "tempo",
		"cluster", "us-central1",
		"foo", "bar",
		"buzz", "fuzz",
	)
	assertLabelResult(t, expected, b.LabelsResult())
	// cached.
	assertLabelResult(t, expected, b.LabelsResult())

	// TODO: Test categories with CategorizedLabelsResult
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

	b = NewBaseLabelsBuilderWithGrouping([]string{"job"}, nil, true, false).ForLabels(lbs, lbs.Hash())
	b.Del("job")
	b.Set(StructuredMetadataLabel, "foo", "bar")
	b.Set(StreamLabel, "job", "something")
	expected = labels.FromStrings("namespace", "loki",
		"cluster", "us-central1",
		"foo", "bar",
	)
	assertLabelResult(t, expected, b.GroupedLabels())

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

// func TestLabelsBuilder_CategorizedLabels(t *testing.T) {
// 	streamLbls := labels.FromStrings(
// 		"namespace", "loki",
// 		"job", "us-central1/loki",
// 	)
// 	builder := NewBaseLabelsBuilder().ForLabels(streamLbls, streamLbls.Hash())
//
// 	builder.Reset()
// 	builder.StructuredMetadata.Set("traceID", "123")
// 	builder.StructuredMetadata.Set("user", "admin")
// 	builder.Parsed.Set("foo", "a")
// 	builder.Parsed.Set("bar", "b")
// 	expected := labels.FromStrings(
// 		"namespace", "loki",
// 		"job", "us-central1/loki",
// 		"traceID", "123",
// 		"user", "admin",
// 		"foo", "a",
// 		"bar", "b",
// 	)
// 	assertLabelResult(t, expected, builder.LabelsResult())
//
// 	builder.Del("namespace")
// 	builder.Del("traceID")
// 	builder.Del("foo")
//
// 	expected = labels.FromStrings(
// 		"job", "us-central1/loki",
// 		"user", "admin",
// 		"bar", "b",
// 	)
// 	assertLabelResult(t, expected, builder.LabelsResult())
//
// 	_, ok := builder.Get("namespace")
// 	require.False(t, ok)
// 	v, ok := builder.Get("job")
// 	require.True(t, ok)
// 	require.Equal(t, "us-central1/loki", v)
// 	_, ok = builder.Get("traceID")
// 	require.False(t, ok)
// 	v, ok = builder.Get("user")
// 	require.True(t, ok)
// 	require.Equal(t, "admin", v)
// 	_, ok = builder.Get("foo")
// 	require.False(t, ok)
// 	v, ok = builder.Get("bar")
// 	require.True(t, ok)
// 	require.Equal(t, "b", v)
//
// }
