//go:build e2e

package e2e

import (
	"net/http"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Two streams carry the data the tests read. A label policy of {env="dev"} lets a reader see the
// first one and not the second, so each line and each label value is unique to one of them.
var (
	devStream = stream{
		labels: map[string]string{"job": "smoke", "env": "dev", "team": "alpha"},
		line:   `level=info msg="dev line" tier=free`,
	}
	prodStream = stream{
		labels: map[string]string{"job": "smoke", "env": "prod", "team": "beta"},
		line:   `level=error msg="prod line" tier=paid`,
	}

	// internalStream carries every label that Loki sets for its own use. The label, series and
	// detected label APIs hide these labels.
	internalStream = stream{
		labels: map[string]string{
			"job":                   "internal",
			"env":                   "dev",
			"__time_shard__":        "1",
			"__stream_shard__":      "0",
			"__aggregated_metric__": "smoke",
			"__pattern__":           "smoke",
		},
		line: "internal line",
	}
	internalLabels = []string{"__time_shard__", "__stream_shard__", "__aggregated_metric__", "__pattern__"}
)

// lbacHeader is the header a trusted gateway sets to restrict what a tenant can read.
const lbacHeader = "X-Prom-Label-Policy"

// devPolicy restricts the test tenant to the dev stream.
func devPolicy() map[string]string {
	return map[string]string{lbacHeader: tenant + ":" + url.PathEscape(`{env="dev"}`)}
}

// TestSmoke deploys Loki in every mode, writes a little data, and checks the write and read APIs.
// A deployment takes a while to start, so the checks share one per mode.
func TestSmoke(t *testing.T) {
	forEachMode(t, func(t *testing.T, h *harness) {
		now := time.Now()
		h.pushLoki(t, now, devStream, prodStream, internalStream)
		h.pushOTLP(t, now, "otlp-smoke", "otlp line")

		// The write path is asynchronous in distributed mode. Wait for the data to be readable
		// before checking the read APIs.
		require.EventuallyWithT(t, func(c *assert.CollectT) {
			status, body := do(t, h.get("/loki/api/v1/query_range", queryParams(now, `{job="smoke"}`)))
			require.Equal(c, http.StatusOK, status, body)
			require.Contains(c, body, "dev line")
			require.Contains(c, body, "prod line")
		}, 90*time.Second, time.Second)

		t.Run("push", func(t *testing.T) { testPush(t, h, now) })
		t.Run("read APIs", func(t *testing.T) { testReadAPIs(t, h, now) })
		t.Run("internal labels", func(t *testing.T) { testInternalLabels(t, h, now) })
		t.Run("label policies", func(t *testing.T) { testLabelPolicies(t, h, now) })
	})
}

// testPush checks that data written through the Loki and OTLP push APIs can be read back.
func testPush(t *testing.T, h *harness, now time.Time) {
	status, body := do(t, h.get("/loki/api/v1/query_range", queryParams(now, `{job="smoke", env="dev"}`)))
	require.Equal(t, http.StatusOK, status, body)
	require.Contains(t, body, "dev line")

	// OTLP pushes a log record as a stream labelled with its service name.
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		status, body := do(t, h.get("/loki/api/v1/query_range", queryParams(now, `{service_name="otlp-smoke"}`)))
		require.Equal(c, http.StatusOK, status, body)
		require.Contains(c, body, "otlp line")
	}, 30*time.Second, time.Second)
}

// endpoint describes a read API and what a response to it looks like for the test data.
type endpoint struct {
	name   string
	path   string
	params func(now time.Time) url.Values

	// allow is text that is in the response, with and without a label policy.
	allow []string
	// deny is text that must not be in the response when the dev policy applies, because it
	// belongs to the prod stream.
	deny []string
}

func (e endpoint) request(h *harness, now time.Time, headers map[string]string) request {
	r := h.get(e.path, e.params(now))
	r.headers = headers
	return r
}

func readEndpoints() []endpoint {
	selector := func(now time.Time) url.Values { return queryParams(now, `{job="smoke"}`) }
	rangeSelector := func(now time.Time) url.Values {
		v := queryParams(now, `{job="smoke"}`)
		v.Set("step", "60")
		return v
	}
	match := func(now time.Time) url.Values {
		v := queryParams(now, "")
		v.Del("query")
		v.Set("match[]", `{job="smoke"}`)
		return v
	}
	volume := func(now time.Time) url.Values {
		v := rangeSelector(now)
		v.Set("targetLabels", "env")
		return v
	}
	shards := func(now time.Time) url.Values {
		v := selector(now)
		v.Set("targetBytesPerShard", "1000000")
		return v
	}
	timeOnly := func(now time.Time) url.Values {
		v := queryParams(now, "")
		v.Del("query")
		return v
	}
	instant := func(query string) func(time.Time) url.Values {
		return func(now time.Time) url.Values {
			return url.Values{"query": {query}, "time": {nanos(now)}}
		}
	}

	return []endpoint{
		{name: "query", path: "/loki/api/v1/query", params: instant(`count_over_time({job="smoke"}[1h])`),
			allow: []string{`"env":"dev"`}, deny: []string{`"env":"prod"`}},
		{name: "query_range", path: "/loki/api/v1/query_range", params: selector,
			allow: []string{"dev line"}, deny: []string{"prod line"}},
		{name: "query_range metric", path: "/loki/api/v1/query_range", params: func(now time.Time) url.Values {
			v := rangeSelector(now)
			v.Set("query", `sum by (env) (count_over_time({job="smoke"}[1h]))`)
			return v
		}, allow: []string{`"env":"dev"`}, deny: []string{`"env":"prod"`}},
		{name: "series", path: "/loki/api/v1/series", params: match,
			allow: []string{`"env":"dev"`}, deny: []string{`"env":"prod"`, `"team":"beta"`}},
		{name: "labels", path: "/loki/api/v1/labels", params: timeOnly,
			allow: []string{`"env"`, `"team"`}},
		{name: "label values", path: "/loki/api/v1/label/env/values", params: timeOnly,
			allow: []string{`"dev"`}, deny: []string{`"prod"`}},
		{name: "index stats", path: "/loki/api/v1/index/stats", params: selector,
			allow: []string{`"streams"`}},
		{name: "index volume", path: "/loki/api/v1/index/volume", params: volume,
			allow: []string{`"env":"dev"`}, deny: []string{`"env":"prod"`}},
		{name: "index volume_range", path: "/loki/api/v1/index/volume_range", params: volume,
			allow: []string{`"env":"dev"`}, deny: []string{`"env":"prod"`}},
		{name: "index shards", path: "/loki/api/v1/index/shards", params: shards,
			allow: []string{`"shards"`}},
		{name: "patterns", path: "/loki/api/v1/patterns", params: selector,
			allow: []string{`"status":"success"`}},
		{name: "detected labels", path: "/loki/api/v1/detected_labels", params: selector,
			allow: []string{`"team"`}},
		{name: "detected fields", path: "/loki/api/v1/detected_fields", params: selector,
			allow: []string{`"tier"`}},
		{name: "detected field values", path: "/loki/api/v1/detected_field/tier/values", params: selector,
			allow: []string{`"free"`}, deny: []string{`"paid"`}},
		{name: "legacy query", path: "/api/prom/query", params: selector,
			allow: []string{"dev line"}, deny: []string{"prod line"}},
		{name: "legacy label", path: "/api/prom/label", params: timeOnly,
			allow: []string{`"env"`}},
		{name: "legacy label values", path: "/api/prom/label/env/values", params: timeOnly,
			allow: []string{`"dev"`}, deny: []string{`"prod"`}},
		{name: "legacy series", path: "/api/prom/series", params: match,
			allow: []string{`"env":"dev"`}, deny: []string{`"env":"prod"`}},
	}
}

// testReadAPIs checks that every read API answers a basic request.
func testReadAPIs(t *testing.T, h *harness, now time.Time) {
	for _, e := range readEndpoints() {
		t.Run(e.name, func(t *testing.T) {
			status, body := do(t, e.request(h, now, nil))
			require.Equal(t, http.StatusOK, status, body)
			for _, s := range e.allow {
				require.Contains(t, body, s)
			}
		})
	}
	t.Run("tail", func(t *testing.T) { testTail(t, h, nil) })
}

// testInternalLabels checks that the label names, series and detected labels APIs leave out the
// labels Loki sets for its own use.
func testInternalLabels(t *testing.T, h *harness, now time.Time) {
	// The stream with the internal labels has to be there, or the checks prove nothing.
	status, body := do(t, h.get("/loki/api/v1/series", url.Values{
		"match[]": {`{job="internal"}`}, "start": {nanos(now.Add(-time.Hour))}, "end": {nanos(now.Add(time.Minute))},
	}))
	require.Equal(t, http.StatusOK, status, body)
	require.Contains(t, body, `"job":"internal"`)

	for _, name := range []string{"labels", "legacy label", "detected labels"} {
		for _, e := range readEndpoints() {
			if e.name != name {
				continue
			}
			t.Run(e.name, func(t *testing.T) {
				status, body := do(t, e.request(h, now, nil))
				require.Equal(t, http.StatusOK, status, body)
				for _, label := range internalLabels {
					require.NotContains(t, body, label)
				}
			})
		}
	}

	// The series API should return the labels of every stream that matches, minus the internal
	// ones, like the label names API does.
	t.Run("series", func(t *testing.T) {
		status, body := do(t, h.get("/loki/api/v1/series", url.Values{
			"match[]": {`{job="internal"}`}, "start": {nanos(now.Add(-time.Hour))}, "end": {nanos(now.Add(time.Minute))},
		}))
		require.Equal(t, http.StatusOK, status, body)
		require.Contains(t, body, `"env":"dev"`)
		for _, label := range internalLabels {
			require.NotContains(t, body, label)
		}
	})
}

// testLabelPolicies checks that every read API returns only the data a label policy allows.
func testLabelPolicies(t *testing.T, h *harness, now time.Time) {
	for _, e := range readEndpoints() {
		t.Run(e.name, func(t *testing.T) {
			status, body := do(t, e.request(h, now, devPolicy()))
			require.Equal(t, http.StatusOK, status, body)
			for _, s := range e.allow {
				require.Contains(t, body, s)
			}
			for _, s := range e.deny {
				require.NotContains(t, body, s)
			}
		})
	}

	t.Run("tail", func(t *testing.T) { testTail(t, h, devPolicy()) })
}

// get builds a read request for the query-facing component.
func (h *harness) get(path string, query url.Values) request {
	return request{url: h.readURL + path, query: query}
}

// queryParams are the parameters of a query over the last hour.
func queryParams(now time.Time, query string) url.Values {
	return url.Values{
		"query": {query},
		"start": {nanos(now.Add(-time.Hour))},
		"end":   {nanos(now.Add(time.Minute))},
		"limit": {"100"},
	}
}

func nanos(t time.Time) string { return strconv.FormatInt(t.UnixNano(), 10) }
