package syntax

import (
	"errors"
	"strings"
	"text/scanner"
)

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

	p := &parser{
		p:      &exprParserImpl{},
		Reader: strings.NewReader(""),
		lexer:  &lexer{},
	}

	p.Reader.Reset(input)
	p.lexer.Init(p.Reader)
	return p.Parse()
}
