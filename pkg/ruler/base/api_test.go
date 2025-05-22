package base

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/gorilla/mux"
	"github.com/grafana/dskit/services"
	"github.com/grafana/dskit/user"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/rulefmt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/grafana/loki/v3/pkg/ruler/rulespb"
)

func TestRuler_PrometheusRules(t *testing.T) {
	const (
		userID   = "user1"
		interval = time.Minute
	)

	groupName := func(group int) string {
		return fmt.Sprintf("group%d+", group)
	}

	namespaceName := func(ns int) string {
		return fmt.Sprintf("namespace%d+", ns)
	}

	makeFilterTestRules := func() rulespb.RuleGroupList {
		result := rulespb.RuleGroupList{}
		for ns := 1; ns <= 3; ns++ {
			for group := 1; group <= 3; group++ {
				g := &rulespb.RuleGroupDesc{
					Name:      groupName(group),
					Namespace: namespaceName(ns),
					User:      userID,
					Rules: []*rulespb.RuleDesc{
						createRecordingRule("NonUniqueNamedRule", `count_over_time({foo="bar"}[5m])`),
						createAlertingRule(fmt.Sprintf("UniqueNamedRuleN%dG%d", ns, group), `count_over_time({foo="bar"}[5m]) < 1`),
					},
					Interval: interval,
				}
				result = append(result, g)
			}
		}
		return result
	}

	filterTestExpectedRule := func(name string) *recordingRule {
		return &recordingRule{
			Name:   name,
			Query:  `count_over_time({foo="bar"}[5m])`,
			Health: "unknown",
			Type:   "recording",
		}
	}
	filterTestExpectedAlert := func(name string) *alertingRule {
		return &alertingRule{
			Name:   name,
			Query:  `count_over_time({foo="bar"}[5m]) < 1`,
			State:  "inactive",
			Health: "unknown",
			Type:   "alerting",
			Alerts: []*Alert{},
		}
	}

	testCases := map[string]struct {
		configuredRules    rulespb.RuleGroupList
		expectedConfigured int
		expectedStatusCode int
		expectedErrorType  v1.ErrorType
		expectedRules      []*RuleGroup
		queryParams        string
	}{
		"should load and evaluate the configured rules": {
			configuredRules: rulespb.RuleGroupList{
				&rulespb.RuleGroupDesc{
					Name:      "group1",
					Namespace: "namespace1",
					User:      userID,
					Rules:     []*rulespb.RuleDesc{createRecordingRule("COUNT_RULE", `count_over_time({foo="bar"}[5m])`), createAlertingRule("COUNT_ALERT", `count_over_time({foo="bar"}[5m]) < 1`)},
					Interval:  interval,
				},
			},
			expectedConfigured: 1,
			expectedRules: []*RuleGroup{
				{
					Name: "group1",
					File: "namespace1",
					Rules: []rule{
						&recordingRule{
							Name:   "COUNT_RULE",
							Query:  `count_over_time({foo="bar"}[5m])`,
							Health: "unknown",
							Type:   "recording",
						},
						&alertingRule{
							Name:   "COUNT_ALERT",
							Query:  `count_over_time({foo="bar"}[5m]) < 1`,
							State:  "inactive",
							Health: "unknown",
							Type:   "alerting",
							Alerts: []*Alert{},
						},
					},
					Interval: 60,
				},
			},
		},
		"should load and evaluate rule groups and namespaces with special characters": {
			configuredRules: rulespb.RuleGroupList{
				&rulespb.RuleGroupDesc{
					Name:      ")(_+?/|group1+/?",
					Namespace: ")(_+?/|namespace1+/?",
					User:      userID,
					Rules:     []*rulespb.RuleDesc{createRecordingRule("COUNT_RULE", `count_over_time({foo="bar"}[5m])`), createAlertingRule("COUNT_ALERT", `count_over_time({foo="bar"}[5m]) < 1`)},
					Interval:  interval,
				},
			},
			expectedConfigured: 1,
			expectedRules: []*RuleGroup{
				{
					Name: ")(_+?/|group1+/?",
					File: ")(_+?/|namespace1+/?",
					Rules: []rule{
						&recordingRule{
							Name:   "COUNT_RULE",
							Query:  `count_over_time({foo="bar"}[5m])`,
							Health: "unknown",
							Type:   "recording",
						},
						&alertingRule{
							Name:   "COUNT_ALERT",
							Query:  `count_over_time({foo="bar"}[5m]) < 1`,
							State:  "inactive",
							Health: "unknown",
							Type:   "alerting",
							Alerts: []*Alert{},
						},
					},
					Interval: 60,
				},
			},
		},
		"API returns only alerts": {
			configuredRules: rulespb.RuleGroupList{
				&rulespb.RuleGroupDesc{
					Name:      "group1",
					Namespace: "namespace1",
					User:      userID,
					Rules:     []*rulespb.RuleDesc{createRecordingRule("COUNT_RULE", `count_over_time({foo="bar"}[5m])`), createAlertingRule("COUNT_ALERT", `count_over_time({foo="bar"}[5m]) < 1`)},
					Interval:  interval,
				},
			},
			expectedConfigured: 1,
			queryParams:        "?type=alert",
			expectedRules: []*RuleGroup{
				{
					Name: "group1",
					File: "namespace1",
					Rules: []rule{
						&alertingRule{
							Name:   "COUNT_ALERT",
							Query:  `count_over_time({foo="bar"}[5m]) < 1`,
							State:  "inactive",
							Health: "unknown",
							Type:   "alerting",
							Alerts: []*Alert{},
						},
					},
					Interval: 60,
				},
			},
		},
		"API returns only rules": {
			configuredRules: rulespb.RuleGroupList{
				&rulespb.RuleGroupDesc{
					Name:      "group1",
					Namespace: "namespace1",
					User:      userID,
					Rules:     []*rulespb.RuleDesc{createRecordingRule("COUNT_RULE", `count_over_time({foo="bar"}[5m])`), createAlertingRule("COUNT_ALERT", `count_over_time({foo="bar"}[5m]) < 1`)},
					Interval:  interval,
				},
			},
			expectedConfigured: 1,
			queryParams:        "?type=record",
			expectedRules: []*RuleGroup{
				{
					Name: "group1",
					File: "namespace1",
					Rules: []rule{
						&recordingRule{
							Name:   "COUNT_RULE",
							Query:  `count_over_time({foo="bar"}[5m])`,
							Health: "unknown",
							Type:   "recording",
						},
					},
					Interval: 60,
				},
			},
		},
		"Invalid type param": {
			configuredRules:    rulespb.RuleGroupList{},
			expectedConfigured: 0,
			queryParams:        "?type=foo",
			expectedStatusCode: http.StatusBadRequest,
			expectedErrorType:  v1.ErrBadData,
			expectedRules:      []*RuleGroup{},
		},
		"when filtering by an unknown namespace then the API returns nothing": {
			configuredRules:    makeFilterTestRules(),
			expectedConfigured: len(makeFilterTestRules()),
			queryParams:        "?file=unknown",
			expectedRules:      []*RuleGroup{},
		},
		"when filtering by a single known namespace then the API returns only rules from that namespace": {
			configuredRules:    makeFilterTestRules(),
			expectedConfigured: len(makeFilterTestRules()),
			queryParams:        "?" + url.Values{"file": []string{namespaceName(1)}}.Encode(),
			expectedRules: []*RuleGroup{
				{
					Name: groupName(1),
					File: namespaceName(1),
					Rules: []rule{
						filterTestExpectedRule("NonUniqueNamedRule"),
						filterTestExpectedAlert("UniqueNamedRuleN1G1"),
					},
					Interval: 60,
				},
				{
					Name: groupName(2),
					File: namespaceName(1),
					Rules: []rule{
						filterTestExpectedRule("NonUniqueNamedRule"),
						filterTestExpectedAlert("UniqueNamedRuleN1G2"),
					},
					Interval: 60,
				},
				{
					Name: groupName(3),
					File: namespaceName(1),
					Rules: []rule{
						filterTestExpectedRule("NonUniqueNamedRule"),
						filterTestExpectedAlert("UniqueNamedRuleN1G3"),
					},
					Interval: 60,
				},
			},
		},
		"when filtering by a multiple known namespaces then the API returns rules from both namespaces": {
			configuredRules:    makeFilterTestRules(),
			expectedConfigured: len(makeFilterTestRules()),
			queryParams:        "?" + url.Values{"file": []string{namespaceName(1), namespaceName(2)}}.Encode(),
			expectedRules: []*RuleGroup{
				{
					Name: groupName(1),
					File: namespaceName(1),
					Rules: []rule{
						filterTestExpectedRule("NonUniqueNamedRule"),
						filterTestExpectedAlert("UniqueNamedRuleN1G1"),
					},
					Interval: 60,
				},
				{
					Name: groupName(2),
					File: namespaceName(1),
					Rules: []rule{
						filterTestExpectedRule("NonUniqueNamedRule"),
						filterTestExpectedAlert("UniqueNamedRuleN1G2"),
					},
					Interval: 60,
				},
				{
					Name: groupName(3),
					File: namespaceName(1),
					Rules: []rule{
						filterTestExpectedRule("NonUniqueNamedRule"),
						filterTestExpectedAlert("UniqueNamedRuleN1G3"),
					},
					Interval: 60,
				},
				{
					Name: groupName(1),
					File: namespaceName(2),
					Rules: []rule{
						filterTestExpectedRule("NonUniqueNamedRule"),
						filterTestExpectedAlert("UniqueNamedRuleN2G1"),
					},
					Interval: 60,
				},
				{
					Name: groupName(2),
					File: namespaceName(2),
					Rules: []rule{
						filterTestExpectedRule("NonUniqueNamedRule"),
						filterTestExpectedAlert("UniqueNamedRuleN2G2"),
					},
					Interval: 60,
				},
				{
					Name: groupName(3),
					File: namespaceName(2),
					Rules: []rule{
						filterTestExpectedRule("NonUniqueNamedRule"),
						filterTestExpectedAlert("UniqueNamedRuleN2G3"),
					},
					Interval: 60,
				},
			},
		},
		"when filtering by an unknown group then the API returns nothing": {
			configuredRules:    makeFilterTestRules(),
			expectedConfigured: len(makeFilterTestRules()),
			queryParams:        "?rule_group=unknown",
			expectedRules:      []*RuleGroup{},
		},
		"when filtering by a known group then the API returns only rules from that group": {
			configuredRules:    makeFilterTestRules(),
			expectedConfigured: len(makeFilterTestRules()),
			queryParams:        "?" + url.Values{"rule_group": []string{groupName(2)}}.Encode(),
			expectedRules: []*RuleGroup{
				{
					Name: groupName(2),
					File: namespaceName(1),
					Rules: []rule{
						filterTestExpectedRule("NonUniqueNamedRule"),
						filterTestExpectedAlert("UniqueNamedRuleN1G2"),
					},
					Interval: 60,
				},
				{
					Name: groupName(2),
					File: namespaceName(2),
					Rules: []rule{
						filterTestExpectedRule("NonUniqueNamedRule"),
						filterTestExpectedAlert("UniqueNamedRuleN2G2"),
					},
					Interval: 60,
				},
				{
					Name: groupName(2),
					File: namespaceName(3),
					Rules: []rule{
						filterTestExpectedRule("NonUniqueNamedRule"),
						filterTestExpectedAlert("UniqueNamedRuleN3G2"),
					},
					Interval: 60,
				},
			},
		},
		"when filtering by multiple known groups then the API returns rules from both groups": {
			configuredRules:    makeFilterTestRules(),
			expectedConfigured: len(makeFilterTestRules()),
			queryParams:        "?" + url.Values{"rule_group": []string{groupName(2), groupName(3)}}.Encode(),
			expectedRules: []*RuleGroup{
				{
					Name: groupName(2),
					File: namespaceName(1),
					Rules: []rule{
						filterTestExpectedRule("NonUniqueNamedRule"),
						filterTestExpectedAlert("UniqueNamedRuleN1G2"),
					},
					Interval: 60,
				},
				{
					Name: groupName(3),
					File: namespaceName(1),
					Rules: []rule{
						filterTestExpectedRule("NonUniqueNamedRule"),
						filterTestExpectedAlert("UniqueNamedRuleN1G3"),
					},
					Interval: 60,
				},
				{
					Name: groupName(2),
					File: namespaceName(2),
					Rules: []rule{
						filterTestExpectedRule("NonUniqueNamedRule"),
						filterTestExpectedAlert("UniqueNamedRuleN2G2"),
					},
					Interval: 60,
				},
				{
					Name: groupName(3),
					File: namespaceName(2),
					Rules: []rule{
						filterTestExpectedRule("NonUniqueNamedRule"),
						filterTestExpectedAlert("UniqueNamedRuleN2G3"),
					},
					Interval: 60,
				},
				{
					Name: groupName(2),
					File: namespaceName(3),
					Rules: []rule{
						filterTestExpectedRule("NonUniqueNamedRule"),
						filterTestExpectedAlert("UniqueNamedRuleN3G2"),
					},
					Interval: 60,
				},
				{
					Name: groupName(3),
					File: namespaceName(3),
					Rules: []rule{
						filterTestExpectedRule("NonUniqueNamedRule"),
						filterTestExpectedAlert("UniqueNamedRuleN3G3"),
					},
					Interval: 60,
				},
			},
		},

		"when filtering by an unknown rule name then the API returns all empty groups": {
			configuredRules:    makeFilterTestRules(),
			expectedConfigured: len(makeFilterTestRules()),
			queryParams:        "?rule_name=unknown",
			expectedRules:      []*RuleGroup{},
		},
		"when filtering by a known rule name then the API returns only rules with that name": {
			configuredRules:    makeFilterTestRules(),
			expectedConfigured: len(makeFilterTestRules()),
			queryParams:        "?" + url.Values{"rule_name": []string{"UniqueNamedRuleN1G2"}}.Encode(),
			expectedRules: []*RuleGroup{
				{
					Name: groupName(2),
					File: namespaceName(1),
					Rules: []rule{
						filterTestExpectedAlert("UniqueNamedRuleN1G2"),
					},
					Interval: 60,
				},
			},
		},
		"when filtering by multiple known rule names then the API returns both rules": {
			configuredRules:    makeFilterTestRules(),
			expectedConfigured: len(makeFilterTestRules()),
			queryParams:        "?" + url.Values{"rule_name": []string{"UniqueNamedRuleN1G2", "UniqueNamedRuleN2G3"}}.Encode(),
			expectedRules: []*RuleGroup{
				{
					Name: groupName(2),
					File: namespaceName(1),
					Rules: []rule{
						filterTestExpectedAlert("UniqueNamedRuleN1G2"),
					},
					Interval: 60,
				},
				{
					Name: groupName(3),
					File: namespaceName(2),
					Rules: []rule{
						filterTestExpectedAlert("UniqueNamedRuleN2G3"),
					},
					Interval: 60,
				},
			},
		},
		"when filtering by a known namespace and group then the API returns only rules from that namespace and group": {
			configuredRules:    makeFilterTestRules(),
			expectedConfigured: len(makeFilterTestRules()),
			queryParams: "?" + url.Values{
				"file":       []string{namespaceName(3)},
				"rule_group": []string{groupName(2)},
			}.Encode(),
			expectedRules: []*RuleGroup{
				{
					Name: groupName(2),
					File: namespaceName(3),
					Rules: []rule{
						&recordingRule{
							Name:   "NonUniqueNamedRule",
							Query:  `count_over_time({foo="bar"}[5m])`,
							Health: "unknown",
							Type:   "recording",
						},
						&alertingRule{
							Name:   "UniqueNamedRuleN3G2",
							Query:  `count_over_time({foo="bar"}[5m]) < 1`,
							State:  "inactive",
							Health: "unknown",
							Type:   "alerting",
							Alerts: []*Alert{},
						},
					},
					Interval: 60,
				},
			},
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			storageRules := map[string]rulespb.RuleGroupList{
				userID: tc.configuredRules,
			}
			cfg := defaultRulerConfig(t, newMockRuleStore(storageRules))

			r := newTestRuler(t, cfg)
			defer services.StopAndAwaitTerminated(context.Background(), r) //nolint:errcheck

			a := NewAPI(r, r.store, log.NewNopLogger())

			req := requestFor(t, "GET", "https://localhost:8080/api/prom/api/v1/rules"+tc.queryParams, nil, "user1")
			w := httptest.NewRecorder()
			a.PrometheusRules(w, req)

			resp := w.Result()
			if tc.expectedStatusCode != 0 {
				require.Equal(t, tc.expectedStatusCode, resp.StatusCode)
			} else {
				require.Equal(t, http.StatusOK, resp.StatusCode)
			}

			body, _ := io.ReadAll(resp.Body)

			// Check status code and status response
			responseJSON := response{}
			err := json.Unmarshal(body, &responseJSON)
			require.NoError(t, err)

			if tc.expectedErrorType != "" {
				assert.Equal(t, "error", responseJSON.Status)
				assert.Equal(t, tc.expectedErrorType, responseJSON.ErrorType)
				return
			}
			require.Equal(t, responseJSON.Status, "success")

			// Testing the running rules
			expectedResponse, err := json.Marshal(response{
				Status: "success",
				Data: &RuleDiscovery{
					RuleGroups: tc.expectedRules,
				},
			})

			require.NoError(t, err)
			require.Equal(t, string(expectedResponse), string(body))
		})
	}
}

func TestRuler_PrometheusAlerts(t *testing.T) {
	cfg := defaultRulerConfig(t, newMockRuleStore(mockRules))

	r := newTestRuler(t, cfg)
	defer services.StopAndAwaitTerminated(context.Background(), r) //nolint:errcheck

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
	defer services.StopAndAwaitTerminated(context.Background(), r) //nolint:errcheck

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

func createRecordingRule(record, expr string) *rulespb.RuleDesc {
	return &rulespb.RuleDesc{
		Record: record,
		Expr:   expr,
	}
}

func createAlertingRule(alert, expr string) *rulespb.RuleDesc {
	return &rulespb.RuleDesc{
		Alert: alert,
		Expr:  expr,
	}
}
