package base

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/gorilla/mux"
	"github.com/grafana/dskit/services"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/rulefmt"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/grafana/loki/pkg/ruler/rulespb"
)

func TestRuler_rules(t *testing.T) {
	cfg := defaultRulerConfig(t, newMockRuleStore(mockRules))

	r := newTestRuler(t, cfg)
	defer services.StopAndAwaitTerminated(context.Background(), r) //nolint:errcheck

	a := NewAPI(r, r.store, log.NewNopLogger())

	req := requestFor(t, "GET", "https://localhost:8080/api/prom/api/v1/rules", nil, "user1")
	w := httptest.NewRecorder()
	a.PrometheusRules(w, req)

	resp := w.Result()
	body, _ := io.ReadAll(resp.Body)

	// Check status code and status response
	responseJSON := response{}
	err := json.Unmarshal(body, &responseJSON)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, responseJSON.Status, "success")

	// Testing the running rules for user1 in the mock store
	expectedResponse, _ := json.Marshal(response{
		Status: "success",
		Data: &RuleDiscovery{
			RuleGroups: []*RuleGroup{
				{
					Name: "group1",
					File: "namespace1",
					Rules: []rule{
						&recordingRule{
							Name:   "UP_RULE",
							Query:  "up",
							Health: "unknown",
							Type:   "recording",
						},
						&alertingRule{
							Name:   "UP_ALERT",
							Query:  "up < 1",
							State:  "inactive",
							Health: "unknown",
							Type:   "alerting",
							Alerts: []*Alert{},
						},
					},
					Interval: 60,
					Limit:    10,
				},
			},
		},
	})

	require.Equal(t, string(expectedResponse), string(body))
}

func TestRuler_rules_special_characters(t *testing.T) {
	cfg := defaultRulerConfig(t, newMockRuleStore(mockSpecialCharRules))

	r := newTestRuler(t, cfg)
	defer services.StopAndAwaitTerminated(context.Background(), r) //nolint:errcheck

	a := NewAPI(r, r.store, log.NewNopLogger())

	req := requestFor(t, http.MethodGet, "https://localhost:8080/api/prom/api/v1/rules", nil, "user1")
	w := httptest.NewRecorder()
	a.PrometheusRules(w, req)

	resp := w.Result()
	body, _ := io.ReadAll(resp.Body)

	// Check status code and status response
	responseJSON := response{}
	err := json.Unmarshal(body, &responseJSON)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, responseJSON.Status, "success")

	// Testing the running rules for user1 in the mock store
	expectedResponse, _ := json.Marshal(response{
		Status: "success",
		Data: &RuleDiscovery{
			RuleGroups: []*RuleGroup{
				{
					Name: ")(_+?/|group1+/?",
					File: ")(_+?/|namespace1+/?",
					Rules: []rule{
						&recordingRule{
							Name:   "UP_RULE",
							Query:  "up",
							Health: "unknown",
							Type:   "recording",
						},
						&alertingRule{
							Name:   "UP_ALERT",
							Query:  "up < 1",
							State:  "inactive",
							Health: "unknown",
							Type:   "alerting",
							Alerts: []*Alert{},
						},
					},
					Interval: 60,
					Limit:    10,
				},
			},
		},
	})

	require.Equal(t, string(expectedResponse), string(body))
}

func TestRuler_alerts(t *testing.T) {
	cfg := defaultRulerConfig(t, newMockRuleStore(mockRules))

	r := newTestRuler(t, cfg)
	defer r.StopAsync()

	a := NewAPI(r, r.store, log.NewNopLogger())

	req := requestFor(t, http.MethodGet, "https://localhost:8080/api/prom/api/v1/alerts", nil, "user1")
	w := httptest.NewRecorder()
	a.PrometheusAlerts(w, req)

	resp := w.Result()
	body, _ := io.ReadAll(resp.Body)

	// Check status code and status response
	responseJSON := response{}
	err := json.Unmarshal(body, &responseJSON)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, responseJSON.Status, "success")

	// Currently there is not an easy way to mock firing alerts. The empty
	// response case is tested instead.
	expectedResponse, _ := json.Marshal(response{
		Status: "success",
		Data: &AlertDiscovery{
			Alerts: []*Alert{},
		},
	})

	require.Equal(t, string(expectedResponse), string(body))
}

func TestRuler_GetRulesLabelFilter(t *testing.T) {
	cfg := defaultRulerConfig(t, newMockRuleStore(mockRules))

	r := newTestRuler(t, cfg)
	defer r.StopAsync()

	a := NewAPI(r, r.store, log.NewNopLogger())

	allRules := map[string][]rulefmt.RuleGroup{
		"test": {
			{
				Name: "group1",
				Rules: []rulefmt.RuleNode{
					{
						Record: yaml.Node{
							Value:  "UP_RULE",
							Tag:    "!!str",
							Kind:   8,
							Line:   5,
							Column: 19,
						},
						Expr: yaml.Node{
							Value:  "up",
							Tag:    "!!str",
							Kind:   8,
							Line:   6,
							Column: 17,
						},
					},
					{
						Alert: yaml.Node{
							Value:  "UP_ALERT",
							Tag:    "!!str",
							Kind:   8,
							Line:   7,
							Column: 18,
						},
						Expr: yaml.Node{
							Value:  "up < 1",
							Tag:    "!!str",
							Kind:   8,
							Line:   8,
							Column: 17,
						},
						Labels: map[string]string{"foo": "bar"},
					},
					{
						Alert: yaml.Node{
							Value:  "DOWN_ALERT",
							Tag:    "!!str",
							Kind:   8,
							Line:   11,
							Column: 18,
						},
						Expr: yaml.Node{
							Value:  "down < 1",
							Tag:    "!!str",
							Kind:   8,
							Line:   12,
							Column: 17,
						},
						Labels: map[string]string{"namespace": "delta"},
					},
				},
				Interval: model.Duration(1 * time.Minute),
			},
		},
	}
	filteredRules := map[string][]rulefmt.RuleGroup{
		"test": {
			{
				Name: "group1",
				Rules: []rulefmt.RuleNode{
					{
						Alert: yaml.Node{
							Value:  "UP_ALERT",
							Tag:    "!!str",
							Kind:   8,
							Line:   5,
							Column: 18,
						},
						Expr: yaml.Node{
							Value:  "up < 1",
							Tag:    "!!str",
							Kind:   8,
							Line:   6,
							Column: 17,
						},
						Labels: map[string]string{"foo": "bar"},
					},
					{
						Alert: yaml.Node{
							Value:  "DOWN_ALERT",
							Tag:    "!!str",
							Kind:   8,
							Line:   9,
							Column: 18,
						},
						Expr: yaml.Node{
							Value:  "down < 1",
							Tag:    "!!str",
							Kind:   8,
							Line:   10,
							Column: 17,
						},
						Labels: map[string]string{"namespace": "delta"},
					},
				},
				Interval: model.Duration(1 * time.Minute),
			},
		},
	}

	tc := []struct {
		name     string
		URLQuery string
		output   map[string][]rulefmt.RuleGroup
		err      error
	}{
		{
			name:   "query with no filters",
			output: allRules,
		},
		{
			name:     "query with a valid filter found",
			URLQuery: "labels=namespace:delta,foo:bar",
			output:   filteredRules,
		},
		{
			name:     "query with an invalid query param",
			URLQuery: "test=namespace:delta,foo|bar",
			output:   allRules,
		},
	}

	for _, tt := range tc {
		t.Run(tt.name, func(t *testing.T) {
			url := "https://localhost:8080/api/v1/rules"
			if tt.URLQuery != "" {
				url = fmt.Sprintf("%s?%s", url, tt.URLQuery)
			}
			req := requestFor(t, http.MethodGet, url, nil, "user3")

			w := httptest.NewRecorder()
			a.ListRules(w, req)
			require.Equal(t, 200, w.Code)

			var res map[string][]rulefmt.RuleGroup
			err := yaml.Unmarshal(w.Body.Bytes(), &res)

			require.Nil(t, err)
			require.Equal(t, tt.output, res)
		})
	}
}

func TestRuler_Create(t *testing.T) {
	cfg := defaultRulerConfig(t, newMockRuleStore(make(map[string]rulespb.RuleGroupList)))

	r := newTestRuler(t, cfg)
	defer services.StopAndAwaitTerminated(context.Background(), r) //nolint:errcheck

	a := NewAPI(r, r.store, log.NewNopLogger())

	tc := []struct {
		name   string
		input  string
		output string
		err    error
		status int
	}{
		{
			name:   "with an empty payload",
			input:  "",
			status: 400,
			err:    errors.New("invalid rules config: rule group name must not be empty"),
		},
		{
			name: "with no rule group name",
			input: `
interval: 15s
rules:
- record: up_rule
  expr: up
`,
			status: 400,
			err:    errors.New("invalid rules config: rule group name must not be empty"),
		},
		{
			name: "with no rules",
			input: `
name: rg_name
interval: 15s
`,
			status: 400,
			err:    errors.New("invalid rules config: rule group 'rg_name' has no rules"),
		},
		{
			name:   "with a a valid rules file",
			status: 202,
			input: `
name: test
interval: 15s
rules:
- record: up_rule
  expr: up{}
- alert: up_alert
  expr: sum(up{}) > 1
  for: 30s
  annotations:
    test: test
  labels:
    test: test
`,
			output: "name: test\ninterval: 15s\nrules:\n    - record: up_rule\n      expr: up{}\n    - alert: up_alert\n      expr: sum(up{}) > 1\n      for: 30s\n      labels:\n        test: test\n      annotations:\n        test: test\n",
		},
		{
			name:   "with a a valid rules file with limit parameter",
			status: 202,
			input: `
name: test
interval: 15s
limit: 10
rules:
- record: up_rule
  expr: up{}
- alert: up_alert
  expr: sum(up{}) > 1
  for: 30s
  annotations:
    test: test
  labels:
    test: test
`,
			output: "name: test\ninterval: 15s\nlimit: 10\nrules:\n    - record: up_rule\n      expr: up{}\n    - alert: up_alert\n      expr: sum(up{}) > 1\n      for: 30s\n      labels:\n        test: test\n      annotations:\n        test: test\n",
		},
	}

	for _, tt := range tc {
		t.Run(tt.name, func(t *testing.T) {
			router := mux.NewRouter()
			router.Path("/api/v1/rules/{namespace}").Methods("POST").HandlerFunc(a.CreateRuleGroup)
			router.Path("/api/v1/rules/{namespace}/{groupName}").Methods("GET").HandlerFunc(a.GetRuleGroup)
			// POST
			req := requestFor(t, http.MethodPost, "https://localhost:8080/api/v1/rules/namespace", strings.NewReader(tt.input), "user1")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)
			require.Equal(t, tt.status, w.Code)

			if tt.err == nil {
				// GET
				req = requestFor(t, http.MethodGet, "https://localhost:8080/api/v1/rules/namespace/test", nil, "user1")
				w = httptest.NewRecorder()

				router.ServeHTTP(w, req)
				require.Equal(t, 200, w.Code)
				require.Equal(t, tt.output, w.Body.String())
			} else {
				require.Equal(t, tt.err.Error()+"\n", w.Body.String())
			}
		})
	}
}

func TestRuler_DeleteNamespace(t *testing.T) {
	cfg := defaultRulerConfig(t, newMockRuleStore(mockRulesNamespaces))

	r := newTestRuler(t, cfg)
	defer services.StopAndAwaitTerminated(context.Background(), r) //nolint:errcheck

	a := NewAPI(r, r.store, log.NewNopLogger())

	router := mux.NewRouter()
	router.Path("/api/v1/rules/{namespace}").Methods(http.MethodDelete).HandlerFunc(a.DeleteNamespace)
	router.Path("/api/v1/rules/{namespace}/{groupName}").Methods(http.MethodGet).HandlerFunc(a.GetRuleGroup)

	// Verify namespace1 rules are there.
	req := requestFor(t, http.MethodGet, "https://localhost:8080/api/v1/rules/namespace1/group1", nil, "user1")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, "name: group1\ninterval: 1m\nlimit: 10\nrules:\n    - record: UP_RULE\n      expr: up\n    - alert: UP_ALERT\n      expr: up < 1\n", w.Body.String())

	// Delete namespace1
	req = requestFor(t, http.MethodDelete, "https://localhost:8080/api/v1/rules/namespace1", nil, "user1")
	w = httptest.NewRecorder()

	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusAccepted, w.Code)
	require.Equal(t, "{\"status\":\"success\",\"data\":null,\"errorType\":\"\",\"error\":\"\"}", w.Body.String())

	// On Partial failures
	req = requestFor(t, http.MethodDelete, "https://localhost:8080/api/v1/rules/namespace2", nil, "user1")
	w = httptest.NewRecorder()

	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusInternalServerError, w.Code)
	require.Equal(t, "{\"status\":\"error\",\"data\":null,\"errorType\":\"server_error\",\"error\":\"unable to delete rg\"}", w.Body.String())
}

func TestRuler_LimitsPerGroup(t *testing.T) {
	cfg := defaultRulerConfig(t, newMockRuleStore(make(map[string]rulespb.RuleGroupList)))

	r := newTestRuler(t, cfg)
	defer services.StopAndAwaitTerminated(context.Background(), r) //nolint:errcheck

	r.limits = ruleLimits{maxRuleGroups: 1, maxRulesPerRuleGroup: 1}

	a := NewAPI(r, r.store, log.NewNopLogger())

	tc := []struct {
		name   string
		input  string
		output string
		err    error
		status int
	}{
		{
			name:   "when exceeding the rules per rule group limit",
			status: 400,
			input: `
name: test
interval: 15s
rules:
- record: up_rule
  expr: up{}
- alert: up_alert
  expr: sum(up{}) > 1
  for: 30s
  annotations:
    test: test
  labels:
    test: test
`,
			output: "per-user rules per rule group limit (limit: 1 actual: 2) exceeded\n",
		},
	}

	for _, tt := range tc {
		t.Run(tt.name, func(t *testing.T) {
			router := mux.NewRouter()
			router.Path("/api/v1/rules/{namespace}").Methods("POST").HandlerFunc(a.CreateRuleGroup)
			// POST
			req := requestFor(t, http.MethodPost, "https://localhost:8080/api/v1/rules/namespace", strings.NewReader(tt.input), "user1")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)
			require.Equal(t, tt.status, w.Code)
			require.Equal(t, tt.output, w.Body.String())
		})
	}
}

func TestRuler_RulerGroupLimits(t *testing.T) {
	cfg := defaultRulerConfig(t, newMockRuleStore(make(map[string]rulespb.RuleGroupList)))

	r := newTestRuler(t, cfg)
	defer services.StopAndAwaitTerminated(context.Background(), r) //nolint:errcheck

	r.limits = ruleLimits{maxRuleGroups: 1, maxRulesPerRuleGroup: 1}

	a := NewAPI(r, r.store, log.NewNopLogger())

	tc := []struct {
		name   string
		input  string
		output string
		err    error
		status int
	}{
		{
			name:   "when pushing the first group within bounds of the limit",
			status: 202,
			input: `
name: test_first_group_will_succeed
interval: 15s
rules:
- record: up_rule
  expr: up{}
`,
			output: "{\"status\":\"success\",\"data\":null,\"errorType\":\"\",\"error\":\"\"}",
		},
		{
			name:   "when exceeding the rule group limit after sending the first group",
			status: 400,
			input: `
name: test_second_group_will_fail
interval: 15s
rules:
- record: up_rule
  expr: up{}
`,
			output: "per-user rule groups limit (limit: 1 actual: 2) exceeded\n",
		},
	}

	// define once so the requests build on each other so the number of rules can be tested
	router := mux.NewRouter()
	router.Path("/api/v1/rules/{namespace}").Methods("POST").HandlerFunc(a.CreateRuleGroup)

	for _, tt := range tc {
		t.Run(tt.name, func(t *testing.T) {
			// POST
			req := requestFor(t, http.MethodPost, "https://localhost:8080/api/v1/rules/namespace", strings.NewReader(tt.input), "user1")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)
			require.Equal(t, tt.status, w.Code)
			require.Equal(t, tt.output, w.Body.String())
		})
	}
}

func requestFor(t *testing.T, method string, url string, body io.Reader, userID string) *http.Request {
	t.Helper()

	req := httptest.NewRequest(method, url, body)
	ctx := user.InjectOrgID(req.Context(), userID)

	return req.WithContext(ctx)
}
