package hintprovider

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNeedleKey(t *testing.T) {
	const ngram = 6

	key := func(t *testing.T, query string, ngramLength int) string {
		t.Helper()
		k, ok := needleKey(mustParseExpr(t, query), ngramLength)
		require.True(t, ok, query)
		return k
	}

	base := key(t, `{app="api"} |= "timeout"`, ngram)

	// Queries with the same needles share a key, however the rest of the query
	// differs.
	for _, q := range []string{
		// The lookup does not use stream matchers, so they are not part of the key.
		`{app="web"} |= "timeout"`,
		`{app="api", env="prod"} |= "timeout"`,
		`{app="api"} |= "timeout" | drop __error__`,
		`sum by (level, detected_level) (count_over_time({app="api"} |= "timeout" | drop __error__ [1m]))`,
		`rate({app="api"} |= "timeout" [5m] offset 10m)`,
		`{app="api"} |= "timeout" | json | line_format "{{.msg}}"`,
		`{app="api"} |= "TIMEOUT"`,
		`{app="api"} |~ "timeout"`,
		// The drop stops the later filter from becoming a needle, so the lookup is
		// the same as for the plain query.
		`{app="api"} |= "timeout" | json | drop __error__ | level="critical"`,
	} {
		require.Equal(t, base, key(t, q, ngram), q)
	}
	require.Equal(t,
		key(t, `{app="api", env="prod"} |= "timeout" |= "failure"`, ngram),
		key(t, `{env="prod", app="api"} |= "failure" |= "timeout"`, ngram),
		"matcher and filter order do not matter")

	// Anything that changes the lookup changes the key.
	for _, q := range []string{
		`{app="api"} |= "timeouts"`,
		`{app="api"} |= "timeout" |= "failure"`,
		`{app="api"} |= "timeout" | json | level="critical"`,
	} {
		require.NotEqual(t, base, key(t, q, ngram), q)
	}
	require.NotEqual(t, base, key(t, `{app="api"} |= "timeout"`, 5), "n-gram length")

	// Needles are arbitrary bytes, so one needle containing the separator must
	// not look like two.
	require.NotEqual(t,
		key(t, `{app="api"} |= "a,b"`, 1),
		key(t, `{app="api"} |= "a" |= "b"`, 1))
}

func TestNeedleKey_NotUsable(t *testing.T) {
	for name, tc := range map[string]struct {
		query string
		ngram int
	}{
		"no needles":          {`{app="api"}`, 6},
		"only a short needle": {`{app="api"} |= "err"`, 6},
		"unsupported filter":  {`{app="api"} |= "a" or "b"`, 6},
		"no n-gram length":    {`{app="api"} |= "timeout"`, 0},
		"binary operation":    {`count_over_time({app="api"} |= "timeout" [5m]) / count_over_time({app="api"} |= "timeout" [5m])`, 6},
	} {
		t.Run(name, func(t *testing.T) {
			_, ok := needleKey(mustParseExpr(t, tc.query), tc.ngram)
			require.False(t, ok)
		})
	}
}
