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
	// the line, and both can differ from the bytes of the line: the parsers unescape them, and they
	// replace invalid UTF-8 and U+FFFD in a value with a space.
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
	match           []byte
	caseInsensitive bool

	// escapes are the escape sequences that the parser can decode into a byte of match. A line
	// with one of them can hold match without containing it.
	escapes [][]byte
}

func (l lineLiteral) mayHold(line []byte) bool {
	if contains(line, l.match, l.caseInsensitive) {
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
// sides of an `and`. Other filters, including `or`, add nothing, and neither do reserved `__` labels
// or filters that accept the empty value. It returns dst unchanged for parsers whose values are unknown.
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

		// The value comes first because usually fewer lines contain it than the key, so a line without it
		// fails after one search.
		var need allLineNeeds
		if v := valueNeed(f.Filter, values); v != nil {
			need = append(need, v)
		}
		if values == valuesDecoded {
			need = append(need, keyNeeds(f.Name)...)
		}
		if len(need) == 0 {
			return dst
		}
		return append(dst, lineRequirement{name: f.Name, need: need})
	default:
		return dst
	}
}

// keyNeeds returns what the line must contain for json or logfmt to set the label name.
//
// They make the name from one or more keys: they trim the spaces around each key, replace each
// character other than [A-Za-z0-9_] with an underscore, join the keys of nested objects with an
// underscore, and add the DuplicateSuffix when the name is taken. So each run of [A-Za-z0-9] in the
// name without the suffix is in a key. A key that itself ends in the suffix only makes the
// requirement looser, which is safe.
func keyNeeds(name string) []lineNeed {
	name = strings.TrimSuffix(name, DuplicateSuffix)

	var needs []lineNeed
	start := -1
	for i := 0; i <= len(name); i++ {
		if i < len(name) && isAlphanumeric(name[i]) {
			if start < 0 {
				start = i
			}
			continue
		}
		if start >= 0 {
			needs = append(needs, decodedLiteral([]byte(name[start:i]), false))
			start = -1
		}
	}
	return needs
}

func isAlphanumeric(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
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

// literalNeed returns what the line must contain for the parser to set a value that contains match.
func literalNeed(match []byte, caseInsensitive bool, values parserValues) lineNeed {
	if len(match) == 0 {
		return nil
	}
	if values == valuesVerbatim {
		return lineLiteral{match: match, caseInsensitive: caseInsensitive}
	}

	// The line holds an escape instead of a quote, a backslash or a control character, so it
	// cannot contain them as they are.
	for _, c := range match {
		if c == '"' || c == '\\' || c <= 0x1F {
			return nil
		}
	}

	// A space in a value can come from invalid UTF-8 or U+FFFD in the line, so the line only
	// contains the parts between the spaces.
	var need allLineNeeds
	for _, part := range bytes.Split(match, []byte(" ")) {
		if len(part) > 0 {
			need = append(need, decodedLiteral(part, caseInsensitive))
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
func decodedLiteral(match []byte, caseInsensitive bool) lineLiteral {
	escapes := [][]byte{unicodeEscape}
	if bytes.IndexByte(match, '/') >= 0 {
		escapes = append(escapes, slashEscape)
	}
	if bytes.IndexByte(match, '\'') >= 0 {
		escapes = append(escapes, apostropheEscape)
	}
	return lineLiteral{match: match, caseInsensitive: caseInsensitive, escapes: escapes}
}
