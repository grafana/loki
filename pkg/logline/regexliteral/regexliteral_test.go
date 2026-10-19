package regexliteral

import (
	"math/rand"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/grafana/regexp"
	"github.com/grafana/regexp/syntax"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logline"
	"github.com/grafana/loki/v3/pkg/logline/needle"
)

func TestLiterals(t *testing.T) {
	for _, tc := range []struct {
		name    string
		pattern string
		want    []string
	}{
		// Plain literals.
		{"literal", `database timeout`, []string{"database timeout"}},
		{"escaped metacharacters", `a\.b\*c`, []string{"a.b*c"}},
		{"quoted", `\Qa.b*c\E`, []string{"a.b*c"}},
		{"empty pattern", ``, nil},

		// Wildcards around and between literals.
		{"wildcard wrapped", `.*2254266819291842746.*`, []string{"2254266819291842746"}},
		{"leading space kept", `.* 2254266819291842746.*`, []string{" 2254266819291842746"}},
		{"dot all", `(?s).*needle.*`, []string{"needle"}},
		{"two runs", `.*kubelet.*error_code=500123.*`, []string{"error_code=500123", "kubelet"}},
		{"char class splits", `request_id=[0-9a-f]+ status=timeout`, []string{" status=timeout", "request_id="}},
		{"digit class splits", `foo\d+barbazqux`, []string{"barbazqux", "foo"}},
		{"single dot splits", `data.ase`, []string{"ase", "data"}},
		{"duplicates removed", `abcdef.*abcdef`, []string{"abcdef"}},
		{"contained removed", `abcdef.*xabcdefy`, []string{"xabcdefy"}},

		// Anchors are zero-width, so the literals on both sides join.
		{"anchored both ends", `^start_of.*end$`, []string{"end", "start_of"}},
		{"anchor inside", `abc$\ndef`, []string{"abc\ndef"}},
		{"word boundary inside", `abc\bdef`, []string{"abcdef"}},
		{"multiline anchors", `(?m)^level=error$`, []string{"level=error"}},

		// Captures are transparent.
		{"capture", `abc(def)ghi`, []string{"abcdefghi"}},
		{"nested captures", `((abc)(def))`, []string{"abcdef"}},
		{"capture with wildcard", `x(abc.*def)y`, []string{"defy", "xabc"}},

		// Repeats. The first min copies join the left, the last copy joins the right.
		{"counted repeat", `(abc){2}`, []string{"abcabc"}},
		{"counted range", `(abc){2,3}x`, []string{"abcabc", "abcx"}},
		{"plus joins both sides", `x(abcdef)+ghi`, []string{"abcdefghi", "xabcdef"}},
		{"plus on a concat", `(abc.*def)+ghi`, []string{"abc", "def", "ghi"}},
		{"plus on single rune", `ab+cd`, []string{"ab", "bcd"}},
		{"optional", `x(?:abcdef)?y`, []string{"x", "y"}},
		{"star", `(abcdef)*`, nil},

		// Alternation contributes only what every branch shares, but the text
		// around it does.
		{"alternation", `foo|bar`, nil},
		{"alternation with suffix", `(foo|bar)bazqux`, []string{"bazqux"}},
		// Parse factors the common prefix, which becomes a required literal.
		{"factored prefix", `abcdefgh|abcdefxy`, []string{"abcdef"}},
		{"alternation then literal", `(GET|POST) /api`, []string{" /api"}},
		// Literals shared by every branch are required.
		{"shared branch literal", `a.*xyz|b.*xyz`, []string{"xyz"}},
		{"shared branch literal then suffix", `(foo.*bar|baz.*bar)qux`, []string{"bar", "qux"}},

		// Case folding. Literals keep the parser's canonical form, uppercase for ASCII.
		{"case-insensitive", `(?i)ERROR TIMEOUT`, []string{"ERROR TIMEOUT"}},
		{"case-insensitive wrapped", `(?i).*error.*`, []string{"ERROR"}},
		{"case-insensitive k and s", `(?i)kubelet_restarted`, []string{"KUBELET_RESTARTED"}},
		{"case-insensitive scope", `abc(?i:DEF)ghi`, []string{"abcDEFghi"}},
		{"case-insensitive tail", `KS(?i)KS`, []string{"KSKS"}},
		{"case-insensitive char class", `[Aa]bcdef`, []string{"Abcdef"}},
		{"case-insensitive non-ASCII", `(?i)caféé`, []string{"CAFÉÉ"}},
		{"case-insensitive dotted I stays non-ASCII", "(?i)x\u0130stanbul", []string{"X\u0130STANBUL"}},
		{"case-insensitive unicode same group as ascii", `(?i)databaſe error`, []string{"DATABASE ERROR"}},

		// Nothing required.
		{"dot plus", `.+`, nil},
		{"class only", `[a-z]+`, nil},
		{"invalid", `(`, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, Literals(tc.pattern))
		})
	}
}

func TestNeedles(t *testing.T) {
	for _, tc := range []struct {
		name            string
		pattern         string
		maxConjunctions int
		want            string
	}{
		// Alternation.
		{"alternation", `foo|bar`, 64, `{"bar"} or {"foo"}`},
		{"alternation joined to a suffix", `(GET|POST) /api/users`, 64, `{"GET /api/users"} or {"POST /api/users"}`},
		{"alternation joined to a prefix", `user_id=(123456|789012)`, 64, `{"user_id=123456"} or {"user_id=789012"}`},
		{"factored into a class", `error_code=500|error_code=503`, 64, `{"error_code=500"} or {"error_code=503"}`},
		{"alternation on both sides", `(GET|PUT) /api (ok|fail)`, 64, `{"GET /api fail"} or {"GET /api ok"} or {"PUT /api fail"} or {"PUT /api ok"}`},
		{"single letters become a class", `(a|b)xyz(c|d)`, 64, `{"AxyzC"} or {"AxyzD"} or {"BxyzC"} or {"BxyzD"}`},
		{"case-insensitive", `(?i)timeout|error`, 64, `{"ERROR"} or {"TIMEOUT"}`},
		{"case-insensitive letter in a class", `(?i)status=ok`, 64, `{"STATUS=OK"}`},

		// Optional parts.
		{"optional rune", `colou?r`, 64, `{"color"} or {"colour"}`},
		{"optional suffix absorbs", `errors?`, 64, `{"error"}`},
		{"optional prefix absorbs", `x?abc`, 64, `{"abc"}`},

		// Repeats.
		{"plus on an alternation", `(abc|def)+ghi`, 64, `{"abcghi"} or {"defghi"}`},
		{"plus on a literal", `x(abcdef)+ghi`, 64, `{"abcdefghi", "xabcdef"}`},

		// Small classes become exact sets. Large ones split the run.
		{"small class", `id=[0-3]x`, 64, `{"id=0x"} or {"id=1x"} or {"id=2x"} or {"id=3x"}`},
		{"class letters uppercased", `[ab]cd`, 64, `{"Acd"} or {"Bcd"}`},
		{"large class splits", `request_id=[0-9a-f]+ status=timeout`, 64, `{" status=timeout", "request_id="}`},

		// A run that would grow past maxExact is split.
		// Here the first 16 strings are flushed and the run restarts at the
		// third class. With 16 conjunctions the product with them collapses.
		{"run split", `[0-3][0-3][0-3]x`, 16, `{"0x"} or {"1x"} or {"2x"} or {"3x"}`},

		// Non-exact alternation, each branch walked on its own.
		{"wildcard branches", `abc.*def|xyz.*uvw`, 64, `{"abc", "def"} or {"uvw", "xyz"}`},
		{"branch with nothing required", `abc|.*`, 64, "TRUE"},
		{"empty branch", `abc|`, 64, "TRUE"},
		{"alternation next to required text", `kubelet.*(abc.*|def.*)`, 64, `{"abc", "kubelet"} or {"def", "kubelet"}`},

		// maxConjunctions. Exact sets are capped at it, so the plain text next
		// to an alternation survives a low maxConjunctions.
		{"one conjunction keeps the plain literal", `(a|b)verylongliteral`, 1, `{"verylongliteral"}`},
		{"two conjunctions collapse to shared text", `(a|b|c)xyz`, 2, `{"xyz"}`},
		{"one conjunction without required text", `foo|bar`, 1, "TRUE"},

		// Nothing required.
		{"star", `(abcdef)*`, 64, "TRUE"},
		{"class only", `[a-z]+`, 64, "TRUE"},
		{"empty pattern", ``, 64, "TRUE"},
		{"invalid", `(`, 64, "TRUE"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, Needles(tc.pattern, tc.maxConjunctions).Prune().String())
		})
	}
}

// For random patterns and random lines they match, some conjunction must have
// every n-gram of every literal emitted by the index for that line, for every
// index version and at every maxConjunctions. Otherwise a lookup would miss the line.
func TestNeedles_NoFalseNegatives(t *testing.T) {
	maxConjunctionss := []int{1, 4, 64}
	rng := rand.New(rand.NewSource(1))
	checked, alternatives := 0, 0
	for range 20000 {
		pattern := randomPattern(rng, 3)
		re, err := regexp.Compile(pattern)
		if err != nil {
			continue
		}
		exprs := make([]needle.Expr, len(maxConjunctionss))
		allTrue := true
		for i, maxConjunctions := range maxConjunctionss {
			exprs[i] = Needles(pattern, maxConjunctions)
			require.LessOrEqual(t, len(exprs[i].Conjunctions()), maxConjunctions)
			allTrue = allTrue && exprs[i].IsTrue()
		}
		if allTrue {
			continue
		}
		parsed, err := syntax.Parse(pattern, syntax.Perl)
		require.NoError(t, err)

		for range 4 {
			var sb strings.Builder
			sb.WriteString(randomText(rng))
			generateMatch(rng, parsed, &sb)
			sb.WriteString(randomText(rng))
			line := sb.String()
			// Generation ignores anchors, so let the engine decide.
			if !re.MatchString(line) {
				continue
			}
			checked++
			if len(exprs[len(exprs)-1].Conjunctions()) > 1 {
				alternatives++
			}
			for _, version := range logline.AllVersions() {
				for n := 1; n <= 6; n++ {
					indexed := map[string]struct{}{}
					for _, term := range terms(t, version, n, line) {
						indexed[term] = struct{}{}
					}
					for i, e := range exprs {
						if !satisfiesSome(t, version, n, e, indexed) {
							t.Fatalf("false negative: version=%s n=%d maxConjunctions=%d pattern=%q line=%q needles=%v",
								version, n, maxConjunctionss[i], pattern, line, e)
						}
					}
				}
			}
		}
	}
	require.Greater(t, checked, 1000)
	require.Greater(t, alternatives, 10000)
}

// satisfiesSome reports whether every term of some conjunction of e is
// indexed.
func satisfiesSome(t *testing.T, version string, n int, e needle.Expr, indexed map[string]struct{}) bool {
	t.Helper()
	if e.IsTrue() {
		return true
	}
	for _, conj := range e.Conjunctions() {
		all := true
		for _, lit := range conj {
			for _, term := range terms(t, version, n, lit) {
				if !isIndexed(indexed, term) {
					all = false
				}
			}
		}
		if all {
			return true
		}
	}
	return false
}

// For random patterns and random lines they match, every n-gram of every
// literal must be one the index emits for that line, for every index version.
// Otherwise a lookup would miss the line.
func TestLiterals_NoFalseNegatives(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	checked := 0
	for range 20000 {
		pattern := randomPattern(rng, 3)
		re, err := regexp.Compile(pattern)
		if err != nil {
			continue
		}
		literals := Literals(pattern)
		if len(literals) == 0 {
			continue
		}
		parsed, err := syntax.Parse(pattern, syntax.Perl)
		require.NoError(t, err)

		for range 4 {
			var sb strings.Builder
			sb.WriteString(randomText(rng))
			generateMatch(rng, parsed, &sb)
			sb.WriteString(randomText(rng))
			line := sb.String()
			// Generation ignores anchors, so let the engine decide.
			if !re.MatchString(line) {
				continue
			}
			checked++
			for _, version := range logline.AllVersions() {
				for n := 1; n <= 6; n++ {
					if missing := missingNgram(t, version, n, literals, line); missing != "" {
						t.Fatalf("false negative: version=%s n=%d pattern=%q line=%q literals=%q missing=%q",
							version, n, pattern, line, literals, missing)
					}
				}
			}
		}
	}
	require.Greater(t, checked, 1000)
}

// missingNgram returns the first term a literal needs that the index does not
// emit for line, or "" when all are present.
func missingNgram(t *testing.T, version string, n int, literals []string, line string) string {
	t.Helper()
	extract, err := logline.ExtractorForVersion(version)
	require.NoError(t, err)
	format, err := logline.FormatterForVersion(version)
	require.NoError(t, err)

	indexed := map[string]struct{}{}
	for _, key := range extract(n, line, nil, nil, nil) {
		indexed[format(key, n)] = struct{}{}
	}
	for _, lit := range literals {
		for _, key := range extract(n, lit, nil, nil, nil) {
			if term := format(key, n); !isIndexed(indexed, term) {
				return term
			}
		}
	}
	return ""
}

func isIndexed(indexed map[string]struct{}, term string) bool {
	_, ok := indexed[term]
	return ok
}

func terms(t *testing.T, version string, n int, s string) []string {
	t.Helper()
	extract, err := logline.ExtractorForVersion(version)
	require.NoError(t, err)
	format, err := logline.FormatterForVersion(version)
	require.NoError(t, err)

	var out []string
	for _, key := range extract(n, s, nil, nil, nil) {
		out = append(out, format(key, n))
	}
	return out
}

var (
	atoms    = []string{"a", "b", "k", "s", "K", "S", "x", "1", "2", "9", "_", "-", ".", " ", ":", "é", "\u212A", "\u017F", "\u0130"}
	textPool = []rune{'a', 'k', 's', 'Z', '0', '7', '_', '-', ' ', ',', '"', '\\', '\n', 'é', '\u212A', '\u017F', '\u0130', '\uFFFD'}
)

func randomText(rng *rand.Rand) string {
	var sb strings.Builder
	for i := rng.Intn(4); i > 0; i-- {
		sb.WriteRune(textPool[rng.Intn(len(textPool))])
	}
	return sb.String()
}

func randomPattern(rng *rand.Rand, depth int) string {
	if depth == 0 {
		var sb strings.Builder
		for i := 1 + rng.Intn(8); i > 0; i-- {
			sb.WriteString(regexp.QuoteMeta(atoms[rng.Intn(len(atoms))]))
		}
		return sb.String()
	}
	switch rng.Intn(14) {
	case 0:
		return randomPattern(rng, depth-1) + "|" + randomPattern(rng, depth-1)
	case 1:
		quantifiers := []string{"*", "+", "?", "{2}", "{1,3}", "{0,2}", "{2,}"}
		return "(" + randomPattern(rng, depth-1) + ")" + quantifiers[rng.Intn(len(quantifiers))]
	case 2:
		return "(?i)" + randomPattern(rng, depth-1)
	case 3:
		return "(?i:" + randomPattern(rng, depth-1) + ")" + randomPattern(rng, depth-1)
	case 4:
		classes := []string{".*", ".+", ".", "[a-z]", "[kK]", "[sS]", "[-_]", `\d+`, `\w`, `\s`, "[^a]"}
		return classes[rng.Intn(len(classes))] + randomPattern(rng, depth-1)
	case 5:
		anchors := []string{"^", "$", `\b`, `\B`, "(?m)^", "(?s)"}
		return anchors[rng.Intn(len(anchors))] + randomPattern(rng, depth-1)
	default:
		return randomPattern(rng, depth-1) + randomPattern(rng, depth-1)
	}
}

// generateMatch appends a string re probably matches. Case-folded literals
// pick any rune of the fold orbit, except that an ASCII rune never becomes a
// non-ASCII one such as U+212A or U+017F. Literals accepts missing those.
func generateMatch(rng *rand.Rand, re *syntax.Regexp, sb *strings.Builder) {
	switch re.Op {
	case syntax.OpLiteral:
		for _, r := range re.Rune {
			if re.Flags&syntax.FoldCase != 0 {
				orbit := []rune{r}
				for f := unicode.SimpleFold(r); f != r; f = unicode.SimpleFold(f) {
					if (f < utf8.RuneSelf) == (r < utf8.RuneSelf) {
						orbit = append(orbit, f)
					}
				}
				r = orbit[rng.Intn(len(orbit))]
			}
			sb.WriteRune(r)
		}
	case syntax.OpCharClass:
		if len(re.Rune) == 0 {
			return
		}
		i := 2 * rng.Intn(len(re.Rune)/2)
		lo, hi := re.Rune[i], min(re.Rune[i+1], re.Rune[i]+200)
		sb.WriteRune(lo + rune(rng.Intn(int(hi-lo)+1)))
	case syntax.OpAnyChar, syntax.OpAnyCharNotNL:
		sb.WriteRune(textPool[rng.Intn(len(textPool))])
	case syntax.OpCapture:
		generateMatch(rng, re.Sub[0], sb)
	case syntax.OpConcat:
		for _, sub := range re.Sub {
			generateMatch(rng, sub, sb)
		}
	case syntax.OpAlternate:
		generateMatch(rng, re.Sub[rng.Intn(len(re.Sub))], sb)
	case syntax.OpStar, syntax.OpPlus, syntax.OpQuest, syntax.OpRepeat:
		lo, hi := 0, 3
		switch re.Op {
		case syntax.OpPlus:
			lo = 1
		case syntax.OpQuest:
			hi = 1
		case syntax.OpRepeat:
			lo, hi = re.Min, re.Max
			if hi < 0 {
				hi = lo + 2
			}
		}
		for i := lo + rng.Intn(hi-lo+1); i > 0; i-- {
			generateMatch(rng, re.Sub[0], sb)
		}
	}
}
