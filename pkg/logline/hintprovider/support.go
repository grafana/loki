package hintprovider

import (
	"strings"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/logline/needle"
	"github.com/grafana/loki/v3/pkg/logline/regexliteral"
	logql_log "github.com/grafana/loki/v3/pkg/logql/log"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
)

// DefaultMaxConjunctions is the most needle sets a query is planned into when
// no other value is configured.
const DefaultMaxConjunctions = 64

// SupportedQuery returns the needles for query shapes that can use logline
// index hints. The result is True when the query has no needle.
//
// Needles are a needle.Expr, an OR of sets of literals. A line can match only
// if it contains every literal of at least one set. Each filter becomes a
// clause and all clauses are ANDed:
//   - line filters (|=, |~) that still run on the ingested line
//     (collectLineFilters stops at line_format / decolorize / unpack). An or
//     chain such as |= "a" or "b" is one clause, an OR of its branches.
//   - pre-parser label filters (syntax.ExtractLabelFiltersBeforeParser). and
//     and , combine children with AND, or combines them with OR.
//   - post-parser extracted-field filters after a high-confidence extractor
//     (see postParserClauses).
//
// A leaf we cannot look up is True, never dropped, and a True branch makes its
// whole OR True. True leaves are ip(), negative filters, pattern filters,
// literals shorter than ngramLength, matchers other than = and =~, numeric,
// duration and bytes label filters, regexes with no required literal, label
// filters on reserved `__*` names such as `__error__` (UsableLabelName, Loki
// sets those so their values are not in the line), and post-parser values
// with `"`, `\`, or control bytes (IsVerbatimLineLiteral, the same filter can
// match a parsed field, a stream label, or SM, so we cannot assume the raw
// line used JSON/logfmt escapes).
//
// A regex contributes regexliteral.Needles, so `(GET|POST) /api` gives
// {GET /api} or {POST /api}. Literals are returned with ASCII letters
// uppercased (upperASCII), so filters that differ only in case share a needle.
// Needles are matcher values only, never field names, and value prefixes are
// not stripped.
//
// The result has at most maxConjunctions sets. A clause that would go over
// collapses to the literals all its sets share. maxConjunctions 1 gives one
// set, the needles every branch of every OR shares. A value below 1 means
// DefaultMaxConjunctions.
//
// Indexes are assumed to include structured metadata and stream label values
// (v3). Label-filter value literals are looked up the same way as line filters.
func SupportedQuery(expr syntax.Expr, ngramLength, maxConjunctions int) needle.Expr {
	if expr == nil || ngramLength <= 0 {
		return needle.True()
	}
	if maxConjunctions <= 0 {
		maxConjunctions = DefaultMaxConjunctions
	}
	p := planner{ngramLength: ngramLength, maxConjunctions: maxConjunctions}

	var clauses []needle.Expr
	for _, stage := range collectLineFilters(expr) {
		clauses = append(clauses, p.lineFilterStage(stage))
	}
	for _, filter := range syntax.ExtractLabelFiltersBeforeParser(expr) {
		if filter != nil {
			clauses = append(clauses, p.labelFilter(filter.LabelFilterer, p.keep))
		}
	}
	clauses = append(clauses, p.postParserClauses(expr)...)

	return needle.And(maxConjunctions, clauses...).Prune()
}

// planner builds the needles of one query.
type planner struct {
	ngramLength     int
	maxConjunctions int
}

// keep maps a literal to the needle that is looked up, or reports false when
// it is too short to look up.
func (p planner) keep(s string) (string, bool) {
	if len(s) < p.ngramLength {
		return "", false
	}
	return upperASCII(s), true
}

// keepVerbatim is keep for values compared after a parser, which must also
// appear as they are in the ingested line.
func (p planner) keepVerbatim(s string) (string, bool) {
	if !IsVerbatimLineLiteral(s) {
		return "", false
	}
	return p.keep(s)
}

// literal is the needle of a filter that requires s.
func (p planner) literal(s string, keep func(string) (string, bool)) needle.Expr {
	if s, ok := keep(s); ok {
		return needle.Lit(s)
	}
	return needle.True()
}

// regex is the needle of a filter that requires a match of pattern.
func (p planner) regex(pattern string, keep func(string) (string, bool)) needle.Expr {
	return regexliteral.Needles(pattern, p.maxConjunctions).Map(keep)
}

// lineFilterStage mirrors LineFilterExpr.Filter. Every node on the Left chain
// is ANDed, and a node with an Or chain is an OR of its own LineFilter and
// the LineFilter of every node on that chain.
//
// A negative or chain such as != "a" or "b" is rewritten by the parser into
// nested negative filters, so it never reaches here as an Or.
func (p planner) lineFilterStage(f *syntax.LineFilterExpr) needle.Expr {
	var clauses []needle.Expr
	for curr := f; curr != nil; curr = curr.Left {
		branches := []needle.Expr{p.lineFilter(curr.LineFilter)}
		for or := curr.Or; or != nil; or = or.Or {
			branches = append(branches, p.lineFilter(or.LineFilter))
		}
		clauses = append(clauses, needle.Or(p.maxConjunctions, branches...))
	}
	return needle.And(p.maxConjunctions, clauses...)
}

// lineFilter is the needle of a single line filter.
func (p planner) lineFilter(f syntax.LineFilter) needle.Expr {
	// ip() and other function filters cannot be looked up.
	if f.Op != "" {
		return needle.True()
	}
	switch f.Ty {
	case logql_log.LineMatchEqual:
		return p.literal(f.Match, p.keep)
	case logql_log.LineMatchRegexp:
		return p.regex(f.Match, p.keep)
	default:
		// Negative and pattern filters.
		return needle.True()
	}
}

// labelFilter is the needle of a label filterer. and combines children with
// AND, or with OR.
func (p planner) labelFilter(filter logql_log.LabelFilterer, keep func(string) (string, bool)) needle.Expr {
	switch f := filter.(type) {
	case *logql_log.BinaryLabelFilter:
		left := p.labelFilter(f.Left, keep)
		right := p.labelFilter(f.Right, keep)
		if f.And {
			return needle.And(p.maxConjunctions, left, right)
		}
		return needle.Or(p.maxConjunctions, left, right)

	case *logql_log.LineFilterLabelFilter:
		// Usual leaf after ParseExpr for | foo="bar" / | foo=~"...".
		return p.matcher(f.Matcher, keep)

	case *logql_log.StringLabelFilter:
		// Same LogQL as above. NewStringLabelFilter usually returns
		// LineFilterLabelFilter instead of StringLabelFilter, this is the
		// rare fallback form.
		return p.matcher(f.Matcher, keep)

	default:
		// Numeric, duration, bytes, IP, noop, and other filterers.
		return needle.True()
	}
}

func (p planner) matcher(m *labels.Matcher, keep func(string) (string, bool)) needle.Expr {
	if m == nil || !UsableLabelName(m.Name) {
		return needle.True()
	}
	switch m.Type {
	case labels.MatchEqual:
		return p.literal(m.Value, keep)
	case labels.MatchRegexp:
		// Label regexes are anchored, but the value is looked up as a
		// substring of the indexed text, so required literals still apply.
		return p.regex(m.Value, keep)
	default:
		return needle.True()
	}
}

// postParserClauses returns the clauses of label filters after json, logfmt,
// regexp, or pattern (e.g. | json | msg="ok").
// ExtractLabelFiltersBeforeParser stops at the first parser. This continues
// past it and keeps a value only if it still appears in the ingested line.
func (p planner) postParserClauses(expr syntax.Expr) []needle.Expr {
	var out []needle.Expr
	visitor := &syntax.DepthFirstTraversal{
		VisitPipelineFn: func(_ syntax.RootVisitor, pipe *syntax.PipelineExpr) {
			out = append(out, p.postParserWindow(pipe.MultiStages)...)
		},
	}
	expr.Accept(visitor)
	return out
}

// postParserWindow walks one pipeline left to right. If a parser pulls a
// value from the original logline, we collect the label filters that come
// after it. If we hit a stage that rewrites the line or invents labels, the
// filters after it are no longer safe n-gram lookups, so we stop.
func (p planner) postParserWindow(stages syntax.MultiStageExpr) []needle.Expr {
	windowOpen := false
	closedPermanently := false
	var out []needle.Expr

	closeWindow := func() {
		closedPermanently = true
		windowOpen = false
	}
	openWindow := func() {
		// line_format/decolorize set closedPermanently=true. A parser after them
		// would extract from the rewritten line, so do not reopen.
		if closedPermanently {
			windowOpen = false
			return
		}
		windowOpen = true
	}

	for _, stage := range stages {
		switch s := stage.(type) {
		// | logfmt | trace_id="..."
		// | json dashboardUID="dashboardUID" | dashboardUID="..."
		// | logfmt trace_id="trace_id" | trace_id="..."
		case *syntax.LogfmtParserExpr, *syntax.JSONExpressionParserExpr, *syntax.LogfmtExpressionParserExpr:
			openWindow()

		case *syntax.LineParserExpr:
			if s == nil {
				closeWindow()
				continue
			}
			switch s.Op {
			// | json | dashboardUID="..."  (also matches stream/SM of that name)
			// | regexp "(?P<id>[a-z0-9]+)" | id="..."
			// | pattern "<_> <id>" | id="..."
			case syntax.OpParserTypeJSON, syntax.OpParserTypeRegexp, syntax.OpParserTypePattern:
				openWindow()
			default:
				// | unpack | json | foo="..." — unpack rewrites the line to
				// _entry, so later json/regexp/pattern no longer read the
				// ingested line. Seal the window; do not reopen.
				closeWindow()
			}

		// line_format / decolorize rewrite the line. Labels already extracted
		// are unchanged, so | json | decolorize | foo="..." still pulls
		// foo. A later parser does not reopen the window: ANSI in the
		// middle of a value is gone after decolorize, so the extracted bytes
		// may not be a contiguous substring of the ingested line.
		case *syntax.LineFmtExpr, *syntax.DecolorizeExpr:
			closedPermanently = true

		// These change labels (invent, keep, or drop). Keep what we already
		// collected; skip filters after this. Same if this is before the parser.
		case *syntax.LabelFmtExpr, *syntax.KeepLabelsExpr, *syntax.DropLabelsExpr:
			closeWindow()

		case *syntax.LabelFilterExpr:
			if !windowOpen || s == nil {
				continue
			}
			out = append(out, p.labelFilter(s.LabelFilterer, p.keepVerbatim))

		case *syntax.LineFilterExpr:
			// |= "..." does not extract or derive labels.

		default:
			closeWindow()
		}
	}

	return out
}

// IsVerbatimLineLiteral reports whether s can be looked up like |= "s" on
// the ingested line. JSON and logfmt unescape quotes, backslashes, and
// control characters, so the parsed value may not be in the line.
//
// Example: | json | msg="hello \"world\"" looks up hello "world", but the
// line contains hello \"world\". Different bytes, so the hint would miss.
//
// We cannot re-escape to fix that. The same filter can also match a stream
// or SM label, where those quotes were never escaped.
func IsVerbatimLineLiteral(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		// ASCII C0 controls are 0x00–0x1F (NUL, tab, \n, \r, ...). Space is 0x20.
		if c <= 0x1F || c == '"' || c == '\\' {
			return false
		}
	}
	return true
}

// UsableLabelName reports whether a label name is non-empty and not reserved.
// Reserved `__*` names (e.g. `__error__`) are set by Loki, not read from the line.
func UsableLabelName(name string) bool {
	return name != "" && !strings.HasPrefix(name, model.ReservedLabelPrefix)
}

// upperASCII uppercases a-z and leaves every other byte as it is. This is the
// case change every index version's extractor applies, so the lookup does not
// change, and filters that differ only in case dedupe to one needle.
//
// strings.ToUpper must not be used. It turns U+0131 and U+017F into ASCII I
// and S, while the extractors treat those runes as separators, so the needle
// would look up terms the matching line never produced. It also rewrites
// invalid UTF-8 bytes as U+FFFD, which changes the needle length.
func upperASCII(s string) string {
	for i := 0; i < len(s); i++ {
		if c := s[i]; c >= 'a' && c <= 'z' {
			b := []byte(s)
			for j := i; j < len(b); j++ {
				if b[j] >= 'a' && b[j] <= 'z' {
					b[j] -= 'a' - 'A'
				}
			}
			return string(b)
		}
	}
	return s
}
