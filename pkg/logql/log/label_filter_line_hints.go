package log

import (
	"bytes"
	"strings"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
)

// parserValues tells where a parser takes the labels it sets from, which decides what a label
// filter after it requires of the line.
type parserValues int

const (
	// valuesUnknown is for the parsers whose labels the line does not determine in a way the hints
	// model, such as unpack, which replaces the line, and the expression parsers.
	valuesUnknown parserValues = iota

	// valuesDecoded is for json and logfmt. They take the label name from a key and the value from
	// the line, and the value can differ from the bytes of the line: json unescapes values, logfmt
	// unescapes quoted values, and both replace invalid UTF-8 and U+FFFD in a value with a space.
	valuesDecoded

	// valuesVerbatim is for regexp and pattern. They take the label name from the query and the
	// value is a slice of the line.
	valuesVerbatim
)

func parserValuesOf(parser Stage) parserValues {
	switch parser.(type) {
	case *JSONParser, *LogfmtParser:
		return valuesDecoded
	case *RegexpParser, *PatternParser:
		return valuesVerbatim
	default:
		return valuesUnknown
	}
}

// lineRequirement is what a line must contain for a label filter after the parser to pass, unless
// the label the filter reads is already set when the parser runs.
type lineRequirement struct {
	name string
	need lineNeed
}

// labelSet reports whether the label is set before the parser runs. The parser then leaves it, or
// sets the value it reads under the name with the DuplicateSuffix, and the filter reads a value that
// does not come from the line.
func (r lineRequirement) labelSet(lbs *LabelsBuilder) bool {
	return lbs.BaseHas(r.name) || lbs.HasInCategory(r.name, StructuredMetadataLabel) || lbs.HasInCategory(r.name, ParsedLabel)
}

// lineNeed is a condition on the bytes of a line.
type lineNeed interface {
	// mayHold returns false only when the line cannot hold what the label filter needs.
	mayHold(line []byte) bool
}

// lineLiteral needs the line to contain match.
type lineLiteral struct {
	match []byte

	// escapes are the escape sequences that the parser can decode into a byte of match. A line
	// with one of them can hold match without containing it.
	escapes [][]byte
}

func (l lineLiteral) mayHold(line []byte) bool {
	if bytes.Contains(line, l.match) {
		return true
	}
	if len(l.escapes) == 0 || bytes.IndexByte(line, '\\') < 0 {
		return false
	}
	for _, e := range l.escapes {
		if bytes.Contains(line, e) {
			return true
		}
	}
	return false
}

// allLineNeeds needs the line to hold each of them.
type allLineNeeds []lineNeed

func (a allLineNeeds) mayHold(line []byte) bool {
	for _, n := range a {
		if !n.mayHold(line) {
			return false
		}
	}
	return true
}

// anyLineNeeds needs the line to hold one of them.
type anyLineNeeds []lineNeed

func (a anyLineNeeds) mayHold(line []byte) bool {
	for _, n := range a {
		if n.mayHold(line) {
			return true
		}
	}
	return false
}

// appendLineRequirements appends a requirement for each `=` or `=~` filter in f, descending into both
// sides of an `and`. Other filters, including `or`, add nothing, and neither do reserved `__` labels,
// filters that accept the empty value, or a `=~` filter whose regexp does not simplify to literals,
// such as `e[rx]ror`. It returns dst unchanged for parsers whose values are unknown.
func appendLineRequirements(dst []lineRequirement, f LabelFilterer, values parserValues) []lineRequirement {
	if values == valuesUnknown {
		return dst
	}

	switch f := f.(type) {
	case *BinaryLabelFilter:
		if !f.And {
			return dst
		}
		dst = appendLineRequirements(dst, f.Left, values)
		return appendLineRequirements(dst, f.Right, values)
	case *LineFilterLabelFilter:
		if f.Type != labels.MatchEqual && f.Type != labels.MatchRegexp {
			return dst
		}
		// Loki sets the reserved labels, such as __error__, so their values are not in the line.
		if strings.HasPrefix(f.Name, model.ReservedLabelPrefix) {
			return dst
		}
		// A filter that accepts the empty value also passes a line on which the parser sets no label.
		if f.Filter.Filter(nil) {
			return dst
		}

		need := valueNeed(f.Filter, values)
		if need == nil {
			return dst
		}
		return append(dst, lineRequirement{name: f.Name, need: need})
	default:
		return dst
	}
}

// valueNeed returns what the line must contain for the parser to set a value that f matches, or
// nil when any line can hold one. f is the simplified filter of an `=` or `=~` label filter.
func valueNeed(f Filterer, values parserValues) lineNeed {
	switch f := f.(type) {
	case equalFilter:
		return literalNeed(f.match, f.caseInsensitive, values)
	case *containsFilter:
		return literalNeed(f.match, f.caseInsensitive, values)
	case containsAllFilter:
		var need allLineNeeds
		for _, m := range f.matches {
			if n := literalNeed(m.match, m.caseInsensitive, values); n != nil {
				need = append(need, n)
			}
		}
		if len(need) == 0 {
			return nil
		}
		return need
	case andFilter:
		left, right := valueNeed(f.left, values), valueNeed(f.right, values)
		switch {
		case left == nil:
			return right
		case right == nil:
			return left
		}
		return allLineNeeds{left, right}
	case orFilter:
		left, right := valueNeed(f.left, values), valueNeed(f.right, values)
		if left == nil || right == nil {
			return nil
		}
		return anyLineNeeds{left, right}
	default:
		// A regexp that does not simplify to literals can match anything as far as the hints know.
		return nil
	}
}

// minLiteralLen is the length of the shortest literal worth searching a line for. Shorter ones are
// in too many lines to reject many.
const minLiteralLen = 4

// literalNeed returns what the line must contain for the parser to set a value that contains match.
// A case-insensitive match requires nothing, because contains checks it a byte at a time, which
// costs several case-sensitive searches.
func literalNeed(match []byte, caseInsensitive bool, values parserValues) lineNeed {
	if caseInsensitive || len(match) < minLiteralLen {
		return nil
	}
	if values == valuesVerbatim {
		return lineLiteral{match: match}
	}

	// The line can hold an escape such as `\"`, `\\` or `\t` instead of a quote, a backslash or a
	// control character, and decodedLiteral does not allow for those escapes. So the value requires
	// nothing.
	for _, c := range match {
		if c == '"' || c == '\\' || c <= 0x1F {
			return nil
		}
	}

	// A space in a value can come from invalid UTF-8 or U+FFFD in the line, so the line only
	// contains the parts between the spaces.
	var need allLineNeeds
	for _, part := range bytes.Split(match, []byte(" ")) {
		if len(part) >= minLiteralLen {
			need = append(need, decodedLiteral(part))
		}
	}
	switch len(need) {
	case 0:
		return nil
	case 1:
		return need[0]
	}
	return need
}

var (
	unicodeEscape    = []byte(`\u`)
	slashEscape      = []byte(`\/`)
	apostropheEscape = []byte(`\'`)
)

// decodedLiteral needs the line to contain match, or an escape that json or logfmt can decode into
// a byte of it. match has no quote, backslash or control character, which have escapes of their own.
func decodedLiteral(match []byte) lineLiteral {
	escapes := [][]byte{unicodeEscape}
	if bytes.IndexByte(match, '/') >= 0 {
		escapes = append(escapes, slashEscape)
	}
	if bytes.IndexByte(match, '\'') >= 0 {
		escapes = append(escapes, apostropheEscape)
	}
	return lineLiteral{match: match, escapes: escapes}
}
