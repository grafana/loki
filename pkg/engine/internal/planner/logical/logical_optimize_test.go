package logical

import (
	"context"
	"embed"
	"io/fs"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/tools/txtar"

	"github.com/grafana/loki/v3/pkg/engine/internal/semconv"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
)

func Test_simplifyRegexPass(t *testing.T) {
	params, err := logql.NewLiteralParams(
		`{job="loki"} !~ "debug|DEBUG|info|INFO" |~ "error|ERROR|fatal|FATAL"`,
		time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2025, time.January, 2, 0, 0, 0, 0, time.UTC),
		0 /* step */, 0, /* duration */
		logproto.BACKWARD,
		1000,
		[]string{"0_of_1"},
		nil,
	)
	require.NoError(t, err)

	expect := strings.TrimSpace(`
%1 = EQ label.job "loki"
%2 = MATCH_STR builtin.message "debug"
%3 = MATCH_STR builtin.message "DEBUG"
%4 = OR %2 %3
%5 = MATCH_STR builtin.message "info"
%6 = OR %4 %5
%7 = MATCH_STR builtin.message "INFO"
%8 = OR %6 %7
%9 = NOT(%8)
%10 = MATCH_STR builtin.message "error"
%11 = MATCH_STR builtin.message "ERROR"
%12 = OR %10 %11
%13 = MATCH_STR builtin.message "fatal"
%14 = OR %12 %13
%15 = MATCH_STR builtin.message "FATAL"
%16 = OR %14 %15
%17 = AND %9 %16
%18 = MAKETABLE [selector=%1, predicates=[%17], shard=0_of_1]
%19 = GTE builtin.timestamp 2025-01-01T00:00:00Z
%20 = SELECT %18 [predicate=%19]
%21 = LT builtin.timestamp 2025-01-02T00:00:00Z
%22 = SELECT %20 [predicate=%21]
%23 = SELECT %22 [predicate=%17]
%24 = TOPK %23 [sort_by=builtin.timestamp, k=1000, asc=false, nulls_first=false]
%25 = LOGQL_COMPAT %24
RETURN %25
`)

	p, err := BuildPlan(context.Background(), params)
	require.NoError(t, err)
	require.NoError(t, Optimize(p), "optimization should not fail")

	actual := strings.TrimSpace(p.String())
	require.Equal(t, expect, actual, "Actual plan:\n%s", actual)
}

func Test_simplifyRegexPass_IgnoreStreamSelector(t *testing.T) {
	// See comment in logical_optimize.go for why stream selectors can't have
	// regex simplification rules applied yet.
	params, err := logql.NewLiteralParams(
		`{cluster=~".*dev.*", job=~"loki.*"} |~ "foo|bar"`,
		time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2025, time.January, 2, 0, 0, 0, 0, time.UTC),
		0 /* step */, 0, /* duration */
		logproto.BACKWARD,
		1000,
		[]string{"0_of_1"},
		nil,
	)
	require.NoError(t, err)

	expect := strings.TrimSpace(`
%1 = MATCH_RE label.cluster ".*dev.*"
%2 = MATCH_RE label.job "loki.*"
%3 = AND %1 %2
%4 = MATCH_STR builtin.message "foo"
%5 = MATCH_STR builtin.message "bar"
%6 = OR %4 %5
%7 = MAKETABLE [selector=%3, predicates=[%6], shard=0_of_1]
%8 = GTE builtin.timestamp 2025-01-01T00:00:00Z
%9 = SELECT %7 [predicate=%8]
%10 = LT builtin.timestamp 2025-01-02T00:00:00Z
%11 = SELECT %9 [predicate=%10]
%12 = SELECT %11 [predicate=%6]
%13 = TOPK %12 [sort_by=builtin.timestamp, k=1000, asc=false, nulls_first=false]
%14 = LOGQL_COMPAT %13
RETURN %14
`)

	p, err := BuildPlan(context.Background(), params)
	require.NoError(t, err)
	require.NoError(t, Optimize(p), "optimization should not fail")

	actual := strings.TrimSpace(p.String())
	require.Equal(t, expect, actual, "Actual plan:\n%s", actual)
}

func Test_simplifyRegexPass_Negate(t *testing.T) {
	params, err := logql.NewLiteralParams(
		`{job="loki"} !~ "foo|bar"`,
		time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2025, time.January, 2, 0, 0, 0, 0, time.UTC),
		0 /* step */, 0, /* duration */
		logproto.BACKWARD,
		1000,
		[]string{"0_of_1"},
		nil,
	)
	require.NoError(t, err)

	expect := strings.TrimSpace(`
%1 = EQ label.job "loki"
%2 = MATCH_STR builtin.message "foo"
%3 = MATCH_STR builtin.message "bar"
%4 = OR %2 %3
%5 = NOT(%4)
%6 = MAKETABLE [selector=%1, predicates=[%5], shard=0_of_1]
%7 = GTE builtin.timestamp 2025-01-01T00:00:00Z
%8 = SELECT %6 [predicate=%7]
%9 = LT builtin.timestamp 2025-01-02T00:00:00Z
%10 = SELECT %8 [predicate=%9]
%11 = SELECT %10 [predicate=%5]
%12 = TOPK %11 [sort_by=builtin.timestamp, k=1000, asc=false, nulls_first=false]
%13 = LOGQL_COMPAT %12
RETURN %13
`)

	p, err := BuildPlan(context.Background(), params)
	require.NoError(t, err)
	require.NoError(t, Optimize(p), "optimization should not fail")

	actual := strings.TrimSpace(p.String())
	require.Equal(t, expect, actual, "Actual plan:\n%s", actual)
}

func Test_simplifyRegexPass_CaseInsensitiveNegate(t *testing.T) {
	params, err := logql.NewLiteralParams(
		`{region="ap-southeast-1"} !~ "(?i)debug"`,
		time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2025, time.January, 2, 0, 0, 0, 0, time.UTC),
		0 /* step */, 0, /* duration */
		logproto.BACKWARD,
		1000,
		[]string{"0_of_1"},
		nil,
	)
	require.NoError(t, err)

	expect := strings.TrimSpace(`
%1 = EQ label.region "ap-southeast-1"
%2 = NOT_MATCH_RE builtin.message "(?i)debug"
%3 = MAKETABLE [selector=%1, predicates=[%2], shard=0_of_1]
%4 = GTE builtin.timestamp 2025-01-01T00:00:00Z
%5 = SELECT %3 [predicate=%4]
%6 = LT builtin.timestamp 2025-01-02T00:00:00Z
%7 = SELECT %5 [predicate=%6]
%8 = SELECT %7 [predicate=%2]
%9 = TOPK %8 [sort_by=builtin.timestamp, k=1000, asc=false, nulls_first=false]
%10 = LOGQL_COMPAT %9
RETURN %10
`)

	p, err := BuildPlan(context.Background(), params)
	require.NoError(t, err)
	require.NoError(t, Optimize(p), "optimization should not panic on case-insensitive negated regex")

	actual := strings.TrimSpace(p.String())
	require.Equal(t, expect, actual, "Actual plan:\n%s", actual)
}

//go:embed testdata/simplifyRegexPass/*.txtar
var simplifyRegexTests embed.FS

func Test_simplifyRegexPass_Apply(t *testing.T) {
	type testCase struct {
		column string
		regex  string

		simplified bool
		expect     string
	}

	parseTestCase := func(t *testing.T, ar *txtar.Archive) testCase {
		t.Helper()

		var (
			setColumn bool
			setRegex  bool
		)

		var tc testCase
		for _, f := range ar.Files {
			switch f.Name {
			case "column":
				tc.column = strings.TrimSpace(string(f.Data))
				setColumn = true
			case "regex":
				tc.regex = strings.TrimSpace(string(f.Data))
				setRegex = true
			case "expect":
				tc.simplified = true
				tc.expect = strings.TrimSpace(string(f.Data))
			default:
				t.Fatalf("unexpected file %q in txtar archive", f.Name)
			}
		}

		require.True(t, setColumn, "no column name found")
		require.True(t, setRegex, "no regex found")

		return tc
	}

	_ = fs.WalkDir(simplifyRegexTests, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}

		testName := strings.TrimSuffix(d.Name(), ".txtar")
		t.Run(testName, func(t *testing.T) {
			data, err := simplifyRegexTests.ReadFile(path)
			require.NoError(t, err)
			tc := parseTestCase(t, txtar.Parse(data))

			id, err := semconv.ParseFQN(tc.column)
			require.NoError(t, err, "column is not a valid column name")

			orig := &BinOp{
				Left:  &ColumnRef{Ref: id.ColumnRef()},
				Op:    types.BinaryOpMatchRe,
				Right: NewLiteral(tc.regex),
			}

			var pass simplifyRegexPass
			instrs, changed, err := pass.simplifyBinop(orig)
			require.NoError(t, err)
			require.Equal(t, changed, tc.simplified)

			if tc.simplified {
				// Build a fake plan so we can print it.
				p, err := convertToPlan(instrs[len(instrs)-1].(Value))
				require.NoError(t, err, "should not fail when converting optimized result to a plan")

				actual := strings.TrimSpace(p.String())
				require.Equal(t, tc.expect, actual, "Actual plan:\n%s", actual)
			}
		})

		return nil
	})
}
