package syntax

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/prometheus/prometheus/model/labels"
	promql_parser "github.com/prometheus/prometheus/promql/parser"

	"github.com/grafana/loki/v3/pkg/logqlmodel"
	"github.com/grafana/loki/v3/pkg/util"
)

const (
	EmptyMatchers = "{}"

	errAtleastOneEqualityMatcherRequired = "queries require at least one regexp or equality matcher that does not have an empty-compatible value. For instance, app=~\".*\" does not meet this requirement, but app=~\".+\" will"
)

var parserPool = sync.Pool{
	New: func() interface{} {
		p := &parser{
			p:      &syntaxParserImpl{},
			Reader: strings.NewReader(""),
			lexer:  &lexer{},
		}
		return p
	},
}

// (E.Welch) We originally added this limit from fuzz testing and realizing there should be some maximum limit to an allowed query size.
// The original limit was 5120 based on some internet searching and a best estimate of what a reasonable limit would be.
// We have seen use cases with queries containing a lot of filter expressions or long expanded variable names where this limit was too small.
// Apparently the spec does not specify a limit, and more internet searching suggests almost all browsers will handle 100k+ length urls without issue
// Some limit here still seems prudent however, so the new limit is now 128k.
// Also note this is used to allocate the buffer for reading the query string, so there is some memory cost to making this larger.
const maxInputSize = 131072

func init() {
	// Improve the error messages coming out of yacc.
	syntaxErrorVerbose = true
	// uncomment when you need to understand yacc rule tree.
	// exprDebug = 3
	for str, tok := range tokens {
		syntaxToknames[tok-syntaxPrivate+1] = str
	}
}

type parser struct {
	p *syntaxParserImpl
	*lexer
	expr Expr
	*strings.Reader
}

func (p *parser) Parse() (Expr, error) {
	p.lexer.errs = p.lexer.errs[:0]
	p.lexer.Scanner.Error = func(_ *Scanner, msg string) {
		p.lexer.Error(msg)
	}
	e := p.p.Parse(p)
	if e != 0 || len(p.lexer.errs) > 0 {
		return nil, p.lexer.errs[0]
	}
	return p.expr, nil
}

// ParseExpr parses a string and returns an Expr.
func ParseExpr(input string) (Expr, error) {
	expr, err := ParseExprWithoutValidation(input)
	if err != nil {
		return nil, err
	}
	if err := validateExpr(expr); err != nil {
		return nil, err
	}
	return expr, nil
}

func ParseExprWithoutValidation(input string) (expr Expr, err error) {
	if len(input) >= maxInputSize {
		return nil, logqlmodel.NewParseError(fmt.Sprintf("input size too long (%d > %d)", len(input), maxInputSize), 0, 0)
	}

	defer func() {
		if r := recover(); r != nil {
			var ok bool
			if err, ok = r.(error); ok {
				if errors.Is(err, logqlmodel.ErrParse) {
					return
				}
				err = logqlmodel.NewParseError(err.Error(), 0, 0)
			}
		}
	}()

	p := parserPool.Get().(*parser)
	defer parserPool.Put(p)

	p.Reader.Reset(input)
	p.lexer.Init(p.Reader)
	return p.Parse()
}

func MustParseExpr(input string) Expr {
	expr, err := ParseExpr(input)
	if err != nil {
		panic(err)
	}
	return expr
}

func validateExpr(expr Expr) error {
	switch e := expr.(type) {
	case SampleExpr:
		return validateSampleExpr(e)
	case LogSelectorExpr:
		return validateLogSelectorExpression(e)
	case VariantsExpr:
		return validateVariantsExpr(e)
	default:
		return logqlmodel.NewParseError(fmt.Sprintf("unexpected expression type: %v", e), 0, 0)
	}
}

func validateVariantsExpr(e VariantsExpr) error {
	err := validateLogSelectorExpression(e.LogRange().Left)
	if err != nil {
		return err
	}

	for _, variant := range e.Variants() {
		err = validateSampleExpr(variant)
		if err != nil {
			return err
		}
	}

	return nil
}

// validateMatchers checks whether a query would touch all the streams in the query range or uses at least one matcher to select specific streams.
func validateMatchers(matchers []*labels.Matcher) error {
	_, matchers = util.SplitFiltersAndMatchers(matchers)
	if len(matchers) == 0 {
		return logqlmodel.NewParseError(errAtleastOneEqualityMatcherRequired, 0, 0)
	}
	return nil
}

// ParseMatchers parses a string and returns labels matchers, if the expression contains
// anything else it will return an error.
func ParseMatchers(input string, validate bool) ([]*labels.Matcher, error) {
	var (
		expr Expr
		err  error
	)

	if validate {
		expr, err = ParseExpr(input)
	} else {
		expr, err = ParseExprWithoutValidation(input)
	}

	if err != nil {
		return nil, err
	}
	matcherExpr, ok := expr.(*MatchersExpr)
	if !ok {
		return nil, logqlmodel.ErrParseMatchers
	}
	return matcherExpr.Mts, nil
}

func MatchersString(xs []*labels.Matcher) string {
	return newMatcherExpr(xs).String()
}

// ParseSampleExpr parses a string and returns the sampleExpr
func ParseSampleExpr(input string) (SampleExpr, error) {
	expr, err := ParseExpr(input)
	if err != nil {
		return nil, err
	}
	sampleExpr, ok := expr.(SampleExpr)
	if !ok {
		return nil, errors.New("only sample expression supported")
	}

	return sampleExpr, nil
}

func validateSampleExpr(expr SampleExpr) error {
	switch e := expr.(type) {
	case *BinOpExpr:
		if e.err != nil {
			return e.err
		}
		if err := validateSampleExpr(e.SampleExpr); err != nil {
			return err
		}
		return validateSampleExpr(e.RHS)
	case *LiteralExpr:
		if e.err != nil {
			return e.err
		}
		return nil
	case *VectorExpr:
		if e.err != nil {
			return e.err
		}
		return nil
	case *VectorAggregationExpr:
		if e.err != nil {
			return e.err
		}
		if e.Operation == OpTypeSort || e.Operation == OpTypeSortDesc {
			if err := validateSortGrouping(e.Grouping); err != nil {
				return err
			}
		}
		return validateSampleExpr(e.Left)
	case *LabelReplaceExpr:
		if e.err != nil {
			return e.err
		}
		return validateSampleExpr(e.Left)
	default:
		selector, err := e.Selector()
		if err != nil {
			return err
		}
		return validateLogSelectorExpression(selector)
	}
}

func validateLogSelectorExpression(expr LogSelectorExpr) error {
	switch e := expr.(type) {
	case *VectorExpr:
		return nil
	default:
		return validateMatchers(e.Matchers())
	}
}

// validateSortGrouping prevent by|without groupings on sort operations.
// This will keep compatibility with promql and allowing sort by (foo) doesn't make much sense anyway when sort orders by value instead of labels.
func validateSortGrouping(grouping *Grouping) error {
	if grouping != nil && len(grouping.Groups) > 0 {
		return logqlmodel.NewParseError("sort and sort_desc doesn't allow grouping by ", 0, 0)
	}
	return nil
}

// ParseLogSelector parses a log selector expression `{app="foo"} |= "filter"`
func ParseLogSelector(input string, validate bool) (LogSelectorExpr, error) {
	expr, err := ParseExprWithoutValidation(input)
	if err != nil {
		return nil, err
	}
	logSelector, ok := expr.(LogSelectorExpr)
	if !ok {
		return nil, errors.New("only log selector is supported")
	}
	if validate {
		if err := validateExpr(expr); err != nil {
			return nil, err
		}
	}
	return logSelector, nil
}

// ParseLabels parses labels from a string using logql parser.
func ParseLabels(lbs string) (labels.Labels, error) {
	ls, err := promql_parser.ParseMetric(lbs)
	if err != nil {
		return labels.EmptyLabels(), err
	}

	// Use the label builder to trim empty label values.
	// Empty label values are equivalent to absent labels
	// in Prometheus, but they unfortunately alter the
	// Hash values created. This can cause problems in Loki
	// if we can't rely on a set of labels to have a deterministic
	// hash value.
	// Therefore we must normalize early in the write path.
	// See https://github.com/grafana/loki/pull/7355
	// for more information
	return labels.NewBuilder(ls).Labels(), nil
}

// ParseLabelsWithDots parses labels from a string, supporting dots in label names.
// This is a custom parser that extends Prometheus label parsing to support dots.
func ParseLabelsWithDots(lbs string) (labels.Labels, error) {
	// First try the standard parser
	ls, err := promql_parser.ParseMetric(lbs)
	if err == nil {
		return labels.NewBuilder(ls).Labels(), nil
	}

	// Check if the error is due to dots in label names
	// If it's a parse error about unexpected character '.', try custom parsing
	if strings.Contains(err.Error(), "unexpected character inside braces: '.'") {
		return parseLabelsWithDots(lbs)
	}

	// For other errors, return the original error to maintain compatibility
	return labels.EmptyLabels(), err
}

// parseLabelsWithDots is a custom parser that handles dots in label names
func parseLabelsWithDots(lbs string) (labels.Labels, error) {
	// Remove outer braces
	lbs = strings.TrimSpace(lbs)
	if !strings.HasPrefix(lbs, "{") || !strings.HasSuffix(lbs, "}") {
		return labels.EmptyLabels(), fmt.Errorf("labels must be wrapped in braces")
	}
	lbs = lbs[1 : len(lbs)-1]

	// Split by comma, handling quoted values
	var result []labels.Label
	pairs := splitLabelsWithDots(lbs)

	for _, pair := range pairs {
		key, value, err := parseLabelPair(pair)
		if err != nil {
			return labels.EmptyLabels(), err
		}
		result = append(result, labels.Label{Name: key, Value: value})
	}

	// Convert slice to labels using FromStrings
	args := make([]string, 0, len(result)*2)
	for _, label := range result {
		args = append(args, label.Name, label.Value)
	}
	return labels.FromStrings(args...), nil
}

// splitLabelsWithDots splits a label string by comma, respecting quoted values
func splitLabelsWithDots(lbs string) []string {
	var pairs []string
	var current strings.Builder
	inQuotes := false
	escapeNext := false

	for i := 0; i < len(lbs); i++ {
		char := lbs[i]

		if escapeNext {
			current.WriteByte(char)
			escapeNext = false
			continue
		}

		if char == '\\' {
			escapeNext = true
			current.WriteByte(char)
			continue
		}

		if char == '"' {
			inQuotes = !inQuotes
			current.WriteByte(char)
			continue
		}

		if char == ',' && !inQuotes {
			if current.Len() > 0 {
				pairs = append(pairs, strings.TrimSpace(current.String()))
				current.Reset()
			}
			continue
		}

		current.WriteByte(char)
	}

	if current.Len() > 0 {
		pairs = append(pairs, strings.TrimSpace(current.String()))
	}

	return pairs
}

// parseLabelPair parses a single key=value pair, supporting dots in keys
func parseLabelPair(pair string) (string, string, error) {
	// Find the first unquoted equals sign
	inQuotes := false
	escapeNext := false

	for i := 0; i < len(pair); i++ {
		char := pair[i]

		if escapeNext {
			escapeNext = false
			continue
		}

		if char == '\\' {
			escapeNext = true
			continue
		}

		if char == '"' {
			inQuotes = !inQuotes
			continue
		}

		if char == '=' && !inQuotes {
			key := strings.TrimSpace(pair[:i])
			value := strings.TrimSpace(pair[i+1:])

			// Remove quotes from value if present
			if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
				value = value[1 : len(value)-1]
			}

			return key, value, nil
		}
	}

	return "", "", fmt.Errorf("invalid label pair: %s", pair)
}
