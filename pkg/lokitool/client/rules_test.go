package client

import (
	"context"
	"net/http"
	"testing"

	"github.com/prometheus/prometheus/model/rulefmt"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/lokitool/rules/rwrulefmt"
)

func TestRulesPath(t *testing.T) {
	for _, tc := range []struct {
		test       string
		legacy     bool
		namespace  string
		name       string
		expURLPath string
	}{
		{
			test:       "regular-characters",
			namespace:  "my-namespace",
			name:       "my-name",
			expURLPath: "/loki/api/v1/rules/my-namespace/my-name",
		},
		{
			test:       "legacy-routes",
			legacy:     true,
			namespace:  "my-namespace",
			name:       "my-name",
			expURLPath: "/api/prom/rules/my-namespace/my-name",
		},
		{
			test:       "special-characters-spaces",
			namespace:  "My: Namespace",
			name:       "My: Name",
			expURLPath: "/loki/api/v1/rules/My:%20Namespace/My:%20Name",
		},
		{
			test:       "special-characters-slashes",
			namespace:  "My/Namespace",
			name:       "My/Name",
			expURLPath: "/loki/api/v1/rules/My%2FNamespace/My%2FName",
		},
		{
			test:       "special-characters-slash-first",
			namespace:  "My/Namespace",
			name:       "/first-char-slash",
			expURLPath: "/loki/api/v1/rules/My%2FNamespace/%2Ffirst-char-slash",
		},
		{
			test:       "special-characters-slash-last",
			namespace:  "My/Namespace",
			name:       "last-char-slash/",
			expURLPath: "/loki/api/v1/rules/My%2FNamespace/last-char-slash%2F",
		},
	} {
		t.Run(tc.test, func(t *testing.T) {
			srv, requests := newServer(t, http.StatusOK, "hello")
			c, err := New(Config{Address: srv.URL, OrgID: "my-id", UseLegacyRoutes: tc.legacy})
			require.NoError(t, err)

			require.NoError(t, c.DeleteRuleGroup(context.Background(), tc.namespace, tc.name))

			got := requests()
			require.Len(t, got, 1)
			require.Equal(t, http.MethodDelete, got[0].Method)
			require.Equal(t, tc.expURLPath, got[0].Path)
			require.Equal(t, "my-id", got[0].Header.Get(HeaderOrgID))
		})
	}
}

func TestRules(t *testing.T) {
	const group = `name: group1
rules:
    - record: job:up:sum
      expr: sum by (job) (up)
`

	t.Run("get", func(t *testing.T) {
		srv, _ := newServer(t, http.StatusOK, group)
		c, err := New(Config{Address: srv.URL})
		require.NoError(t, err)

		rg, err := c.GetRuleGroup(context.Background(), "ns", "group1")
		require.NoError(t, err)
		require.Equal(t, "group1", rg.Name)
		require.Len(t, rg.Rules, 1)
		require.Equal(t, "job:up:sum", rg.Rules[0].Record)
	})

	t.Run("get unknown", func(t *testing.T) {
		srv, _ := newServer(t, http.StatusNotFound, "group does not exist")
		c, err := New(Config{Address: srv.URL})
		require.NoError(t, err)

		_, err = c.GetRuleGroup(context.Background(), "ns", "unknown")
		require.ErrorIs(t, err, ErrResourceNotFound)
	})

	t.Run("list", func(t *testing.T) {
		srv, requests := newServer(t, http.StatusOK, "ns1:\n    - "+group[:len(group)-1]+"\n")
		c, err := New(Config{Address: srv.URL})
		require.NoError(t, err)

		rules, err := c.ListRules(context.Background(), "")
		require.NoError(t, err)
		require.Len(t, rules["ns1"], 1)
		require.Equal(t, "/loki/api/v1/rules", requests()[0].Path)

		_, err = c.ListRules(context.Background(), "my ns")
		require.NoError(t, err)
		require.Equal(t, "/loki/api/v1/rules/my%20ns", requests()[1].Path)
	})

	t.Run("create", func(t *testing.T) {
		srv, requests := newServer(t, http.StatusAccepted, "")
		c, err := New(Config{Address: srv.URL})
		require.NoError(t, err)

		rg := rwrulefmt.RuleGroup{RuleGroup: rulefmt.RuleGroup{
			Name:  "group1",
			Rules: []rulefmt.Rule{{Record: "job:up:sum", Expr: "sum by (job) (up)"}},
		}}
		require.NoError(t, c.CreateRuleGroup(context.Background(), "ns/1", rg))

		got := requests()
		require.Len(t, got, 1)
		require.Equal(t, http.MethodPost, got[0].Method)
		require.Equal(t, "/loki/api/v1/rules/ns%2F1", got[0].Path)
		require.Contains(t, got[0].Body, "record: job:up:sum")
	})
}
