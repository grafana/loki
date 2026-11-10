package logql

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logql/syntax"
)

func TestParentReads_ShouldReadEverythingByDefault(t *testing.T) {
	var req ParentReads
	require.True(t, req.Labels().All())
	require.True(t, req.Identity())
	require.True(t, req.Values())
	require.Equal(t, readsEverything(), req)
	require.Equal(t, readsEverything(), newParentReads(allLabels(), true, true))
}

func TestParentReads_ShouldKeepTheAxesApart(t *testing.T) {
	req := newParentReads(onlyLabels("app"), false, true)
	require.Equal(t, onlyLabels("app"), req.Labels())
	require.False(t, req.Identity())
	require.True(t, req.Values())

	req = newParentReads(noLabels(), true, false)
	require.True(t, req.Labels().None())
	require.True(t, req.Identity())
	require.False(t, req.Values())

	require.Equal(t, newParentReads(labelsWithout("pod"), true, false), req.withLabels(labelsWithout("pod")))
}

func TestLabelReads_ShouldReadTheNamedLabels(t *testing.T) {
	for name, tc := range map[string]struct {
		labels   LabelReads
		wantAll  bool
		wantNone bool
		reads    []string
		skips    []string
	}{
		"all":                 {labels: allLabels(), wantAll: true, reads: []string{"app", "pod"}},
		"none":                {labels: noLabels(), wantNone: true, skips: []string{"app", "pod"}},
		"only":                {labels: onlyLabels("pod", "app"), reads: []string{"app", "pod"}, skips: []string{"namespace"}},
		"without":             {labels: labelsWithout("pod"), reads: []string{"app"}, skips: []string{"pod"}},
		"only nothing":        {labels: onlyLabels(), wantNone: true, skips: []string{"app"}},
		"without nothing":     {labels: labelsWithout(), wantAll: true, reads: []string{"app"}},
		"grouping by":         {labels: groupingLabels(&syntax.Grouping{Groups: []string{"app"}}), reads: []string{"app"}, skips: []string{"pod"}},
		"grouping by ()":      {labels: groupingLabels(&syntax.Grouping{}), wantNone: true, skips: []string{"app"}},
		"grouping without":    {labels: groupingLabels(&syntax.Grouping{Groups: []string{"pod"}, Without: true}), reads: []string{"app"}, skips: []string{"pod"}},
		"grouping without ()": {labels: groupingLabels(&syntax.Grouping{Without: true}), wantAll: true, reads: []string{"app"}},
		"no grouping":         {labels: groupingLabels(nil), wantAll: true, reads: []string{"app"}},
	} {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, tc.wantAll, tc.labels.All())
			require.Equal(t, tc.wantNone, tc.labels.None())
			for _, l := range tc.reads {
				require.True(t, tc.labels.Reads(l), l)
			}
			for _, l := range tc.skips {
				require.False(t, tc.labels.Reads(l), l)
			}
		})
	}
}

func TestLabelReads_ShouldCompareEqualWhateverTheOrderOfNames(t *testing.T) {
	require.Equal(t, onlyLabels("app", "pod"), onlyLabels("pod", "app", "pod"))
	require.Equal(t, labelsWithout("app", "pod"), labelsWithout("pod", "app", "app"))
	require.Equal(t, noLabels(), onlyLabels([]string{}...))
	require.Equal(t, allLabels(), labelsWithout([]string{}...))
}

func TestLabelReads_ShouldNotChangeTheGroupsOfAGrouping(t *testing.T) {
	g := &syntax.Grouping{Groups: []string{"pod", "app", "pod"}}
	require.Equal(t, onlyLabels("app", "pod"), groupingLabels(g))
	require.Equal(t, []string{"pod", "app", "pod"}, g.Groups)
}

func TestLabelReads_ShouldUnionTheLabelsOfBothSides(t *testing.T) {
	for name, tc := range map[string]struct {
		l, o LabelReads
		want LabelReads
	}{
		"only and only":         {l: onlyLabels("app"), o: onlyLabels("pod", "app"), want: onlyLabels("app", "pod")},
		"none and only":         {l: noLabels(), o: onlyLabels("app"), want: onlyLabels("app")},
		"none and none":         {l: noLabels(), o: noLabels(), want: noLabels()},
		"all and only":          {l: allLabels(), o: onlyLabels("app"), want: allLabels()},
		"all and none":          {l: allLabels(), o: noLabels(), want: allLabels()},
		"without and without":   {l: labelsWithout("app", "pod"), o: labelsWithout("pod", "namespace"), want: labelsWithout("pod")},
		"without and only":      {l: labelsWithout("app", "pod"), o: onlyLabels("pod", "namespace"), want: labelsWithout("app")},
		"only and without":      {l: onlyLabels("pod", "namespace"), o: labelsWithout("app", "pod"), want: labelsWithout("app")},
		"without all of only":   {l: labelsWithout("app"), o: onlyLabels("app"), want: allLabels()},
		"without and all":       {l: labelsWithout("app"), o: allLabels(), want: allLabels()},
		"disjoint without sets": {l: labelsWithout("app"), o: labelsWithout("pod"), want: allLabels()},
	} {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, tc.want, tc.l.union(tc.o))
			require.Equal(t, tc.want, tc.o.union(tc.l))
		})
	}
}

func TestLabelReads_ShouldNotChangeEitherSideOfAUnion(t *testing.T) {
	l, o := labelsWithout("app", "pod"), onlyLabels("pod")
	_ = l.union(o)
	require.Equal(t, labelsWithout("app", "pod"), l)
	require.Equal(t, onlyLabels("pod"), o)
}
