package querier

import (
	"context"
	"maps"
	"slices"
	"sync"
	"testing"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/storage/chunk"

	"github.com/grafana/loki/pkg/push"
)

func TestDataObjStore_LBACPushdown(t *testing.T) {
	const query = `sum by (app) (count_over_time({env=~".+"}[1m]))`

	readStreams := func(store *testDataObjStore) []string {
		var out []string
		for _, row := range store.selectSamples(testCtx(t), query, at(0), at(10)) {
			out = append(out, row.Labels)
		}
		return out
	}

	streams := []logproto.Stream{
		{Labels: `{app="a", env="prod"}`, Entries: []push.Entry{entry(t, 1, "one")}},
		{Labels: `{app="b", env="prod", secret="true"}`, Entries: []push.Entry{entry(t, 1, "two")}},
		{Labels: `{app="c", env="dev", secret="true"}`, Entries: []push.Entry{entry(t, 1, "three")}},
		{Labels: `{app="d", env="dev"}`, Entries: []push.Entry{entry(t, 1, "four")}},
		{Labels: `{app="e", env="dev", team="x"}`, Entries: []push.Entry{entry(t, 1, "five")}},
	}

	t.Run("pushes the matchers down, so the filterer is asked only about the streams that match them", func(t *testing.T) {
		filterer := newMatcherFilterer(t, `{secret="true", env="dev"}`)
		store := newTestDataObjStore(t, streams, withStreamFilterer(filterer))

		got := readStreams(store)

		require.Equal(t, []string{`{app="c"}`}, got)
		require.Equal(t, []string{"c"}, filterer.asked.apps())
	})

	t.Run("keeps the matchers that match the empty string out of the push-down", func(t *testing.T) {
		filterer := newMatcherFilterer(t, `{team=~".*", secret!="true", env="dev"}`)
		store := newTestDataObjStore(t, streams, withStreamFilterer(filterer))

		got := readStreams(store)

		require.Equal(t, []string{`{app="d"}`, `{app="e"}`}, got)
		require.Equal(t, []string{"c", "d", "e"}, filterer.asked.apps())
	})

	t.Run("asks the filterer about every stream when it states no matcher", func(t *testing.T) {
		filterer := newMatcherFilterer(t, `{secret="true", env="dev"}`)
		store := newTestDataObjStore(t, streams, withStreamFilterer(withoutPushdown{filterer}))

		got := readStreams(store)

		require.Equal(t, []string{`{app="c"}`}, got)
		require.Equal(t, []string{"a", "b", "c", "d", "e"}, filterer.asked.apps())
	})

	t.Run("fails a selector with no stream matcher of its own even when the filterer states matchers", func(t *testing.T) {
		store := newTestDataObjStore(t, streams, withStreamFilterer(newMatcherFilterer(t, `{env="dev"}`)))

		it, err := store.selectSamplesIter(testCtx(t), `sum by (app) (count_over_time({__name__=~".+"}[1m]))`, at(0), at(10))
		require.NoError(t, err)
		require.False(t, it.Next())
		require.EqualError(t, it.Err(), "resolving data object sections: at least one stream matcher is required")
		require.Error(t, it.Close())
	})

	testCases := []struct {
		name     string
		selector string
		want     []string
	}{
		{
			name:     "an inequality keeps a stream that lacks the label",
			selector: `{secret!="true"}`,
			want:     []string{`{app="a"}`, `{app="d"}`, `{app="e"}`},
		},
		{
			name:     "an equality keeps only the streams with the value",
			selector: `{secret="true"}`,
			want:     []string{`{app="b"}`, `{app="c"}`},
		},
		{
			name:     "an equality with an empty value keeps the streams that lack the label",
			selector: `{team=""}`,
			want:     []string{`{app="a"}`, `{app="b"}`, `{app="c"}`, `{app="d"}`},
		},
		{
			name:     "an inequality with an empty value keeps only the streams that have the label",
			selector: `{team!=""}`,
			want:     []string{`{app="e"}`},
		},
		{
			name:     "a regular expression that matches every value keeps every stream",
			selector: `{team=~".*"}`,
			want:     []string{`{app="a"}`, `{app="b"}`, `{app="c"}`, `{app="d"}`, `{app="e"}`},
		},
		{
			name:     "a regular expression that needs a value keeps only the streams that have the label",
			selector: `{team=~".+"}`,
			want:     []string{`{app="e"}`},
		},
		{
			name:     "a regular expression that can match an empty value keeps the streams that lack the label",
			selector: `{team=~"x|"}`,
			want:     []string{`{app="a"}`, `{app="b"}`, `{app="c"}`, `{app="d"}`, `{app="e"}`},
		},
		{
			name:     "a negated regular expression keeps the streams that lack the label",
			selector: `{team!~"x.*"}`,
			want:     []string{`{app="a"}`, `{app="b"}`, `{app="c"}`, `{app="d"}`},
		},
		{
			name:     "several matchers keep the streams that match all of them",
			selector: `{secret!="true", env="dev"}`,
			want:     []string{`{app="d"}`, `{app="e"}`},
		},
		{
			name:     "a matcher on a label that no stream has keeps no stream",
			selector: `{nope="x"}`,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name+" with and without the push-down", func(t *testing.T) {
			filterer := newMatcherFilterer(t, testCase.selector)
			pushed := newTestDataObjStore(t, streams, withStreamFilterer(filterer))
			planned := newTestDataObjStore(t, streams, withStreamFilterer(withoutPushdown{filterer}))

			require.Equal(t, testCase.want, readStreams(pushed), "with the push-down")
			require.Equal(t, testCase.want, readStreams(planned), "without the push-down")
		})
	}
}

// matcherFilterer admits the streams that match all of its matchers, states them for the
// push-down, and records the streams it is asked about.
type matcherFilterer struct {
	matchers []*labels.Matcher
	asked    *askedStreams
}

// newMatcherFilterer parses a selector that may match the empty string, such as {team=""}.
func newMatcherFilterer(t *testing.T, selector string) matcherFilterer {
	t.Helper()

	matchers, err := syntax.ParseMatchers(selector, false)
	require.NoError(t, err)

	return matcherFilterer{matchers: matchers, asked: &askedStreams{}}
}

func (f matcherFilterer) ForRequest(context.Context) chunk.Filterer { return f }

func (f matcherFilterer) ShouldFilter(streamLabels labels.Labels) bool {
	f.asked.add(streamLabels.Get("app"))

	for _, matcher := range f.matchers {
		if !matcher.Matches(streamLabels.Get(matcher.Name)) {
			return true
		}
	}
	return false
}

func (f matcherFilterer) RequiredLabelNames() []string { return nil }

func (f matcherFilterer) PushableLabelMatchers() []*labels.Matcher { return f.matchers }

// askedStreams collects the app label of every stream a filterer is asked about.
type askedStreams struct {
	mu   sync.Mutex
	seen map[string]struct{}
}

func (a *askedStreams) add(app string) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.seen == nil {
		a.seen = map[string]struct{}{}
	}
	a.seen[app] = struct{}{}
}

// apps returns the collected app labels in order.
func (a *askedStreams) apps() []string {
	a.mu.Lock()
	defer a.mu.Unlock()

	return slices.Sorted(maps.Keys(a.seen))
}

// withoutPushdown hides the push-down of the filterer it wraps, so the planner is the only place
// that applies it.
type withoutPushdown struct{ filterer chunk.RequestChunkFilterer }

func (f withoutPushdown) ForRequest(ctx context.Context) chunk.Filterer {
	return noPushdownFilterer{f.filterer.ForRequest(ctx)}
}

// noPushdownFilterer states no pushable matcher and leaves every other decision to the filterer it
// embeds.
type noPushdownFilterer struct{ chunk.Filterer }

func (noPushdownFilterer) PushableLabelMatchers() []*labels.Matcher { return nil }
