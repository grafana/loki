package hintprovider

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logline"
	"github.com/grafana/loki/v3/pkg/logline/needle"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
)

func TestSupportedQuery_Or(t *testing.T) {
	for _, tc := range []struct {
		name            string
		query           string
		maxConjunctions int
		want            string
	}{
		// Line filter or.
		{"line filter or", `{app="foo"} |= "abcdef" or "ghijkl"`, 64, `{"ABCDEF"} or {"GHIJKL"}`},
		{
			"or next to a required filter", `{app="foo"} |= "seed text" |= "option_a" or "option_b"`, 64,
			`{"OPTION_A", "SEED TEXT"} or {"OPTION_B", "SEED TEXT"}`,
		},
		{"or chain of three", `{app="foo"} |= "abcdef" or "ghijkl" or "mnopqr"`, 64, `{"ABCDEF"} or {"GHIJKL"} or {"MNOPQR"}`},
		{"or absorbs a longer branch", `{app="foo"} |= "timeout" or "timeouts"`, 64, `{"TIMEOUT"}`},
		{"regex or chain", `{app="foo"} |~ "abcdef" or "ghi.*jkl123"`, 64, `{"ABCDEF"} or {"JKL123"}`},

		// A branch we cannot look up makes the whole or True.
		{"or with a short branch", `{app="foo"} |= "abcdef" or "ab"`, 64, "TRUE"},
		{"or with an empty branch", `{app="foo"} |= "abcdef" or ""`, 64, "TRUE"},
		{"or with an ip branch", `{app="foo"} |= "abcdef" or ip("10.0.0.1")`, 64, "TRUE"},
		{"true or next to a required filter", `{app="foo"} |= "seed text" |= "abcdef" or "ab"`, 64, `{"SEED TEXT"}`},
		{"negative or is an and of negatives", `{app="foo"} |= "seed text" != "abcdef" or "ghijkl"`, 64, `{"SEED TEXT"}`},

		// Regex alternation.
		{"regex alternation", `{app="foo"} |~ "abcdef|ghijkl"`, 64, `{"ABCDEF"} or {"GHIJKL"}`},
		{"regex alternation joined to text", `{app="foo"} |~ "(GET|POST) /api/users"`, 64, `{"GET /API/USERS"} or {"POST /API/USERS"}`},
		{"regex alternation with a short branch", `{app="foo"} |~ "abcdef|ab"`, 64, "TRUE"},

		// Label filters.
		{"label or", `{app="foo"} | x="abcdef" or y="ghijkl"`, 64, `{"ABCDEF"} or {"GHIJKL"}`},
		{"label and", `{app="foo"} | x="abcdef" and y="ghijkl"`, 64, `{"ABCDEF", "GHIJKL"}`},
		{"label regex alternation", `{app="foo"} | x=~"abcdef|ghijkl"`, 64, `{"ABCDEF"} or {"GHIJKL"}`},
		{"label or with a numeric branch", `{app="foo"} | x="abcdef" or y > 5`, 64, "TRUE"},
		{"label or with a reserved name", `{app="foo"} | x="abcdef" or __error__="ghijkl"`, 64, "TRUE"},

		// Post-parser filters.
		{"post-parser or", `{app="foo"} | json | x="abcdef" or y="ghijkl"`, 64, `{"ABCDEF"} or {"GHIJKL"}`},
		{"post-parser or with a non-verbatim branch", `{app="foo"} | json | x="abcdef" or y="ghi\"jkl"`, 64, "TRUE"},

		// maxConjunctions.
		{
			"product of two ors", `{app="foo"} |= "aaaaaa" or "bbbbbb" |= "cccccc" or "dddddd"`, 64,
			`{"AAAAAA", "CCCCCC"} or {"AAAAAA", "DDDDDD"} or {"BBBBBB", "CCCCCC"} or {"BBBBBB", "DDDDDD"}`,
		},
		{
			"product over the limit collapses one or", `{app="foo"} |= "aaaaaa" or "bbbbbb" |= "cccccc" or "dddddd"`, 3,
			`{"CCCCCC"} or {"DDDDDD"}`,
		},
		{
			"collapse keeps the shared literal", `{app="foo"} |~ "(abc|def|ghi)_shared_suffix"`, 2,
			`{"_SHARED_SUFFIX"}`,
		},
		{"one conjunction keeps today's needles", `{app="foo"} |= "seed text" |= "option_a" or "option_b"`, 1, `{"SEED TEXT"}`},
		{"one conjunction without shared needles", `{app="foo"} |= "abcdef" or "ghijkl"`, 1, "TRUE"},
		{"zero means the default", `{app="foo"} |= "abcdef" or "ghijkl"`, 0, `{"ABCDEF"} or {"GHIJKL"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			expr, err := syntax.ParseExpr(tc.query)
			require.NoError(t, err)
			require.Equal(t, tc.want, SupportedQuery(expr, 6, tc.maxConjunctions).String())
		})
	}
}

// Random pipelines of line filters and or chains are run by Loki on random
// lines. Every line Loki keeps must contain all the terms of some needle set,
// for every index version. Otherwise the hint would drop it.
func TestSupportedQuery_NoFalseNegatives(t *testing.T) {
	const ngramLength = 3
	maxConjunctions := []int{1, 4, 64}
	rng := rand.New(rand.NewSource(1))
	stream := labels.FromStrings("app", "foo")

	checked, multi := 0, 0
	for range 3000 {
		query := randomQuery(rng)
		expr, err := syntax.ParseLogSelector(query, true)
		require.NoError(t, err, query)
		pipeline, err := expr.Pipeline()
		require.NoError(t, err, query)
		sp := pipeline.ForStream(stream)

		exprs := make([]needle.Expr, len(maxConjunctions))
		for i, mc := range maxConjunctions {
			exprs[i] = SupportedQuery(expr, ngramLength, mc)
			require.LessOrEqual(t, len(exprs[i].Conjunctions()), mc)
		}

		for range 30 {
			line := randomQueryLine(rng)
			if _, _, ok := sp.Process(0, []byte(line), labels.EmptyLabels()); !ok {
				continue
			}
			checked++
			if len(exprs[len(exprs)-1].Conjunctions()) > 1 {
				multi++
			}
			for _, version := range logline.AllVersions() {
				indexed := lineTerms(t, line, ngramLength, version)
				for i, e := range exprs {
					if !someSetIndexed(t, e, ngramLength, version, indexed) {
						t.Fatalf("false negative: version=%s maxConjunctions=%d query=%s line=%q needles=%v",
							version, maxConjunctions[i], query, line, e)
					}
				}
			}
		}
	}
	require.Greater(t, checked, 5000)
	require.Greater(t, multi, 1000)
}

var queryWords = []string{"abc", "bcd", "abcd", "cab", "dab", "ab", ""}

func randomQueryWord(rng *rand.Rand) string {
	return queryWords[rng.Intn(len(queryWords))]
}

// randomQuery builds up to three line filter stages. Each is |=, |~ or !=,
// sometimes with an or chain. An |= chain sometimes has an ip() branch, ip()
// is only valid there.|=, |~ or !=,
// sometimes with an or chain, and sometimes an ip() branch.
func randomQuery(rng *rand.Rand) string {
	var sb strings.Builder
	sb.WriteString(`{app="foo"}`)
	for range 1 + rng.Intn(3) {
		op := []string{"|=", "|=", "|~", "!="}[rng.Intn(4)]
		fmt.Fprintf(&sb, " %s %s", op, randomOperand(rng, op))
		for range rng.Intn(3) {
			sb.WriteString(" or ")
			if op == "|=" && rng.Intn(8) == 0 {
				sb.WriteString(`ip("10.0.0.1")`)
				continue
			}
			sb.WriteString(randomOperand(rng, op))
		}
	}
	return sb.String()
}

func randomOperand(rng *rand.Rand, op string) string {
	if op != "|~" {
		return fmt.Sprintf("%q", randomQueryWord(rng))
	}
	switch rng.Intn(4) {
	case 0:
		return fmt.Sprintf("%q", randomQueryWord(rng)+"|"+randomQueryWord(rng))
	case 1:
		return fmt.Sprintf("%q", "("+randomQueryWord(rng)+"|"+randomQueryWord(rng)+")"+randomQueryWord(rng))
	case 2:
		return fmt.Sprintf("%q", randomQueryWord(rng)+".*"+randomQueryWord(rng))
	default:
		return fmt.Sprintf("%q", randomQueryWord(rng)+"s?")
	}
}

func randomQueryLine(rng *rand.Rand) string {
	var sb strings.Builder
	for range rng.Intn(5) {
		sb.WriteString(randomQueryWord(rng))
		if rng.Intn(3) == 0 {
			sb.WriteByte(" -sx"[rng.Intn(4)])
		}
	}
	return sb.String()
}

func lineTerms(t *testing.T, line string, n int, version string) map[string]struct{} {
	t.Helper()
	extract, err := logline.ExtractorForVersion(version)
	require.NoError(t, err)
	format, err := logline.FormatterForVersion(version)
	require.NoError(t, err)
	out := map[string]struct{}{}
	for _, key := range extract(n, line, nil, nil, nil) {
		out[format(key, n)] = struct{}{}
	}
	return out
}

// someSetIndexed reports whether every term of some set of e is indexed.
// When some set has no term under version, the query is unconstrained there
// (buildTermJobs returns ErrUnconstrained), so it cannot miss.
func someSetIndexed(t *testing.T, e needle.Expr, n int, version string, indexed map[string]struct{}) bool {
	t.Helper()
	if e.IsTrue() {
		return true
	}
	for _, conj := range e.Conjunctions() {
		all, hasTerms := true, false
		for _, lit := range conj {
			terms, err := ExtractQueryNgrams(lit, n, version)
			require.NoError(t, err)
			for _, term := range terms {
				hasTerms = true
				if _, ok := indexed[term]; !ok {
					all = false
				}
			}
		}
		if !hasTerms || all {
			return true
		}
	}
	return false
}

func BenchmarkSupportedQuery(b *testing.B) {
	ids := make([]string, 1000)
	for i := range ids {
		ids[i] = fmt.Sprintf("%016x", uint64(i)*0x9E3779B97F4A7C15)
	}
	var product strings.Builder
	product.WriteString(`{app="foo"}`)
	for i := range 6 {
		fmt.Fprintf(&product, ` |= "clause%d_a" or "clause%d_b"`, i, i)
	}

	for _, bc := range []struct {
		name  string
		query string
	}{
		{"trace_id_alternation_1000", `{app="foo"} |~ "trace_id=(` + strings.Join(ids, "|") + `)"`},
		{"or_product_6_clauses", product.String()},
	} {
		expr, err := syntax.ParseExpr(bc.query)
		require.NoError(b, err)
		b.Run(bc.name, func(b *testing.B) {
			for b.Loop() {
				SupportedQuery(expr, 6, DefaultMaxConjunctions)
			}
		})
	}
}
