package local

import (
	"context"
	"os"
	"path"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/rulefmt"
	"github.com/prometheus/prometheus/promql/parser"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"

	"github.com/grafana/loki/v3/pkg/ruler/rulespb"
)

func TestClient_LoadAllRuleGroups(t *testing.T) {
	user1 := "user"
	user2 := "second-user"

	namespace1 := "ns"
	namespace2 := "z-another" // This test relies on the fact that ioutil.ReadDir() returns files sorted by name.

	dir := t.TempDir()

	ruleGroups := rulefmt.RuleGroups{
		Groups: []rulefmt.RuleGroup{
			{
				Name:     "rule",
				Interval: model.Duration(100 * time.Second),
				Rules: []rulefmt.Rule{
					{
						Record: "test_rule",
						Expr:   "up",
					},
				},
			},
		},
	}

	b, err := yaml.Marshal(ruleGroups)
	require.NoError(t, err)

	err = os.MkdirAll(path.Join(dir, user1), 0777)
	require.NoError(t, err)

	// Link second user to first.
	err = os.Symlink(user1, path.Join(dir, user2))
	require.NoError(t, err)

	err = os.WriteFile(path.Join(dir, user1, namespace1), b, 0777)
	require.NoError(t, err)

	const ignoredDir = "ignored-dir"
	err = os.Mkdir(path.Join(dir, user1, ignoredDir), os.ModeDir|0644)
	require.NoError(t, err)

	err = os.Symlink(ignoredDir, path.Join(dir, user1, "link-to-dir"))
	require.NoError(t, err)

	// Link second namespace to first.
	err = os.Symlink(namespace1, path.Join(dir, user1, namespace2))
	require.NoError(t, err)

	client, err := NewLocalRulesClient(Config{
		Directory: dir,
	}, testFileLoader{})
	require.NoError(t, err)

	ctx := context.Background()
	userMap, err := client.ListAllRuleGroups(ctx) // Client loads rules in its List method.
	require.NoError(t, err)

	for _, u := range []string{user1, user2} {
		actual, found := userMap[u]
		require.True(t, found)

		require.Equal(t, 2, len(actual))
		// We rely on the fact that files are parsed in alphabetical order, and our namespace1 < namespace2.
		require.Equal(t, rulespb.ToProto(u, namespace1, ruleGroups.Groups[0]), actual[0])
		require.Equal(t, rulespb.ToProto(u, namespace2, ruleGroups.Groups[0]), actual[1])
	}
}

type testFileLoader struct{}

func (testFileLoader) Load(identifier string, ignoreUnknownFields bool, nameValidationScheme model.ValidationScheme) (*rulefmt.RuleGroups, []error) {
	return rulefmt.ParseFile(identifier, ignoreUnknownFields, nameValidationScheme, parser.NewParser(parser.Options{}))
}

func (testFileLoader) Parse(query string) (parser.Expr, error) {
	return parser.NewParser(parser.Options{}).ParseExpr(query)
}
