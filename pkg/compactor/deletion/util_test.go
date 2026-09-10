package deletion

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseLogQLExpressionForDeletion(t *testing.T) {
	for _, tc := range []struct {
		name        string
		query       string
		errContains string
	}{
		{"invalid logql", "gjgjg ggj", "syntax error"},
		{"pipeline expression with invalid line filter", `{env="dev", secret="true"} |= social sec number`, "syntax error"},
		{"matcher matching everything", `{env=~".*"}`, "queries require at least one regexp or equality matcher"},
		{"only empty-compatible matchers", `{env!="dev"}`, "queries require at least one regexp or equality matcher"},
		{"unclosed character class in line filter", `{env="dev"} |~ "["`, "error parsing regexp: missing closing ]"},
		{"unclosed character class in negated line filter", `{env="dev"} !~ "["`, "error parsing regexp: missing closing ]"},
		{"invalid repetition in label filter regex", `{env="dev"} | addr=~"*"`, "error parsing regexp: missing argument to repetition operator"},
		{"invalid ip pattern in label filter", `{env="dev"} | addr=ip("not-an-ip")`, `ip: invalid pattern: "not-an-ip"`},
		{"out of range ip in label filter", `{env="dev"} | addr=ip("192.168.0.500")`, `ip: invalid pattern: "192.168.0.500"`},
		{"truncated ip after parser stage", `{env="dev"} | json | addr=ip("1.2.3")`, `ip: invalid pattern: "1.2.3"`},
		{"invalid ip pattern in line filter", `{env="dev"} |= ip("garbage")`, `ip: invalid pattern: "garbage"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			logSelectorExpr, err := parseDeletionQuery(tc.query)
			require.Nil(t, logSelectorExpr)
			require.ErrorIs(t, err, errInvalidQuery)
			// the reason has to reach the caller, "invalid query expression" alone is not actionable
			require.ErrorContains(t, err, tc.errContains)
		})
	}

	for _, tc := range []struct {
		name  string
		query string
	}{
		{"matcher expression", `{env="dev", secret="true"}`},
		{"regex matcher requiring a value", `{env=~".+"}`},
		{"pipeline expression with line filter", `{env="dev", secret="true"} |= "social sec number"`},
		{"pipeline expression with multiple line filters", `{env="dev", secret="true"} |= "social sec number" |~ "[abd]*" `},
		{"valid regex line filter", `{env="dev"} |~ "[abc]+"`},
		{"valid ip in label filter", `{env="dev"} | addr=ip("192.168.0.1")`},
		{"valid ip range in line filter", `{env="dev"} |= ip("192.168.4.5-192.168.4.20")`},
		{"valid ip cidr in line filter", `{env="dev"} |= ip("192.168.4.0/24")`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			logSelectorExpr, err := parseDeletionQuery(tc.query)
			require.NotNil(t, logSelectorExpr)
			require.NoError(t, err)
		})
	}
}
