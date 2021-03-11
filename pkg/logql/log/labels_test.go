package log

import (
	"sort"
	"testing"

	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/stretchr/testify/require"
)

func TestLabelsBuilder_Get(t *testing.T) {
	lbs := labels.Labels{labels.Label{Name: "already", Value: "in"}}
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
	lbs := labels.Labels{labels.Label{Name: "already", Value: "in"}}
	b := NewBaseLabelsBuilder().ForLabels(lbs, lbs.Hash())
	b.Reset()
	b.SetErr("err")
	lbsWithErr := b.Labels()
	require.Equal(
		t,
		labels.Labels{
			labels.Label{Name: ErrorLabel, Value: "err"},
			labels.Label{Name: "already", Value: "in"},
		},
		lbsWithErr,
	)
	// make sure the original labels is unchanged.
	require.Equal(t, labels.Labels{labels.Label{Name: "already", Value: "in"}}, lbs)
}

func TestLabelsBuilder_LabelsResult(t *testing.T) {
	lbs := labels.Labels{
		labels.Label{Name: "namespace", Value: "loki"},
		labels.Label{Name: "job", Value: "us-central1/loki"},
		labels.Label{Name: "cluster", Value: "us-central1"},
	}
	sort.Sort(lbs)
	b := NewBaseLabelsBuilder().ForLabels(lbs, lbs.Hash())
	b.Reset()
	assertLabelResult(t, lbs, b.LabelsResult())
	b.SetErr("err")
	withErr := append(lbs, labels.Label{Name: ErrorLabel, Value: "err"})
	sort.Sort(withErr)
	assertLabelResult(t, withErr, b.LabelsResult())

	b.Set("foo", "bar")
	b.Set("namespace", "tempo")
	b.Set("buzz", "fuzz")
	b.Del("job")
	expected := labels.Labels{
		labels.Label{Name: ErrorLabel, Value: "err"},
		labels.Label{Name: "namespace", Value: "tempo"},
		labels.Label{Name: "cluster", Value: "us-central1"},
		labels.Label{Name: "foo", Value: "bar"},
		labels.Label{Name: "buzz", Value: "fuzz"},
	}
	sort.Sort(expected)
	assertLabelResult(t, expected, b.LabelsResult())
	// cached.
	assertLabelResult(t, expected, b.LabelsResult())
}

func TestLabelsBuilder_GroupedLabelsResult(t *testing.T) {
	lbs := labels.Labels{
		labels.Label{Name: "namespace", Value: "loki"},
		labels.Label{Name: "job", Value: "us-central1/loki"},
		labels.Label{Name: "cluster", Value: "us-central1"},
	}
	sort.Sort(lbs)
	b := NewBaseLabelsBuilderWithGrouping([]string{"namespace"}, nil, false, false).ForLabels(lbs, lbs.Hash())
	b.Reset()
	assertLabelResult(t, labels.Labels{labels.Label{Name: "namespace", Value: "loki"}}, b.GroupedLabels())
	b.SetErr("err")
	withErr := append(lbs, labels.Label{Name: ErrorLabel, Value: "err"})
	sort.Sort(withErr)
	assertLabelResult(t, withErr, b.GroupedLabels())

	b.Reset()
	b.Set("foo", "bar")
	b.Set("namespace", "tempo")
	b.Set("buzz", "fuzz")
	b.Del("job")
	expected := labels.Labels{
		labels.Label{Name: "namespace", Value: "tempo"},
	}
	sort.Sort(expected)
	assertLabelResult(t, expected, b.GroupedLabels())
	// cached.
	assertLabelResult(t, expected, b.GroupedLabels())

	b = NewBaseLabelsBuilderWithGrouping([]string{"job"}, nil, false, false).ForLabels(lbs, lbs.Hash())
	assertLabelResult(t, labels.Labels{labels.Label{Name: "job", Value: "us-central1/loki"}}, b.GroupedLabels())
	assertLabelResult(t, labels.Labels{labels.Label{Name: "job", Value: "us-central1/loki"}}, b.GroupedLabels())
	b.Del("job")
	assertLabelResult(t, labels.Labels{}, b.GroupedLabels())
	b.Reset()
	b.Set("namespace", "tempo")
	assertLabelResult(t, labels.Labels{labels.Label{Name: "job", Value: "us-central1/loki"}}, b.GroupedLabels())

	b = NewBaseLabelsBuilderWithGrouping([]string{"job"}, nil, true, false).ForLabels(lbs, lbs.Hash())
	b.Del("job")
	b.Set("foo", "bar")
	b.Set("job", "something")
	expected = labels.Labels{
		labels.Label{Name: "namespace", Value: "loki"},
		labels.Label{Name: "cluster", Value: "us-central1"},
		labels.Label{Name: "foo", Value: "bar"},
	}
	sort.Sort(expected)
	assertLabelResult(t, expected, b.GroupedLabels())

	b = NewBaseLabelsBuilderWithGrouping(nil, nil, false, false).ForLabels(lbs, lbs.Hash())
	b.Set("foo", "bar")
	b.Set("job", "something")
	expected = labels.Labels{
		labels.Label{Name: "namespace", Value: "loki"},
		labels.Label{Name: "job", Value: "something"},
		labels.Label{Name: "cluster", Value: "us-central1"},
		labels.Label{Name: "foo", Value: "bar"},
	}
	sort.Sort(expected)
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
