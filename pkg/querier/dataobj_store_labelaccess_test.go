package querier

import (
	"context"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/objtest"
	"github.com/grafana/loki/v3/pkg/labelaccess"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/storage/chunk"

	"github.com/grafana/loki/pkg/push"
)

func TestDataObjStore_LBACPushdown(t *testing.T) {
	const query = `sum by (app) (count_over_time({env=~".+"}[1m]))`

	readStreams := func(store *testDataObjStore, policies ...string) []string {
		ctx := accessControlContext(t, objtest.Tenant, policies...)
		return sampleLabels(store.selectSamples(ctx, query, at(0), at(10)))
	}

	streams := []logproto.Stream{
		{Labels: `{app="a", env="prod"}`, Entries: []push.Entry{entry(t, 1, "one")}},
		{Labels: `{app="b", env="prod", secret="true"}`, Entries: []push.Entry{entry(t, 1, "two")}},
		{Labels: `{app="c", env="dev", secret="true"}`, Entries: []push.Entry{entry(t, 1, "three")}},
		{Labels: `{app="d", env="dev"}`, Entries: []push.Entry{entry(t, 1, "four")}},
		{Labels: `{app="e", env="dev", team="x"}`, Entries: []push.Entry{entry(t, 1, "five")}},
	}

	t.Run("pushes the matchers down, so the filterer is asked only about the streams that match them", func(t *testing.T) {
		filterer := newRecordingRequestFilterer(true)
		store := newTestDataObjStore(t, streams, withStreamFilterer(filterer))

		got := readStreams(store, `{secret="true", env="dev"}`)

		require.Equal(t, []string{`{app="c"}`}, got)
		require.Equal(t, []string{"c"}, filterer.asked.apps())
	})

	t.Run("keeps the matchers that match the empty string out of the push-down", func(t *testing.T) {
		filterer := newRecordingRequestFilterer(true)
		store := newTestDataObjStore(t, streams, withStreamFilterer(filterer))

		got := readStreams(store, `{team=~".*", secret!="true", env="dev"}`)

		require.Equal(t, []string{`{app="d"}`, `{app="e"}`}, got)
		require.Equal(t, []string{"c", "d", "e"}, filterer.asked.apps())
	})

	t.Run("does not push the matchers down when the request has several policies, so the filterer is asked about every stream", func(t *testing.T) {
		filterer := newRecordingRequestFilterer(true)
		store := newTestDataObjStore(t, streams, withStreamFilterer(filterer))

		got := readStreams(store, `{secret="true", env="dev"}`, `{app="a"}`)

		require.Equal(t, []string{`{app="a"}`, `{app="c"}`}, got)
		require.Equal(t, []string{"a", "b", "c", "d", "e"}, filterer.asked.apps())
	})

	t.Run("asks the filterer about every stream when it hides its matchers", func(t *testing.T) {
		filterer := newRecordingRequestFilterer(false)
		store := newTestDataObjStore(t, streams, withStreamFilterer(filterer))

		got := readStreams(store, `{secret="true", env="dev"}`)

		require.Equal(t, []string{`{app="c"}`}, got)
		require.Equal(t, []string{"a", "b", "c", "d", "e"}, filterer.asked.apps())
	})

	t.Run("fails a selector with no stream matcher of its own even when the filterer states matchers", func(t *testing.T) {
		store := newTestDataObjStore(t, streams, withStreamFilterer(newRecordingRequestFilterer(true)))

		ctx := accessControlContext(t, objtest.Tenant, `{env="dev"}`)
		it, err := store.selectSamplesIter(ctx, `sum by (app) (count_over_time({__name__=~".+"}[1m]))`, at(0), at(10))
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
			pushed := newTestDataObjStore(t, streams, withStreamFilterer(newRecordingRequestFilterer(true)))
			planned := newTestDataObjStore(t, streams, withStreamFilterer(newRecordingRequestFilterer(false)))

			require.Equal(t, testCase.want, readStreams(pushed, testCase.selector), "with the push-down")
			require.Equal(t, testCase.want, readStreams(planned, testCase.selector), "without the push-down")
		})
	}
}

// recordingRequestFilterer wraps the label access filterer of Loki and records the streams the
// filterer of each request is asked about.
type recordingRequestFilterer struct {
	filterer chunk.RequestChunkFilterer
	asked    *askedStreams
	pushdown bool
}

// newRecordingRequestFilterer returns a filterer that states its matchers for the push-down only
// when pushdown is true.
func newRecordingRequestFilterer(pushdown bool) recordingRequestFilterer {
	return recordingRequestFilterer{
		filterer: &labelaccess.RequestChunkFilterer{},
		asked:    &askedStreams{},
		pushdown: pushdown,
	}
}

func (f recordingRequestFilterer) ForRequest(ctx context.Context) chunk.Filterer {
	filterer := f.filterer.ForRequest(ctx)
	if filterer == nil {
		return nil
	}
	return recordingFilterer{Filterer: filterer, asked: f.asked, pushdown: f.pushdown}
}

// recordingFilterer records the streams it is asked about and leaves every decision to the filterer
// it embeds.
type recordingFilterer struct {
	chunk.Filterer
	asked    *askedStreams
	pushdown bool
}

func (f recordingFilterer) ShouldFilter(streamLabels labels.Labels) bool {
	f.asked.add(streamLabels.Get("app"))
	return f.Filterer.ShouldFilter(streamLabels)
}

func (f recordingFilterer) PushableLabelMatchers() []*labels.Matcher {
	if !f.pushdown {
		return nil
	}
	return f.Filterer.PushableLabelMatchers()
}

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

// policySet parses the selectors of one tenant the way Loki parses the X-Prom-Label-Policy header.
func policySet(t *testing.T, tenantID string, selectors ...string) labelaccess.LabelPolicySet {
	t.Helper()

	values := make([]string, 0, len(selectors))
	for _, selector := range selectors {
		values = append(values, tenantID+":"+url.PathEscape(selector))
	}
	set, err := labelaccess.ExtractLabelMatchersFromHeaders(http.Header{
		labelaccess.HTTPHeaderKey: {strings.Join(values, ",")},
	})
	require.NoError(t, err)
	return set
}

// accessControlContext returns a request context that carries the selectors as policies of the
// tenant, as the HTTP middleware of Loki does.
func accessControlContext(t *testing.T, tenantID string, selectors ...string) context.Context {
	t.Helper()
	return labelaccess.InjectLabelMatchersContext(testCtx(t), policySet(t, tenantID, selectors...))
}

// sampleLabels returns the label set of each sample, in the order of the samples. It returns nil for
// no sample.
func sampleLabels(rows []sampleRow) []string {
	var out []string
	for _, row := range rows {
		out = append(out, row.Labels)
	}
	return out
}
