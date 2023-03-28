package log

import (
	"strings"

	"github.com/grafana/loki/pkg/logqlmodel"
)

var noParserHints = &Hints{}

// ParserHint are hints given to LogQL parsers.
// This is specially useful for parser that extract implicitly all possible label keys.
// This is used only within metric queries since it's rare that you need all label keys.
// For example in the following expression:
//
//	sum by (status_code) (rate({app="foo"} | json [5m]))
//
// All we need to extract is the status_code in the json parser.
type ParserHint interface {
	// Tells if a label with the given key should be extracted.
	ShouldExtract(key string) bool
	// Tells if there's any hint that start with the given prefix.
	// This allows to speed up key searching in nested structured like json.
	ShouldExtractPrefix(prefix string) bool
	// Tells if we should not extract any labels.
	// For example in :
	//		 sum(rate({app="foo"} | json [5m]))
	// We don't need to extract any labels from the log line.
	NoLabels() bool

	// Holds state about what's already been extracted for the associated
	// labels. This assumes that only required labels are ever extracted
	RecordExtracted(string)
	AllRequiredExtracted() bool
	Reset()
	// PreserveError returns true when parsing errors were specifically requested
	PreserveError() bool
}

type Hints struct {
	noLabels            bool
	requiredLabels      []string
	shouldPreserveError bool
	extracted           []string
}

func (p *Hints) ShouldExtract(key string) bool {
	if len(p.requiredLabels) == 0 {
		return true
	}

	for _, l := range p.extracted {
		if l == key {
			return false
		}
	}

	for _, l := range p.requiredLabels {
		if l == key {
			return true
		}
	}

	return false
}

func (p *Hints) ShouldExtractPrefix(prefix string) bool {
	if len(p.requiredLabels) == 0 {
		return true
	}
	for _, l := range p.requiredLabels {
		if strings.HasPrefix(l, prefix) {
			return true
		}
	}

	return false
}

func (p *Hints) NoLabels() bool {
	return p.noLabels || p.AllRequiredExtracted()
}

func (p *Hints) RecordExtracted(key string) {
	for _, l := range p.requiredLabels {
		if l == key {
			p.extracted = append(p.extracted, key)
			return
		}
	}
}

func (p *Hints) AllRequiredExtracted() bool {
	if len(p.requiredLabels) == 0 {
		return false
	}
	return len(p.extracted) == len(p.requiredLabels)
}

func (p *Hints) Reset() {
	p.extracted = p.extracted[:0]
}

// NewParserHint creates a new parser hint using the list of labels that are seen and required in a query.
func (p *Hints) PreserveError() bool {
	return p.shouldPreserveError
}

// NewParserHint creates a new parser hint using the list of labels that are seen and required in a query.
func NewParserHint(requiredLabelNames, groups []string, without, noLabels bool, metricLabelName string) *Hints {
	hints := make([]string, 0, 2*(len(requiredLabelNames)+len(groups)+1))
	hints = appendLabelHints(hints, requiredLabelNames...)
	hints = appendLabelHints(hints, groups...)
	hints = appendLabelHints(hints, metricLabelName)
	hints = uniqueString(hints)

	extracted := make([]string, 0, len(hints))
	if noLabels {
		if len(hints) > 0 {
			return &Hints{requiredLabels: hints, extracted: extracted, shouldPreserveError: containsError(hints)}
		}
		return &Hints{noLabels: true}
	}
	// we don't know what is required when a without clause is used.
	// Same is true when there's no grouping.
	// no hints available then.
	if without || len(groups) == 0 {
		return noParserHints
	}
	return &Hints{requiredLabels: hints, extracted: extracted, shouldPreserveError: containsError(hints)}
}

func containsError(hints []string) bool {
	for _, s := range hints {
		if s == logqlmodel.ErrorLabel {
			return true
		}
	}
	return false
}

// appendLabelHints Appends the label to the list of hints with and without the duplicate suffix.
// If a parsed label collides with a stream label we add the `_extracted` suffix to it, however hints
// are used by the parsers before we know they will collide with a stream label and hence before the
// _extracted suffix is added. Therefore we must strip the _extracted suffix from any required labels
// that were parsed from somewhere in the query, say in a filter or an aggregation clause.
// Because it's possible for a valid json or logfmt key to already end with _extracted, we'll just
// leave the existing entry ending with _extracted but also add a version with the suffix removed.
func appendLabelHints(dst []string, src ...string) []string {
	for _, l := range src {
		dst = append(dst, l)
		if strings.HasSuffix(l, duplicateSuffix) {
			dst = append(dst, strings.TrimSuffix(l, duplicateSuffix))
		}
	}
	return dst
}
