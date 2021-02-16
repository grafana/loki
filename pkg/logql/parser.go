package logql

import (
	"errors"
	"sort"
	"strings"
	"sync"
	"text/scanner"

	"github.com/cortexproject/cortex/pkg/util"
	"github.com/prometheus/prometheus/pkg/labels"
	promql_parser "github.com/prometheus/prometheus/promql/parser"
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
func ParseExpr(input string) (expr Expr, err error) {
	defer func() {
		if r := recover(); r != nil {
			var ok bool
			if err, ok = r.(error); ok {
				if errors.Is(err, ErrParse) {
					return
				}
				err = newParseError(err.Error(), 0, 0)
			}
		}
	}()
	p := parserPool.Get().(*parser)
	defer parserPool.Put(p)

	p.Reader.Reset(input)
	p.lexer.Init(p.Reader)
	return p.Parse()
}

// checkEqualityMatchers checks whether a query would touch all the streams in the query range or uses at least one matcher to select specific streams.
func checkEqualityMatchers(matchers []*labels.Matcher) error {
	if len(matchers) == 0 {
		return nil
	}
	_, matchers = util.SplitFiltersAndMatchers(matchers)
	if len(matchers) == 0 {
		return errors.New(errAtleastOneEqualityMatcherRequired)
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
	matcherExpr, ok := expr.(*matchersExpr)
	if !ok {
		return nil, errors.New("only label matchers is supported")
	}
	return matcherExpr.matchers, nil
}

// ParseSampleExpr parses a string and returns the sampleExpr
func ParseSampleExpr(input string, equalityMatcherRequired bool) (SampleExpr, error) {
	expr, err := ParseExpr(input)
	if err != nil {
		return nil, err
	}
	sampleExpr, ok := expr.(SampleExpr)
	if !ok {
		return nil, errors.New("only sample expression supported")
	}

	if equalityMatcherRequired {
		err = sampleExprCheckEqualityMatchers(sampleExpr)
		if err != nil {
			return nil, err
		}
	}
	return sampleExpr, nil
}

func sampleExprCheckEqualityMatchers(expr SampleExpr) error {
	switch e := expr.(type) {
	case *binOpExpr:
		if err := sampleExprCheckEqualityMatchers(e.SampleExpr); err != nil {
			return err
		}

		return sampleExprCheckEqualityMatchers(e.RHS)
	case *literalExpr:
		return nil
	default:
		return checkEqualityMatchers(expr.Selector().Matchers())
	}
}

// ParseLogSelector parses a log selector expression `{app="foo"} |= "filter"`
func ParseLogSelector(input string, equalityMatcherRequired bool) (LogSelectorExpr, error) {
	expr, err := ParseExpr(input)
	if err != nil {
		return nil, err
	}
	logSelector, ok := expr.(LogSelectorExpr)
	if !ok {
		return nil, errors.New("only log selector is supported")
	}

	if equalityMatcherRequired {
		err = checkEqualityMatchers(logSelector.Matchers())
		if err != nil {
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
