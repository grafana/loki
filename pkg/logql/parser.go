package logql

import (
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"text/scanner"

	"github.com/cortexproject/cortex/pkg/util"
	errors2 "github.com/pkg/errors"
	"github.com/prometheus/prometheus/pkg/labels"
	promql_parser "github.com/prometheus/prometheus/promql/parser"

	"github.com/grafana/loki/pkg/loghttp"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logqlmodel"
)

const errAtleastOneEqualityMatcherRequired = "queries require at least one regexp or equality matcher that does not have an empty-compatible value. For instance, app=~\".*\" does not meet this requirement, but app=~\".+\" will"

var parserPool = sync.Pool{
	New: func() interface{} {
		p := &parser{
			p:      &exprParserImpl{},
			Reader: strings.NewReader(""),
			lexer:  &lexer{},
		}
		return p
	},
}

const maxInputSize = 5120

func init() {
	// Improve the error messages coming out of yacc.
	exprErrorVerbose = true
	// uncomment when you need to understand yacc rule tree.
	// exprDebug = 3
	for str, tok := range tokens {
		exprToknames[tok-exprPrivate+1] = str
	}
}

type parser struct {
	p *exprParserImpl
	*lexer
	expr Expr
	*strings.Reader
}

func (p *parser) Parse() (Expr, error) {
	p.lexer.errs = p.lexer.errs[:0]
	p.lexer.Scanner.Error = func(_ *scanner.Scanner, msg string) {
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
	expr, err := parseExprWithoutValidation(input)
	if err != nil {
		return nil, err
	}
	if err := validateExpr(expr); err != nil {
		return nil, err
	}
	return expr, nil
}

func parseExprWithoutValidation(input string) (expr Expr, err error) {
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

func validateExpr(expr Expr) error {
	switch e := expr.(type) {
	case SampleExpr:
		err := validateSampleExpr(e)
		if err != nil {
			return err
		}
	case LogSelectorExpr:
		err := validateMatchers(e.Matchers())
		if err != nil {
			return err
		}
	default:
		return logqlmodel.NewParseError(fmt.Sprintf("unexpected expression type: %v", e), 0, 0)
	}
	return nil
}

// validateMatchers checks whether a query would touch all the streams in the query range or uses at least one matcher to select specific streams.
func validateMatchers(matchers []*labels.Matcher) error {
	if len(matchers) == 0 {
		return nil
	}
	_, matchers = util.SplitFiltersAndMatchers(matchers)
	if len(matchers) == 0 {
		return logqlmodel.NewParseError(errAtleastOneEqualityMatcherRequired, 0, 0)
	}

	return nil
}

// ParseMatchers parses a string and returns labels matchers, if the expression contains
// anything else it will return an error.
func ParseMatchers(input string) ([]*labels.Matcher, error) {
	expr, err := ParseExpr(input)
	if err != nil {
		return nil, err
	}
	matcherExpr, ok := expr.(*MatchersExpr)
	if !ok {
		return nil, errors.New("only label matchers is supported")
	}
	return matcherExpr.matchers, nil
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
		if err := validateSampleExpr(e.SampleExpr); err != nil {
			return err
		}

		return validateSampleExpr(e.RHS)
	case *LiteralExpr:
		return nil
	default:
		return validateMatchers(expr.Selector().Matchers())
	}
}

// ParseLogSelector parses a log selector expression `{app="foo"} |= "filter"`
func ParseLogSelector(input string, validate bool) (LogSelectorExpr, error) {
	expr, err := parseExprWithoutValidation(input)
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
		return nil, err
	}
	sort.Sort(ls)
	return ls, nil
}

// Match extracts and parses multiple matcher groups from a slice of strings
func Match(xs []string) ([][]*labels.Matcher, error) {
	groups := make([][]*labels.Matcher, 0, len(xs))
	for _, x := range xs {
		ms, err := ParseMatchers(x)
		if err != nil {
			return nil, err
		}
		if len(ms) == 0 {
			return nil, errors2.Errorf("0 matchers in group: %s", x)
		}
		groups = append(groups, ms)
	}

	return groups, nil
}

func ParseAndValidateSeriesQuery(r *http.Request) (*logproto.SeriesRequest, error) {
	req, err := loghttp.ParseSeriesQuery(r)
	if err != nil {
		return nil, err
	}
	// ensure matchers are valid before fanning out to ingesters/store as well as returning valuable parsing errors
	// instead of 500s
	_, err = Match(req.Groups)
	if err != nil {
		return nil, err
	}
	return req, nil
}
