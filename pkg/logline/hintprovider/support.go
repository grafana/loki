package hintprovider

import (
	"strings"
	"unicode"

	regexpsyntax "github.com/grafana/regexp/syntax"
	"github.com/prometheus/prometheus/model/labels"

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
//   - accepted ops: equality, or regex whose AST is all literals
//     (e.g. "foo", "(?i)foo", "foo(?i)bar")
//   - OR branches are not mandatory and are ignored
//   - unsupported filters are ignored when a mandatory positive literal remains
//   - extracted literals must be at least ngramLength bytes
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
		if _, exists := seen[match]; exists {
			return
		}
		seen[match] = struct{}{}
		literals = append(literals, match)
	}

	for _, filter := range lineFilters {
		match, ok := extractMandatoryPositiveFilterLiteral(filter)
		if ok {
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

func extractMandatoryPositiveFilterLiteral(filter *syntax.LineFilterExpr) (string, bool) {
	if filter == nil {
		return "", false
	}

	// In positive line-filter OR chains, no individual branch is guaranteed.
	// Example: |= "foo" or "bar"
	if filter.Or != nil || filter.IsOrChild {
		return "", false
	}

	// Keep IP and other function filters unsupported.
	if filter.Op != "" {
		return "", false
	}

	switch filter.Ty {
	case logql_log.LineMatchEqual:
		return filter.Match, true
	case logql_log.LineMatchRegexp:
		return extractRegexpLiteral(filter.Match)
	default:
		return "", false
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
		if lit, ok := extractMatcherLiteral(f.Matcher); ok {
			return []string{lit}
		}
		return nil

	case *logql_log.StringLabelFilter:
		// Same LogQL as above; NewStringLabelFilter usually returns
		// LineFilterLabelFilter instead of StringLabelFilter.
		// Handle the rare fallback form.
		if lit, ok := extractMatcherLiteral(f.Matcher); ok {
			return []string{lit}
		}
		return nil

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

func extractMatcherLiteral(m *labels.Matcher) (string, bool) {
	if m == nil {
		return "", false
	}
	switch m.Type {
	case labels.MatchEqual:
		return m.Value, true
	case labels.MatchRegexp:
		return extractRegexpLiteral(m.Value)
	default:
		return "", false
	}
}

// extractRegexpLiteral returns the literal string if every leaf of the regex
// AST is an OpLiteral. This accepts patterns like "foo", "(?i)foo", and
// "foo(?i)bar" while rejecting anything with wildcards, quantifiers,
// alternations, anchors, or character classes. FoldCase leaves have their
// runes lowercased since the parser stores them uppercase.
func extractRegexpLiteral(match string) (string, bool) {
	// Matches Loki's parseRegexpFilter: grafana/regexp/syntax with Perl flags.
	parsed, err := regexpsyntax.Parse(match, regexpsyntax.Perl)
	if err != nil {
		return "", false
	}

	parsed = parsed.Simplify()

	var buf strings.Builder
	if !collectLiteralRunes(parsed, &buf) || buf.Len() == 0 {
		return "", false
	}
	return buf.String(), true
}

// collectLiteralRunes walks the regex AST and appends runes from literal
// leaves. Returns false if any non-literal leaf is encountered.
func collectLiteralRunes(re *regexpsyntax.Regexp, buf *strings.Builder) bool {
	switch re.Op {
	case regexpsyntax.OpLiteral:
		if re.Flags&regexpsyntax.FoldCase != 0 {
			for _, r := range re.Rune {
				buf.WriteRune(unicode.ToLower(r))
			}
		} else {
			for _, r := range re.Rune {
				buf.WriteRune(r)
			}
		}
		return true
	case regexpsyntax.OpConcat, regexpsyntax.OpCapture:
		for _, sub := range re.Sub {
			if !collectLiteralRunes(sub, buf) {
				return false
			}
		}
		return true
	case regexpsyntax.OpEmptyMatch:
		return true
	default:
		return false
	}
}
