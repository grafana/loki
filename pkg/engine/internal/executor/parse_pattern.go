package executor

import (
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"

	"github.com/grafana/loki/v3/pkg/logql/log/pattern"
)

// patternParser parses log lines using a compiled pattern expression with named captures.
type patternParser struct {
	matcher *pattern.Matcher
	names   []string
}

// newPatternParser creates a new pattern parser with the given expression.
// Returns an error if the pattern is invalid or has no named captures.
func newPatternParser(expression string) (*patternParser, error) {
	// TODO(meher): Should we cache this to avoid parsing the same expression when reused across queries?
	matcher, err := pattern.New(expression)
	if err != nil {
		return nil, err
	}

	return &patternParser{
		matcher: matcher,
		names:   matcher.Names(),
	}, nil
}

// process parses a single line and returns the extracted key-value pairs.
func (p *patternParser) process(line string) (map[string]string, error) {
	result := make(map[string]string)

	matches := p.matcher.Matches([]byte(line)) // TODO(meher): Is using unsafe to avoid allocation here worth it?

	if matches == nil {
		return result, nil
	}

	// Map captures to names
	// Note: matches may be shorter than names if the pattern partially matched
	names := p.names[:len(matches)]
	for i, match := range matches {
		result[names[i]] = string(match) // TODO(meher): Same thing about allocation
	}

	return result, nil
}

func buildPatternColumns(input *array.String, expression string) ([]string, []arrow.Array) {
	parser, err := newPatternParser(expression)
	if err != nil {
		return buildErrorColumns(input, "PatternParseErr", err.Error())
	}

	parseFunc := func(line string) (map[string]string, error) {
		return parser.process(line)
	}

	return buildColumns(input, nil, parseFunc, "PatternParseErr")
}
