package commands

import (
	"encoding/base64"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/prometheus/prometheus/model/rulefmt"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/lokitool/rules/rwrulefmt"
)

func TestCheckDuplicates(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   []rwrulefmt.RuleGroup
		want []compareRuleType
	}{
		{
			name: "no duplicates",
			in: []rwrulefmt.RuleGroup{{
				RuleGroup: rulefmt.RuleGroup{
					Name: "rulegroup",
					Rules: []rulefmt.Rule{
						{
							Record: "up",
							Expr:   "up==1",
						},
						{
							Record: "down",
							Expr:   "up==0",
						},
					},
				},
				RWConfigs: []rwrulefmt.RemoteWriteConfig{},
			}},
			want: nil,
		},
		{
			name: "with duplicates",
			in: []rwrulefmt.RuleGroup{{
				RuleGroup: rulefmt.RuleGroup{
					Name: "rulegroup",
					Rules: []rulefmt.Rule{
						{
							Record: "up",
							Expr:   "up==1",
						},
						{
							Record: "up",
							Expr:   "up==0",
						},
					},
				},
				RWConfigs: []rwrulefmt.RemoteWriteConfig{},
			}},
			want: []compareRuleType{{metric: "up", label: map[string]string(nil)}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, checkDuplicates(tc.in))
		})
	}
}

const rulesResponse = `ns1:
    - name: group1
      rules:
        - record: job:requests:rate1m
          expr: sum by (job) (rate({app="foo"}[1m]))
`

const ruleFile = `groups:
  - name: group1
    rules:
      - record: job:requests:rate1m
        expr: sum by (job) (rate({app="foo"}[1m]))
`

func writeRuleFile(t *testing.T, name string) string {
	t.Helper()

	file := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(file, []byte(ruleFile), 0o600))
	return file
}

func TestRulesList(t *testing.T) {
	ruler := newServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(rulesResponse))
	})

	for _, tc := range []struct {
		args []string
		want []string
	}{
		{[]string{"rules", "list"}, []string{"Namespace", "ns1", "group1"}},
		{[]string{"rules", "list", "--format=table"}, []string{"Namespace", "ns1", "group1"}},
		{[]string{"rules", "list", "--format=json"}, []string{`"ns1"`, `"group1"`}},
		{[]string{"rules", "list", "--output=json"}, []string{`"ns1"`, `"group1"`}},
		{[]string{"rules", "list", "--disable-color", "-o", "yaml"}, []string{"namespace: ns1", "rulegroup: group1"}},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			out, err := runApplication(t, append([]string{"--address=" + ruler.URL}, tc.args...)...)
			require.NoError(t, err)
			for _, want := range tc.want {
				require.Contains(t, out, want)
			}
		})
	}

	t.Run("invalid format", func(t *testing.T) {
		_, err := runApplication(t, "--address="+ruler.URL, "rules", "list", "--format=jsonl")
		require.ErrorContains(t, err, "enum value must be one of json,yaml,table")
	})
}

func TestRulesConnection(t *testing.T) {
	ruler := newServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(rulesResponse))
	})

	t.Run("flag aliases", func(t *testing.T) {
		_, err := runApplication(t, "--addr="+ruler.URL, "--id=tenant", "--user=u", "--pass=p", "rules", "list")
		require.NoError(t, err)

		got := ruler.Requests()
		require.NotEmpty(t, got)
		last := got[len(got)-1]
		require.Equal(t, "/loki/api/v1/rules", last.Path)
		require.Equal(t, "tenant", last.Header.Get("X-Scope-OrgID"))
		require.Equal(t, "Basic "+base64.StdEncoding.EncodeToString([]byte("u:p")), last.Header.Get("Authorization"))
	})

	t.Run("environment variables of lokitool", func(t *testing.T) {
		t.Setenv("LOKI_ADDRESS", ruler.URL)
		t.Setenv("LOKI_TENANT_ID", "env-tenant")
		t.Setenv("LOKI_API_KEY", "key")

		_, err := runApplication(t, "rules", "list")
		require.NoError(t, err)

		got := ruler.Requests()
		last := got[len(got)-1]
		require.Equal(t, "env-tenant", last.Header.Get("X-Scope-OrgID"))
		// The password without a username uses the tenant ID as username.
		require.Equal(t, "Basic "+base64.StdEncoding.EncodeToString([]byte("env-tenant:key")), last.Header.Get("Authorization"))
	})

	t.Run("legacy routes", func(t *testing.T) {
		_, err := runApplication(t, "--address="+ruler.URL, "rules", "--use-legacy-routes", "list")
		require.NoError(t, err)

		got := ruler.Requests()
		require.Equal(t, "/api/prom/rules", got[len(got)-1].Path)
	})

	t.Run("server error", func(t *testing.T) {
		failing := newServer(t, func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "boom", http.StatusInternalServerError)
		})

		_, err := runApplication(t, "--address="+failing.URL, "rules", "list")
		require.ErrorContains(t, err, "unable to read rules from loki")
		require.ErrorContains(t, err, "500 Internal Server Error: boom")
	})
}

func TestRulesGetAndDelete(t *testing.T) {
	t.Run("get", func(t *testing.T) {
		ruler := newServer(t, func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("name: group1\nrules:\n    - record: a:b\n      expr: sum(rate({app=\"foo\"}[1m]))\n"))
		})

		out, err := runApplication(t, "--address="+ruler.URL, "rules", "get", "--disable-color", "ns1", "group1")
		require.NoError(t, err)
		require.Contains(t, out, "group1")
		require.Equal(t, "/loki/api/v1/rules/ns1/group1", ruler.Requests()[0].Path)
	})

	t.Run("get unknown group", func(t *testing.T) {
		ruler := newServer(t, func(w http.ResponseWriter, _ *http.Request) {
			http.NotFound(w, nil)
		})

		_, err := runApplication(t, "--address="+ruler.URL, "rules", "get", "ns1", "unknown")
		require.NoError(t, err)
	})

	t.Run("delete", func(t *testing.T) {
		ruler := newServer(t, nil)

		_, err := runApplication(t, "--address="+ruler.URL, "rules", "delete", "ns1", "group1")
		require.NoError(t, err)

		got := ruler.Requests()
		require.Len(t, got, 1)
		require.Equal(t, http.MethodDelete, got[0].Method)
		require.Equal(t, "/loki/api/v1/rules/ns1/group1", got[0].Path)
	})

	t.Run("delete unknown group", func(t *testing.T) {
		ruler := newServer(t, func(w http.ResponseWriter, r *http.Request) {
			http.NotFound(w, r)
		})

		_, err := runApplication(t, "--address="+ruler.URL, "rules", "delete", "ns1", "unknown")
		require.NoError(t, err)
	})
}

func TestRulesSync(t *testing.T) {
	file := writeRuleFile(t, "ns1.yaml")

	t.Run("creates missing group", func(t *testing.T) {
		ruler := newServer(t, func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet {
				_, _ = w.Write([]byte("{}"))
				return
			}
			w.WriteHeader(http.StatusAccepted)
		})

		out, err := runApplication(t, "--address="+ruler.URL, "rules", "sync", file)
		require.NoError(t, err)
		require.Contains(t, out, "Sync Summary: 1 Groups Created, 0 Groups Updated, 0 Groups Deleted")

		got := ruler.Requests()
		require.Len(t, got, 2)
		require.Equal(t, http.MethodGet, got[0].Method)
		require.Equal(t, http.MethodPost, got[1].Method)
		require.Equal(t, "/loki/api/v1/rules/ns1", got[1].Path)
		require.Contains(t, got[1].Body, "name: group1")
	})

	t.Run("diff does not change anything", func(t *testing.T) {
		ruler := newServer(t, func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("{}"))
		})

		out, err := runApplication(t, "--address="+ruler.URL, "rules", "diff", "--disable-color", file)
		require.NoError(t, err)
		require.Contains(t, out, "Diff Summary: 1 Groups Created")

		for _, r := range ruler.Requests() {
			require.Equal(t, http.MethodGet, r.Method)
		}
	})
}

func TestRulesOfflineCommandsDoNotNeedAConnection(t *testing.T) {
	file := writeRuleFile(t, "ns1.yaml")

	// A password without username and org ID is an invalid connection configuration.
	_, err := runApplication(t, "--password=secret", "rules", "format", "--dry-run", file)
	require.NoError(t, err)

	_, err = runApplication(t, "--password=secret", "rules", "check", file)
	require.NoError(t, err)

	_, err = runApplication(t, "--password=secret", "rules", "list")
	require.ErrorContains(t, err, "password requires a username or org-id")
}
