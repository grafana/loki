package log

import (
	"github.com/grafana/loki/v3/pkg/logqlmodel"
)

// LabelFilterHints lets a parser drop a line early. Right after the parser extracts a label, it
// checks the label against the label filters later in the pipeline.
//
// A parser gets a filter only when the early check gives the same result as the filter at its own
// position:
//   - The filter comes after the parser.
//   - No stage between them can change a label that the filter reads.
//   - The parser cannot change a label that the filter reads later in the same line.
//
// A nil *LabelFilterHints gives no filters to any parser.
type LabelFilterHints struct {
	parsers []parserLabelFilters
}

type parserLabelFilters struct {
	parser  Stage
	filters ParserLabelFilters
}

// ParserLabelFilters are the label filters that one parser can check early. The zero value has no
// filters.
type ParserLabelFilters struct {
	// filters run right after the parser extracts their label. None of them is on a special label.
	// Such a filter reads the builder error, not a parsed label, and the parser can still set the
	// error later in the line.
	filters []labelFilterHint

	// errorFilters are on __error__. A strict logfmt parser runs them after it sets the error at the
	// end of the line.
	errorFilters []labelFilterHint
}

type labelFilterHint struct {
	// name is the label that triggers the check. Store it next to the filter because
	// RequiredLabelNames allocates.
	name   string
	filter LabelFilterer

	// setsError is true when the filter, or one side of it, sets __error__ on a failed conversion.
	setsError bool
}

// NewLabelFilterHints finds, for each parser in stages, the label filters it can check early. The
// stages must be in pipeline order.
func NewLabelFilterHints(stages []Stage) *LabelFilterHints {
	h := &LabelFilterHints{}
	for i, s := range stages {
		if !usesLabelFilterHints(s) {
			continue
		}

		// ForParser finds a parser by identity. A parser at two positions would need two
		// different filter lists, so it gets none.
		if j := h.indexOf(s); j >= 0 {
			h.parsers[j].filters = ParserLabelFilters{}
			continue
		}

		h.parsers = append(h.parsers, parserLabelFilters{
			parser:  s,
			filters: labelFiltersAfter(s, stages[i+1:]),
		})
	}
	return h
}

// ForParser returns the label filters that parser can check early.
func (h *LabelFilterHints) ForParser(parser Stage) ParserLabelFilters {
	if h == nil {
		return ParserLabelFilters{}
	}
	if i := h.indexOf(parser); i >= 0 {
		return h.parsers[i].filters
	}
	return ParserLabelFilters{}
}

func (h *LabelFilterHints) indexOf(parser Stage) int {
	for i, p := range h.parsers {
		// NewLabelFilterHints stores only pointer parsers, so == cannot panic.
		if p.parser == parser {
			return i
		}
	}
	return -1
}

// ShouldContinueParsingLine returns false when the named label, which the parser just extracted,
// fails a label filter later in the pipeline.
func (f ParserLabelFilters) ShouldContinueParsingLine(labelName string, lbs *LabelsBuilder) bool {
	for _, h := range f.filters {
		if h.name == labelName && !h.matches(lbs) {
			return false
		}
	}
	return true
}

// ShouldContinueAfterParseError returns false when the __error__ label, which the parser set at
// the end of the line, fails a label filter later in the pipeline.
func (f ParserLabelFilters) ShouldContinueAfterParseError(lbs *LabelsBuilder) bool {
	for _, h := range f.errorFilters {
		if !h.matches(lbs) {
			return false
		}
	}
	return true
}

func (h labelFilterHint) matches(lbs *LabelsBuilder) bool {
	if !h.setsError {
		_, ok := h.filter.Process(0, nil, lbs)
		return ok
	}

	// Restore the error labels, so that the error appears only at the position of the filter.
	errLabel, errDetails := lbs.GetErr(), lbs.GetErrorDetails()
	_, ok := h.filter.Process(0, nil, lbs)
	lbs.SetErr(errLabel)
	lbs.SetErrorDetails(errDetails)
	return ok
}

// usesLabelFilterHints reports whether the stage checks label filter hints while it parses.
func usesLabelFilterHints(s Stage) bool {
	switch s.(type) {
	case *JSONParser, *LogfmtParser, *RegexpParser, *PatternParser, *UnpackParser:
		return true
	default:
		return false
	}
}

// labelFiltersAfter returns the filters in stages that parser can check early. The stages come
// after the parser in the pipeline.
func labelFiltersAfter(parser Stage, stages []Stage) ParserLabelFilters {
	var filters ParserLabelFilters
	for i, s := range stages {
		f, ok := s.(LabelFilterer)
		if !ok {
			continue
		}

		// The parser checks one label at a time, so a filter on more labels can see a label that
		// the parser did not extract yet.
		names := f.RequiredLabelNames()
		if len(names) != 1 {
			continue
		}
		name := names[0]

		reads, ok := labelsReadBy(f)
		if !ok || !canCheckEarly(parser, stages[:i], name, reads) {
			continue
		}

		h := labelFilterHint{name: name, filter: f, setsError: f.Hints().CanModifyLabels}
		switch {
		case name == logqlmodel.ErrorLabel:
			if p, ok := parser.(*LogfmtParser); ok && p.strict {
				filters.errorFilters = append(filters.errorFilters, h)
			}
		case !isSpecialLabel(name):
			filters.filters = append(filters.filters, h)
		}
	}
	return filters
}

// canCheckEarly reports whether the early check gives the same result as the filter at its own
// position. The filter reads the given labels. The parser runs the check right after it extracts
// the trigger label.
func canCheckEarly(parser Stage, between []Stage, trigger string, reads []string) bool {
	for _, name := range reads {
		// The parser never changes the trigger label after it extracts it. The other labels can
		// still change later in the line.
		if name != trigger && parserCanChangeLater(parser, name) {
			return false
		}
		for _, s := range between {
			if canChangeExtractedLabel(s, name) {
				return false
			}
		}
	}
	return true
}

// labelsReadBy returns the labels whose values decide the result of the filter. It returns false
// for a filter type that it does not know.
func labelsReadBy(f LabelFilterer) ([]string, bool) {
	switch f := f.(type) {
	case *StringLabelFilter, *LineFilterLabelFilter, *NumericLabelFilter, *DurationLabelFilter, *BytesLabelFilter:
		return f.RequiredLabelNames(), true
	case *IPLabelFilter:
		// It keeps every line that has an error, so its result depends on __error__.
		return []string{f.Label, logqlmodel.ErrorLabel}, true
	case *BinaryLabelFilter:
		left, ok := labelsReadBy(f.Left)
		if !ok {
			return nil, false
		}
		right, ok := labelsReadBy(f.Right)
		if !ok {
			return nil, false
		}
		return uniqueString(append(left, right...)), true
	default:
		return nil, false
	}
}

// parserCanChangeLater reports whether the parser can change the named label in the same line,
// after it checked a label filter hint.
func parserCanChangeLater(parser Stage, name string) bool {
	switch p := parser.(type) {
	case *JSONParser:
		// A syntax error after the checked key sets the error labels.
		return isSpecialLabel(name)
	case *LogfmtParser:
		// A strict parse sets the error labels at the end of the line.
		return p.strict && isSpecialLabel(name)
	case *RegexpParser, *PatternParser, *UnpackParser:
		return false
	default:
		return true
	}
}

// canChangeExtractedLabel reports whether the stage can change the named label after a parser
// extracted it. It returns true when it cannot tell.
func canChangeExtractedLabel(s Stage, name string) bool {
	if !s.Hints().CanModifyLabels {
		return false
	}

	switch s := s.(type) {
	case *JSONParser, *LogfmtParser, *RegexpParser, *PatternParser:
		// These parsers skip a key that ParserHint.Extracted reports as extracted. They can only
		// overwrite the error labels.
		//
		// UnpackParser is not in this list: it checks the key before it sanitizes it, so a key
		// such as "a.b" can overwrite the "a_b" label.
		return isSpecialLabel(name)
	case *JSONExpressionParser:
		for _, id := range s.ids {
			if isIdentifierLabel(name, id) {
				return true
			}
		}
		return isSpecialLabel(name)
	case *LogfmtExpressionParser:
		for id := range s.expressions {
			if isIdentifierLabel(name, id) {
				return true
			}
		}
		return isSpecialLabel(name)
	case *LabelsFormatter:
		for _, f := range s.formats {
			if name == f.Name || (f.Rename && name == f.Value) {
				return true
			}
		}
		return isSpecialLabel(name)
	case *LineFormatter, *NumericLabelFilter, *DurationLabelFilter, *BytesLabelFilter, unwrapConversion:
		return isSpecialLabel(name)
	case *DropLabels:
		for _, l := range s.labels {
			if (l.Matcher != nil && l.Matcher.Name == name) || (l.Matcher == nil && l.Name == name) {
				return true
			}
		}
		return false
	case *KeepLabels:
		if len(s.labels) == 0 || isSpecialLabel(name) {
			return false
		}
		// A label kept by name stays. A matcher keeps the label only for some values.
		for _, l := range s.labels {
			if l.Name == name {
				return false
			}
		}
		return true
	case *BinaryLabelFilter:
		return canChangeExtractedLabel(s.Left, name) || canChangeExtractedLabel(s.Right, name)
	default:
		return true
	}
}

// isIdentifierLabel reports whether an expression parser identifier can set the named label. The
// parser adds the duplicate suffix when the identifier clashes with a stream or structured
// metadata label.
func isIdentifierLabel(name, id string) bool {
	return name == id || name == id+duplicateSuffix
}
