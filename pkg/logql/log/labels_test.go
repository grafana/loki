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
	b.Set("foo", "bar")
	b.Set("bar", "buzz")
	b.Del("foo")
	_, ok := b.Get("foo")
	require.False(t, ok)
	v, ok := b.Get("bar")
	require.True(t, ok)
	require.Equal(t, "buzz", v)
	v, ok = b.Get("already")
	require.True(t, ok)
	require.Equal(t, "in", v)
	b.Del("bar")
	_, ok = b.Get("bar")
	require.False(t, ok)
	b.Del("already")
	_, ok = b.Get("already")
	require.False(t, ok)
}

func TestLabelsBuilder_LabelsError(t *testing.T) {
	lbs := labels.FromStrings("already", "in")
	b := NewBaseLabelsBuilder().ForLabels(lbs, lbs.Hash())
	b.Reset()
	b.SetErr("err")
	lbsWithErr := b.LabelsResult().Labels()
	require.Equal(
		t,
		labels.FromStrings(logqlmodel.ErrorLabel, "err",
			"already", "in",
		),
		lbsWithErr,
	)
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

	b.Set("foo", "bar")
	b.Set("namespace", "tempo")
	b.Set("buzz", "fuzz")
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
	b.Set("foo", "bar")
	b.Set("namespace", "tempo")
	b.Set("buzz", "fuzz")
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
	b.Set("namespace", "tempo")
	assertLabelResult(t, labels.FromStrings("job", "us-central1/loki"), b.GroupedLabels())

	b = NewBaseLabelsBuilderWithGrouping([]string{"job"}, nil, true, false).ForLabels(lbs, lbs.Hash())
	b.Del("job")
	b.Set("foo", "bar")
	b.Set("job", "something")
	expected = labels.FromStrings("namespace", "loki",
		"cluster", "us-central1",
		"foo", "bar",
	)
	assertLabelResult(t, expected, b.GroupedLabels())

	b = NewBaseLabelsBuilderWithGrouping(nil, nil, false, false).ForLabels(lbs, lbs.Hash())
	b.Set("foo", "bar")
	b.Set("job", "something")
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
