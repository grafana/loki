package ruler

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/pkg/errors"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/rulefmt"
	"github.com/prometheus/prometheus/promql/parser"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func init() {
	model.NameValidationScheme = model.LegacyValidation
}

func Test_GroupLoader(t *testing.T) {
	for _, tc := range []struct {
		desc  string
		data  string
		match string
	}{
		{
			desc: "load correctly",
			data: `
groups:
  - name: testgrp2
    interval: 0s
    rules:
      - alert: HTTPCredentialsLeaked
        expr: sum by (cluster, job, pod) (rate({namespace=~"%s"} |~ "http(s?)://(\\w+):(\\w+)@" [5m]) > 0)
        for: 2m
        labels:
            severity: page
        annotations:
            summary: High request latency
`,
		},
		{
			desc:  "fail empty groupname",
			match: "Groupname must not be empty",
			data: `
groups:
  - name:
    interval: 0s
    rules:
      - alert: HighThroughputLogStreams
        expr: sum by (cluster, job, pod) (rate({namespace=~"%s"} |~ "http(s?)://(\\w+):(\\w+)@" [5m]) > 0)
        for: 2m
        labels:
            severity: page
        annotations:
            summary: High request latency
`,
		},
		{
			desc:  "fail duplicate grps",
			match: "repeated in the same file",
			data: `
groups:
  - name: grp1
    interval: 0s
    rules:
      - alert: HighThroughputLogStreams
        expr: sum by (cluster, job, pod) (rate({namespace=~"%s"} |~ "http(s?)://(\\w+):(\\w+)@" [5m]) > 0)
        for: 2m
        labels:
            severity: page
        annotations:
            summary: High request latency
  - name: grp1
    interval: 0s
    rules:
      - alert: HighThroughputLogStreams
        expr: sum by (cluster, job, pod) (rate({namespace=~"%s"} |~ "http(s?)://(\\w+):(\\w+)@" [5m]) > 0)
        for: 2m
        labels:
            severity: page
        annotations:
            summary: High request latency
`,
		},
		{
			desc:  "fail record & alert",
			match: "only one of 'record' and 'alert' must be set",
			data: `
groups:
  - name: grp1
    interval: 0s
    rules:
      - alert: HighThroughputLogStreams
        record: doublevision
        expr: sum by (cluster, job, pod) (rate({namespace=~"%s"} |~ "http(s?)://(\\w+):(\\w+)@" [5m]) > 0)
        for: 2m
        labels:
            severity: page
        annotations:
            summary: High request latency
`,
		},
		{
			desc:  "fail neither record nor alert",
			match: "one of 'record' or 'alert' must be set",
			data: `
groups:
  - name: grp1
    interval: 0s
    rules:
      - expr: sum by (cluster, job, pod) (rate({namespace=~"%s"} |~ "http(s?)://(\\w+):(\\w+)@" [5m]) > 0)
        for: 2m
        labels:
            severity: page
        annotations:
            summary: High request latency
`,
		},
		{
			desc:  "fail empty expr",
			match: "field 'expr' must be set in rule",
			data: `
groups:
  - name: grp1
    interval: 0s
    rules:
      - alert: HighThroughputLogStreams
        for: 2m
        labels:
            severity: page
        annotations:
            summary: High request latency
`,
		},
		{
			desc:  "fail bad expr",
			match: "could not parse expression",
			data: `
groups:
  - name: grp1
    interval: 0s
    rules:
      - alert: HighThroughputLogStreams
        expr: garbage
        for: 2m
        labels:
            severity: page
        annotations:
            summary: High request latency
`,
		},
		{
			desc:  "fail annotations in recording rule",
			match: "invalid field 'annotations' in recording rule",
			data: `
groups:
  - name: grp1
    interval: 0s
    rules:
      - record: HighThroughputLogStreams
        expr: sum by (cluster, job, pod) (rate({namespace=~"%s"} |~ "http(s?)://(\\w+):(\\w+)@" [5m]) > 0)
        labels:
            severity: page
        annotations:
            summary: High request latency
`,
		},
		{
			desc:  "fail for in recording rule",
			match: "invalid field 'for' in recording rule",
			data: `
groups:
  - name: grp1
    interval: 0s
    rules:
      - record: HighThroughputLogStreams
        expr: sum by (cluster, job, pod) (rate({namespace=~"%s"} |~ "http(s?)://(\\w+):(\\w+)@" [5m]) > 0)
        for: 2m
        labels:
            severity: page
`,
		},
		{
			desc:  "fail recording rule name",
			match: "invalid recording rule name:",
			data: `
groups:
  - name: grp1
    interval: 0s
    rules:
      - record: 'Hi.ghThroughputLogStreams'
        expr: sum by (cluster, job, pod) (rate({namespace=~"%s"} |~ "http(s?)://(\\w+):(\\w+)@" [5m]) > 0)
`,
		},
		{
			desc:  "fail invalid label name",
			match: "invalid label name:",
			data: `
groups:
  - name: grp1
    interval: 0s
    rules:
      - alert: HighThroughputLogStreams
        expr: sum by (cluster, job, pod) (rate({namespace=~"%s"} |~ "http(s?)://(\\w+):(\\w+)@" [5m]) > 0)
        for: 2m
        labels:
            'se.verity': page
        annotations:
            summary: High request latency
`,
		},
		{
			desc:  "fail invalid annotation",
			match: "invalid annotation name:",
			data: `
groups:
  - name: grp1
    interval: 0s
    rules:
      - alert: HighThroughputLogStreams
        expr: sum by (cluster, job, pod) (rate({namespace=~"%s"} |~ "http(s?)://(\\w+):(\\w+)@" [5m]) > 0)
        for: 2m
        labels:
            severity: page
        annotations:
            's.ummary': High request latency
`,
		},
		{
			desc:  "unknown fields",
			match: "field unknown not found",
			data: `
unknown: true
groups:
  - name: grp1
    interval: 0s
    rules:
      - alert: HighThroughputLogStreams
        expr: sum by (cluster, job, pod) (rate({namespace=~"%s"} |~ "http(s?)://(\\w+):(\\w+)@" [5m]) > 0)
        for: 2m
        labels:
            severity: page
        annotations:
            's.ummary': High request latency
`,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			var loader GroupLoader
			f, err := os.CreateTemp(os.TempDir(), "rules")
			require.Nil(t, err)
			defer os.Remove(f.Name())
			err = os.WriteFile(f.Name(), []byte(tc.data), 0777)
			require.Nil(t, err)

			_, errs := loader.Load(f.Name(), false)
			if tc.match != "" {
				require.NotNil(t, errs)
				var found bool
				for _, err := range errs {
					found = found || strings.Contains(err.Error(), tc.match)
				}
				if !found {
					fmt.Printf("\nerrors did not contain desired (%s): %v", tc.match, errs)
				}
				require.Equal(t, true, found)
			} else {
				require.Nil(t, errs)
			}
		})
	}
}

func TestCachingGroupLoader(t *testing.T) {
	t.Run("it caches rules as they are loaded from the underlying loader", func(t *testing.T) {
		l := newFakeGroupLoader()
		l.ruleGroups = map[string]*rulefmt.RuleGroups{
			"filename1": ruleGroup1,
			"filename2": ruleGroup2,
		}

		cl := NewCachingGroupLoader(l)

		groups, errs := cl.Load("filename1", true)
		require.Nil(t, errs)
		require.Equal(t, groups, ruleGroup1)

		rules := cl.AlertingRules()
		require.Equal(t, rulefmt.Rule{Alert: "alert-1-name"}, rules[0])

		groups, errs = cl.Load("filename2", true)
		require.Nil(t, errs)
		require.Equal(t, groups, ruleGroup2)

		rules = cl.AlertingRules()
		require.ElementsMatch(t, []rulefmt.Rule{{Alert: "alert-1-name"}, {Alert: "alert-2-name"}}, rules)
	})

	t.Run("it doesn't cache rules when the loader has an error", func(t *testing.T) {
		l := newFakeGroupLoader()
		l.loadErrs = []error{errors.New("something bad")}
		l.ruleGroups = map[string]*rulefmt.RuleGroups{
			"filename1": ruleGroup1,
			"filename2": ruleGroup2,
		}

		cl := NewCachingGroupLoader(l)

		groups, errs := cl.Load("filename1", true)
		require.Equal(t, l.loadErrs, errs)
		require.Nil(t, groups)

		rules := cl.AlertingRules()
		require.Len(t, rules, 0)
	})

	t.Run("it removes files that are not in the list", func(t *testing.T) {
		l := newFakeGroupLoader()
		l.ruleGroups = map[string]*rulefmt.RuleGroups{
			"filename1": ruleGroup1,
			"filename2": ruleGroup2,
		}

		cl := NewCachingGroupLoader(l)

		_, errs := cl.Load("filename1", true)
		require.Nil(t, errs)

		_, errs = cl.Load("filename2", true)
		require.Nil(t, errs)

		cl.Prune([]string{"filename2"})

		rules := cl.AlertingRules()
		require.Len(t, rules, 1)
		require.Equal(t, rulefmt.Rule{Alert: "alert-2-name"}, rules[0])
	})
}

func newFakeGroupLoader() *fakeGroupLoader {
	return &fakeGroupLoader{
		ruleGroups: make(map[string]*rulefmt.RuleGroups),
	}
}

type fakeGroupLoader struct {
	ruleGroups map[string]*rulefmt.RuleGroups
	loadErrs   []error
	expr       parser.Expr
	parseErr   error
}

func (gl *fakeGroupLoader) Load(identifier string, _ bool) (*rulefmt.RuleGroups, []error) {
	return gl.ruleGroups[identifier], gl.loadErrs
}

func (gl *fakeGroupLoader) Parse(_ string) (parser.Expr, error) {
	return gl.expr, gl.parseErr
}

var (
	ruleGroup1 = &rulefmt.RuleGroups{
		Groups: []rulefmt.RuleGroup{
			{
				Rules: []rulefmt.RuleNode{
					{Alert: yaml.Node{Value: "alert-1-name"}},
				},
			},
		},
	}
	ruleGroup2 = &rulefmt.RuleGroups{
		Groups: []rulefmt.RuleGroup{
			{
				Rules: []rulefmt.RuleNode{
					{Alert: yaml.Node{Value: "alert-2-name"}},
				},
			},
		},
	}
)
