package hintprovider

import (
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/logline/regexliteral"
	logql_log "github.com/grafana/loki/v3/pkg/logql/log"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
)

// SupportedQuery returns literal match strings for query shapes that can use
// logline index hints.
//
// Rules:
//   - at least one mandatory positive filter must exist
//   - sources: line filters (|=, |~) that still run on the ingested line
//     (collectLineFilters stops at line_format / decolorize / unpack),
//     pre-parser label filters (syntax.ExtractLabelFiltersBeforeParser), and
//     post-parser extracted-field filters after a high-confidence extractor
//     (see collectPostParserLabelFilterLiterals)
//   - accepted ops: equality, or regex. A regex contributes the literals every
//     match must contain (regexliteral.Required), e.g. ".*foo.*bar" gives
//     "foo" and "bar"
//   - OR branches are not mandatory and are ignored
//   - unsupported filters are ignored when a mandatory positive literal remains
//   - extracted literals must be at least ngramLength bytes
//   - literals are returned with ASCII letters uppercased (upperASCII), so
//     filters that differ only in case share one needle
//   - needles are matcher values only (never field names); value prefixes are
//     not stripped
//   - skip post-parser values with `"`, `\`, or control bytes. The same
//     filter can match a parsed field, a stream label, or SM, so we cannot
//     assume the raw line used JSON/logfmt escapes.
//
// Indexes are assumed to include structured metadata and stream label values
// (v3). Label-filter value literals are looked up the same way as line filters.
func SupportedQuery(expr syntax.Expr, ngramLength int) []string {
	if expr == nil || ngramLength <= 0 {
		return nil
	}

	lineFilters := collectLineFilters(expr)
	labelFilters := syntax.ExtractLabelFiltersBeforeParser(expr)
	postParser := collectPostParserLabelFilterLiterals(expr)

	n := len(lineFilters) + len(labelFilters) + len(postParser)
	literals := make([]string, 0, n)
	seen := make(map[string]struct{}, n)
	appendUnique := func(match string) {
		if len(match) < ngramLength {
			return
		}
		match = upperASCII(match)
		if _, exists := seen[match]; exists {
			return
		}
		seen[match] = struct{}{}
		literals = append(literals, match)
	}

	for _, filter := range lineFilters {
		for _, match := range extractMandatoryPositiveFilterLiterals(filter) {
			appendUnique(match)
		}
	}

	for _, filter := range labelFilters {
		if filter == nil {
			continue
		}
		for _, match := range collectLabelFilterLiterals(filter.LabelFilterer) {
			appendUnique(match)
		}
	}

	for _, match := range postParser {
		appendUnique(match)
	}

	if len(literals) == 0 {
		return nil
	}
	return literals
}

func extractMandatoryPositiveFilterLiterals(filter *syntax.LineFilterExpr) []string {
	if filter == nil {
		return nil
	}

	// In positive line-filter OR chains, no individual branch is guaranteed.
	// Example: |= "foo" or "bar"
	if filter.Or != nil || filter.IsOrChild {
		return nil
	}

	// Keep IP and other function filters unsupported.
	if filter.Op != "" {
		return nil
	}

	switch filter.Ty {
	case logql_log.LineMatchEqual:
		return []string{filter.Match}
	case logql_log.LineMatchRegexp:
		return regexliteral.Required(filter.Match)
	default:
		return nil
	}
}

// collectLabelFilterLiterals walks a label filterer and returns literals from
// mandatory positive equality/regex leaves. AND combines children; OR
// contributes nothing (no individual branch is guaranteed).
func collectLabelFilterLiterals(filter logql_log.LabelFilterer) []string {
	if filter == nil {
		return nil
	}

	switch f := filter.(type) {
	case *logql_log.BinaryLabelFilter:
		// OR: neither branch is mandatory (e.g. | a="x" or b="y") → ignore.
		if !f.And {
			return nil
		}
		// AND: Left/Right are the two children (e.g. | foo="abcdef", bar="ghijkl"
		// → Left=foo, Right=bar). Recurse and keep eligible literals from each;
		// that example yields ["abcdef", "ghijkl"].
		left := collectLabelFilterLiterals(f.Left)
		right := collectLabelFilterLiterals(f.Right)
		if len(left) == 0 {
			return right
		}
		if len(right) == 0 {
			return left
		}
		out := make([]string, 0, len(left)+len(right))
		out = append(out, left...)
		out = append(out, right...)
		return out

	case *logql_log.LineFilterLabelFilter:
		// Usual leaf after ParseExpr for | foo="bar" / | foo=~"...".
		return extractMatcherLiterals(f.Matcher)

	case *logql_log.StringLabelFilter:
		// Same LogQL as above; NewStringLabelFilter usually returns
		// LineFilterLabelFilter instead of StringLabelFilter.
		// Handle the rare fallback form.
		return extractMatcherLiterals(f.Matcher)

	default:
		// Numeric, duration, bytes, IP, noop, and other filterers are ineligible.
		return nil
	}
}

// collectPostParserLabelFilterLiterals returns values from label filters
// after json, logfmt, regexp, or pattern (e.g. | json | msg="ok").
// ExtractLabelFiltersBeforeParser stops at the first parser; this continues
// past it and keeps a value only if it still appears in the ingested line.
func collectPostParserLabelFilterLiterals(expr syntax.Expr) []string {
	if expr == nil {
		return nil
	}

	var out []string
	visitor := &syntax.DepthFirstTraversal{
		VisitPipelineFn: func(_ syntax.RootVisitor, pipe *syntax.PipelineExpr) {
			out = append(out, literalsFromPostParserWindow(pipe.MultiStages)...)
		},
	}
	expr.Accept(visitor)
	return out
}

// literalsFromPostParserWindow walks one pipeline left to right. If a parser pulls
// a value from the original logline, we collect the label-filter values that come after it.
// If we hit a stage that rewrites the line or invents labels, the filters after it
// are no longer safe n-gram lookups, so we stop.
func literalsFromPostParserWindow(stages syntax.MultiStageExpr) []string {
	windowOpen := false
	closedPermanently := false
	var out []string

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
			for _, lit := range collectLabelFilterLiterals(s.LabelFilterer) {
				if IsVerbatimLineLiteral(lit) {
					out = append(out, lit)
				}
			}

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

func extractMatcherLiterals(m *labels.Matcher) []string {
	if m == nil {
		return nil
	}
	switch m.Type {
	case labels.MatchEqual:
		return []string{m.Value}
	case labels.MatchRegexp:
		// Label regexes are anchored, but the value is looked up as a
		// substring of the indexed text, so required literals still apply.
		return regexliteral.Required(m.Value)
	default:
		return nil
	}
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
