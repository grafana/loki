// Package parser is an AWK parser and abstract syntax tree.
//
// Use the ParseProgram function to parse an AWK program, and then
// give the result to one of the interp.Exec* functions to execute it.
//
package parser

import (
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"

	. "github.com/benhoyt/goawk/internal/ast"
	. "github.com/benhoyt/goawk/lexer"
)

// ParseError (actually *ParseError) is the type of error returned by
// ParseProgram.
type ParseError struct {
	// Source line/column position where the error occurred.
	Position Position
	// Error message.
	Message string
}

// Error returns a formatted version of the error, including the line
// and column numbers.
func (e *ParseError) Error() string {
	return fmt.Sprintf("parse error at %d:%d: %s", e.Position.Line, e.Position.Column, e.Message)
}

// ParseConfig lets you specify configuration for the parsing process
// (for example printing type information for debugging).
type ParserConfig struct {
	// Enable printing of type information
	DebugTypes bool

	// io.Writer to print type information on (for example, os.Stderr)
	DebugWriter io.Writer

	// Map of named Go functions to allow calling from AWK. See docs
	// on interp.Config.Funcs for details.
	Funcs map[string]interface{}
}

// ParseProgram parses an entire AWK program, returning the *Program
// abstract syntax tree or a *ParseError on error. "config" describes
// the parser configuration (and is allowed to be nil).
func ParseProgram(src []byte, config *ParserConfig) (prog *Program, err error) {
	defer func() {
		// The parser uses panic with a *ParseError to signal parsing
		// errors internally, and they're caught here. This
		// significantly simplifies the recursive descent calls as
		// we don't have to check errors everywhere.
		if r := recover(); r != nil {
			// Convert to ParseError or re-panic
			err = r.(*ParseError)
		}
	}()
	lexer := NewLexer(src)
	p := parser{lexer: lexer}
	if config != nil {
		p.debugTypes = config.DebugTypes
		p.debugWriter = config.DebugWriter
		p.nativeFuncs = config.Funcs
	}
	p.initResolve()
	p.next() // initialize p.tok
	return p.program(), nil
}

// Program is the abstract syntax tree for an entire AWK program.
type Program struct {
	// These fields aren't intended to be used or modified directly,
	// but are exported for the interpreter (Program itself needs to
	// be exported in package "parser", otherwise these could live in
	// "internal/ast".)
	Begin     []Stmts
	Actions   []Action
	End       []Stmts
	Functions []Function
	Scalars   map[string]int
	Arrays    map[string]int
}

// String returns an indented, pretty-printed version of the parsed
// program.
func (p *Program) String() string {
	parts := []string{}
	for _, ss := range p.Begin {
		parts = append(parts, "BEGIN {\n"+ss.String()+"}")
	}
	for _, a := range p.Actions {
		parts = append(parts, a.String())
	}
	for _, ss := range p.End {
		parts = append(parts, "END {\n"+ss.String()+"}")
	}
	for _, function := range p.Functions {
		parts = append(parts, function.String())
	}
	return strings.Join(parts, "\n\n")
}

// Parser state
type parser struct {
	// Lexer instance and current token values
	lexer *Lexer
	pos   Position // position of last token (tok)
	tok   Token    // last lexed token
	val   string   // string value of last token (or "")

	// Parsing state
	inAction  bool   // true if parsing an action (false in BEGIN or END)
	funcName  string // function name if parsing a func, else ""
	loopDepth int    // current loop depth (0 if not in any loops)

	// Variable tracking and resolving
	locals     map[string]bool                // current function's locals (for determining scope)
	varTypes   map[string]map[string]typeInfo // map of func name to var name to type
	varRefs    []varRef                       // all variable references (usually scalars)
	arrayRefs  []arrayRef                     // all array references
	multiExprs map[*MultiExpr]Position        // tracks comma-separated expressions

	// Function tracking
	functions   map[string]int // map of function name to index
	userCalls   []userCall     // record calls so we can resolve them later
	nativeFuncs map[string]interface{}

	// Configuration and debugging
	debugTypes  bool      // show variable types for debugging
	debugWriter io.Writer // where the debug output goes
}

// Parse an entire AWK program.
func (p *parser) program() *Program {
	prog := &Program{}
	p.optionalNewlines()
	for p.tok != EOF {
		switch p.tok {
		case BEGIN:
			p.next()
			prog.Begin = append(prog.Begin, p.stmtsBrace())
		case END:
			p.next()
			prog.End = append(prog.End, p.stmtsBrace())
		case FUNCTION:
			function := p.function()
			p.addFunction(function.Name, len(prog.Functions))
			prog.Functions = append(prog.Functions, function)
		default:
			p.inAction = true
			// Allow empty pattern, normal pattern, or range pattern
			pattern := []Expr{}
			if !p.matches(LBRACE, EOF) {
				pattern = append(pattern, p.expr())
			}
			if !p.matches(LBRACE, EOF, NEWLINE) {
				p.commaNewlines()
				pattern = append(pattern, p.expr())
			}
			// Or an empty action (equivalent to { print $0 })
			action := Action{pattern, nil}
			if p.tok == LBRACE {
				action.Stmts = p.stmtsBrace()
			}
			prog.Actions = append(prog.Actions, action)
			p.inAction = false
		}
		p.optionalNewlines()
	}

	p.resolveUserCalls(prog)
	p.resolveVars(prog)
	p.checkMultiExprs()

	return prog
}

// Parse a list of statements.
func (p *parser) stmts() Stmts {
	switch p.tok {
	case SEMICOLON:
		// This is so things like this parse correctly:
		// BEGIN { for (i=0; i<10; i++); print "x" }
		p.next()
		return nil
	case LBRACE:
		return p.stmtsBrace()
	default:
		return []Stmt{p.stmt()}
	}
}

// Parse a list of statements surrounded in {...} braces.
func (p *parser) stmtsBrace() Stmts {
	p.expect(LBRACE)
	p.optionalNewlines()
	ss := []Stmt{}
	for p.tok != RBRACE && p.tok != EOF {
		ss = append(ss, p.stmt())
	}
	p.expect(RBRACE)
	if p.tok == SEMICOLON {
		p.next()
	}
	return ss
}

// Parse a "simple" statement (eg: allowed in a for loop init clause).
func (p *parser) simpleStmt() Stmt {
	switch p.tok {
	case PRINT, PRINTF:
		op := p.tok
		p.next()
		args := p.exprList(p.printExpr)
		if len(args) == 1 {
			// This allows parens around all the print args
			if m, ok := args[0].(*MultiExpr); ok {
				args = m.Exprs
				p.useMultiExpr(m)
			}
		}
		redirect := ILLEGAL
		var dest Expr
		if p.matches(GREATER, APPEND, PIPE) {
			redirect = p.tok
			p.next()
			dest = p.expr()
		}
		if op == PRINT {
			return &PrintStmt{args, redirect, dest}
		} else {
			if len(args) == 0 {
				panic(p.error("expected printf args, got none"))
			}
			return &PrintfStmt{args, redirect, dest}
		}
	case DELETE:
		p.next()
		ref := p.arrayRef(p.val, p.pos)
		p.expect(NAME)
		var index []Expr
		if p.tok == LBRACKET {
			p.next()
			index = p.exprList(p.expr)
			if len(index) == 0 {
				panic(p.error("expected expression instead of ]"))
			}
			p.expect(RBRACKET)
		}
		return &DeleteStmt{ref, index}
	case IF, FOR, WHILE, DO, BREAK, CONTINUE, NEXT, EXIT, RETURN:
		panic(p.error("expected print/printf, delete, or expression"))
	default:
		return &ExprStmt{p.expr()}
	}
}

// Parse any top-level statement.
func (p *parser) stmt() Stmt {
	for p.matches(SEMICOLON, NEWLINE) {
		p.next()
	}
	var s Stmt
	switch p.tok {
	case IF:
		p.next()
		p.expect(LPAREN)
		cond := p.expr()
		p.expect(RPAREN)
		p.optionalNewlines()
		body := p.stmts()
		p.optionalNewlines()
		var elseBody Stmts
		if p.tok == ELSE {
			p.next()
			p.optionalNewlines()
			elseBody = p.stmts()
		}
		s = &IfStmt{cond, body, elseBody}
	case FOR:
		// Parse for statement, either "for in" or C-like for loop.
		//
		//     FOR LPAREN NAME IN NAME RPAREN NEWLINE* stmts |
		//     FOR LPAREN [simpleStmt] SEMICOLON NEWLINE*
		//                [expr] SEMICOLON NEWLINE*
		//                [simpleStmt] RPAREN NEWLINE* stmts
		//
		p.next()
		p.expect(LPAREN)
		var pre Stmt
		if p.tok != SEMICOLON {
			pre = p.simpleStmt()
		}
		if pre != nil && p.tok == RPAREN {
			// Match: for (var in array) body
			p.next()
			p.optionalNewlines()
			exprStmt, ok := pre.(*ExprStmt)
			if !ok {
				panic(p.error("expected 'for (var in array) ...'"))
			}
			inExpr, ok := (exprStmt.Expr).(*InExpr)
			if !ok {
				panic(p.error("expected 'for (var in array) ...'"))
			}
			if len(inExpr.Index) != 1 {
				panic(p.error("expected 'for (var in array) ...'"))
			}
			varExpr, ok := (inExpr.Index[0]).(*VarExpr)
			if !ok {
				panic(p.error("expected 'for (var in array) ...'"))
			}
			body := p.loopStmts()
			s = &ForInStmt{varExpr, inExpr.Array, body}
		} else {
			// Match: for ([pre]; [cond]; [post]) body
			p.expect(SEMICOLON)
			p.optionalNewlines()
			var cond Expr
			if p.tok != SEMICOLON {
				cond = p.expr()
			}
			p.expect(SEMICOLON)
			p.optionalNewlines()
			var post Stmt
			if p.tok != RPAREN {
				post = p.simpleStmt()
			}
			p.expect(RPAREN)
			p.optionalNewlines()
			body := p.loopStmts()
			s = &ForStmt{pre, cond, post, body}
		}
	case WHILE:
		p.next()
		p.expect(LPAREN)
		cond := p.expr()
		p.expect(RPAREN)
		p.optionalNewlines()
		body := p.loopStmts()
		s = &WhileStmt{cond, body}
	case DO:
		p.next()
		p.optionalNewlines()
		body := p.loopStmts()
		p.expect(WHILE)
		p.expect(LPAREN)
		cond := p.expr()
		p.expect(RPAREN)
		s = &DoWhileStmt{body, cond}
	case BREAK:
		if p.loopDepth == 0 {
			panic(p.error("break must be inside a loop body"))
		}
		p.next()
		s = &BreakStmt{}
	case CONTINUE:
		if p.loopDepth == 0 {
			panic(p.error("continue must be inside a loop body"))
		}
		p.next()
		s = &ContinueStmt{}
	case NEXT:
		if !p.inAction {
			panic(p.error("next can't be in BEGIN or END"))
		}
		p.next()
		s = &NextStmt{}
	case EXIT:
		p.next()
		var status Expr
		if !p.matches(NEWLINE, SEMICOLON, RBRACE) {
			status = p.expr()
		}
		s = &ExitStmt{status}
	case RETURN:
		if p.funcName == "" {
			panic(p.error("return must be inside a function"))
		}
		p.next()
		var value Expr
		if !p.matches(NEWLINE, SEMICOLON, RBRACE) {
			value = p.expr()
		}
		s = &ReturnStmt{value}
	case LBRACE:
		body := p.stmtsBrace()
		s = &BlockStmt{body}
	default:
		s = p.simpleStmt()
	}
	for p.matches(NEWLINE, SEMICOLON) {
		p.next()
	}
	return s
}

// Same as stmts(), but tracks that we're in a loop (as break and
// continue can only occur inside a loop).
func (p *parser) loopStmts() Stmts {
	p.loopDepth++
	ss := p.stmts()
	p.loopDepth--
	return ss
}

// Parse a function definition and body. As it goes, this resolves
// the local variable indexes and tracks which parameters are array
// parameters.
func (p *parser) function() Function {
	if p.funcName != "" {
		// Should never actually get here (FUNCTION token is only
		// handled at the top level), but just in case.
		panic(p.error("can't nest functions"))
	}
	p.next()
	name := p.val
	if _, ok := p.functions[name]; ok {
		panic(p.error("function %q already defined", name))
	}
	p.expect(NAME)
	p.expect(LPAREN)
	first := true
	params := make([]string, 0, 7) // pre-allocate some to reduce allocations
	p.locals = make(map[string]bool, 7)
	for p.tok != RPAREN {
		if !first {
			p.commaNewlines()
		}
		first = false
		param := p.val
		if param == name {
			panic(p.error("can't use function name as parameter name"))
		}
		if p.locals[param] {
			panic(p.error("duplicate parameter name %q", param))
		}
		p.expect(NAME)
		params = append(params, param)
		p.locals[param] = true
	}
	p.expect(RPAREN)
	p.optionalNewlines()

	// Parse the body
	p.startFunction(name, params)
	body := p.stmtsBrace()
	p.stopFunction()
	p.locals = nil

	return Function{name, params, nil, body}
}

// Parse expressions separated by commas: args to print[f] or user
// function call, or multi-dimensional index.
func (p *parser) exprList(parse func() Expr) []Expr {
	exprs := []Expr{}
	first := true
	for !p.matches(NEWLINE, SEMICOLON, RBRACE, RBRACKET, RPAREN, GREATER, PIPE, APPEND) {
		if !first {
			p.commaNewlines()
		}
		first = false
		exprs = append(exprs, parse())
	}
	return exprs
}

// Here's where things get slightly interesting: only certain
// expression types are allowed in print/printf statements,
// presumably so `print a, b > "file"` is a file redirect instead of
// a greater-than comparison. So we kind of have two ways to recurse
// down here: expr(), which parses all expressions, and printExpr(),
// which skips PIPE GETLINE and GREATER expressions.

// Parse a single expression.
func (p *parser) expr() Expr      { return p.getLine() }
func (p *parser) printExpr() Expr { return p._assign(p.printCond) }

// Parse an "expr | getline [var]" expression:
//
//     assign [PIPE GETLINE [NAME]]
//
func (p *parser) getLine() Expr {
	expr := p._assign(p.cond)
	if p.tok == PIPE {
		p.next()
		p.expect(GETLINE)
		var varExpr *VarExpr
		if p.tok == NAME {
			varExpr = p.varRef(p.val, p.pos)
			p.next()
		}
		return &GetlineExpr{expr, varExpr, nil}
	}
	return expr
}

// Parse an = assignment expression:
//
//     lvalue [assign_op assign]
//
// An lvalue is a variable name, an array[expr] index expression, or
// an $expr field expression.
//
func (p *parser) _assign(higher func() Expr) Expr {
	expr := higher()
	if IsLValue(expr) && p.matches(ASSIGN, ADD_ASSIGN, DIV_ASSIGN,
		MOD_ASSIGN, MUL_ASSIGN, POW_ASSIGN, SUB_ASSIGN) {
		op := p.tok
		p.next()
		right := p._assign(higher)
		switch op {
		case ASSIGN:
			return &AssignExpr{expr, right}
		case ADD_ASSIGN:
			op = ADD
		case DIV_ASSIGN:
			op = DIV
		case MOD_ASSIGN:
			op = MOD
		case MUL_ASSIGN:
			op = MUL
		case POW_ASSIGN:
			op = POW
		case SUB_ASSIGN:
			op = SUB
		}
		return &AugAssignExpr{expr, op, right}
	}
	return expr
}

// Parse a ?: conditional expression:
//
//     or [QUESTION NEWLINE* cond COLON NEWLINE* cond]
//
func (p *parser) cond() Expr      { return p._cond(p.or) }
func (p *parser) printCond() Expr { return p._cond(p.printOr) }

func (p *parser) _cond(higher func() Expr) Expr {
	expr := higher()
	if p.tok == QUESTION {
		p.next()
		p.optionalNewlines()
		t := p.expr()
		p.expect(COLON)
		p.optionalNewlines()
		f := p.expr()
		return &CondExpr{expr, t, f}
	}
	return expr
}

// Parse an || or expresion:
//
//     and [OR NEWLINE* and] [OR NEWLINE* and] ...
//
func (p *parser) or() Expr      { return p.binaryLeft(p.and, true, OR) }
func (p *parser) printOr() Expr { return p.binaryLeft(p.printAnd, true, OR) }

// Parse an && and expresion:
//
//     in [AND NEWLINE* in] [AND NEWLINE* in] ...
//
func (p *parser) and() Expr      { return p.binaryLeft(p.in, true, AND) }
func (p *parser) printAnd() Expr { return p.binaryLeft(p.printIn, true, AND) }

// Parse an "in" expression:
//
//     match [IN NAME] [IN NAME] ...
//
func (p *parser) in() Expr      { return p._in(p.match) }
func (p *parser) printIn() Expr { return p._in(p.printMatch) }

func (p *parser) _in(higher func() Expr) Expr {
	expr := higher()
	for p.tok == IN {
		p.next()
		ref := p.arrayRef(p.val, p.pos)
		p.expect(NAME)
		expr = &InExpr{[]Expr{expr}, ref}
	}
	return expr
}

// Parse a ~ match expression:
//
//     compare [MATCH|NOT_MATCH compare]
//
func (p *parser) match() Expr      { return p._match(p.compare) }
func (p *parser) printMatch() Expr { return p._match(p.printCompare) }

func (p *parser) _match(higher func() Expr) Expr {
	expr := higher()
	if p.matches(MATCH, NOT_MATCH) {
		op := p.tok
		p.next()
		right := p.regexStr(higher) // Not match() as these aren't associative
		return &BinaryExpr{expr, op, right}
	}
	return expr
}

// Parse a comparison expression:
//
//     concat [EQUALS|NOT_EQUALS|LESS|LTE|GREATER|GTE concat]
//
func (p *parser) compare() Expr      { return p._compare(EQUALS, NOT_EQUALS, LESS, LTE, GTE, GREATER) }
func (p *parser) printCompare() Expr { return p._compare(EQUALS, NOT_EQUALS, LESS, LTE, GTE) }

func (p *parser) _compare(ops ...Token) Expr {
	expr := p.concat()
	if p.matches(ops...) {
		op := p.tok
		p.next()
		right := p.concat() // Not compare() as these aren't associative
		return &BinaryExpr{expr, op, right}
	}
	return expr
}

func (p *parser) concat() Expr {
	expr := p.add()
	for p.matches(DOLLAR, NOT, NAME, NUMBER, STRING, LPAREN) ||
		(p.tok >= FIRST_FUNC && p.tok <= LAST_FUNC) {
		right := p.add()
		expr = &BinaryExpr{expr, CONCAT, right}
	}
	return expr
}

func (p *parser) add() Expr {
	return p.binaryLeft(p.mul, false, ADD, SUB)
}

func (p *parser) mul() Expr {
	return p.binaryLeft(p.pow, false, MUL, DIV, MOD)
}

func (p *parser) pow() Expr {
	// Note that pow (expr ^ expr) is right-associative
	expr := p.preIncr()
	if p.tok == POW {
		p.next()
		right := p.pow()
		return &BinaryExpr{expr, POW, right}
	}
	return expr
}

func (p *parser) preIncr() Expr {
	if p.tok == INCR || p.tok == DECR {
		op := p.tok
		p.next()
		expr := p.preIncr()
		if !IsLValue(expr) {
			panic(p.error("expected lvalue after ++ or --"))
		}
		return &IncrExpr{expr, op, true}
	}
	return p.postIncr()
}

func (p *parser) postIncr() Expr {
	expr := p.primary()
	if p.tok == INCR || p.tok == DECR {
		if !IsLValue(expr) {
			panic(p.error("expected lvalue before ++ or --"))
		}
		op := p.tok
		p.next()
		return &IncrExpr{expr, op, false}
	}
	return expr
}

func (p *parser) primary() Expr {
	switch p.tok {
	case NUMBER:
		// AWK allows forms like "1.5e", but ParseFloat doesn't
		s := strings.TrimRight(p.val, "eE")
		n, _ := strconv.ParseFloat(s, 64)
		p.next()
		return &NumExpr{n}
	case STRING:
		s := p.val
		p.next()
		return &StrExpr{s}
	case DIV, DIV_ASSIGN:
		// If we get to DIV or DIV_ASSIGN as a primary expression,
		// it's actually a regex.
		regex := p.nextRegex()
		return &RegExpr{regex}
	case DOLLAR:
		p.next()
		return &FieldExpr{p.primary()}
	case NOT, ADD, SUB:
		op := p.tok
		p.next()
		return &UnaryExpr{op, p.pow()}
	case NAME:
		name := p.val
		namePos := p.pos
		p.next()
		if p.tok == LBRACKET {
			// a[x] or a[x, y] array index expression
			p.next()
			index := p.exprList(p.expr)
			if len(index) == 0 {
				panic(p.error("expected expression instead of ]"))
			}
			p.expect(RBRACKET)
			return &IndexExpr{p.arrayRef(name, namePos), index}
		} else if p.tok == LPAREN && !p.lexer.HadSpace() {
			if p.locals[name] {
				panic(p.error("can't call local variable %q as function", name))
			}
			// Grammar requires no space between function name and
			// left paren for user function calls, hence the funky
			// lexer.HadSpace() method.
			return p.userCall(name, namePos)
		}
		return p.varRef(name, namePos)
	case LPAREN:
		parenPos := p.pos
		p.next()
		exprs := p.exprList(p.expr)
		switch len(exprs) {
		case 0:
			panic(p.error("expected expression, not %s", p.tok))
		case 1:
			p.expect(RPAREN)
			return exprs[0]
		default:
			// Multi-dimensional array "in" requires parens around index
			p.expect(RPAREN)
			if p.tok == IN {
				p.next()
				ref := p.arrayRef(p.val, p.pos)
				p.expect(NAME)
				return &InExpr{exprs, ref}
			}
			// MultiExpr is used as a pseudo-expression for print[f] parsing.
			return p.multiExpr(exprs, parenPos)
		}
	case GETLINE:
		p.next()
		var varExpr *VarExpr
		if p.tok == NAME {
			varExpr = p.varRef(p.val, p.pos)
			p.next()
		}
		var file Expr
		if p.tok == LESS {
			p.next()
			file = p.expr()
		}
		return &GetlineExpr{nil, varExpr, file}
	// Below is the parsing of all the builtin function calls. We
	// could unify these but several of them have special handling
	// (array/lvalue/regex params, optional arguments, etc), and
	// doing it this way means we can check more at parse time.
	case F_SUB, F_GSUB:
		op := p.tok
		p.next()
		p.expect(LPAREN)
		regex := p.regexStr(p.expr)
		p.commaNewlines()
		repl := p.expr()
		args := []Expr{regex, repl}
		if p.tok == COMMA {
			p.commaNewlines()
			in := p.expr()
			if !IsLValue(in) {
				panic(p.error("3rd arg to sub/gsub must be lvalue"))
			}
			args = append(args, in)
		}
		p.expect(RPAREN)
		return &CallExpr{op, args}
	case F_SPLIT:
		p.next()
		p.expect(LPAREN)
		str := p.expr()
		p.commaNewlines()
		ref := p.arrayRef(p.val, p.pos)
		p.expect(NAME)
		args := []Expr{str, ref}
		if p.tok == COMMA {
			p.commaNewlines()
			args = append(args, p.regexStr(p.expr))
		}
		p.expect(RPAREN)
		return &CallExpr{F_SPLIT, args}
	case F_MATCH:
		p.next()
		p.expect(LPAREN)
		str := p.expr()
		p.commaNewlines()
		regex := p.regexStr(p.expr)
		p.expect(RPAREN)
		return &CallExpr{F_MATCH, []Expr{str, regex}}
	case F_RAND:
		p.next()
		p.expect(LPAREN)
		p.expect(RPAREN)
		return &CallExpr{F_RAND, nil}
	case F_SRAND:
		p.next()
		p.expect(LPAREN)
		var args []Expr
		if p.tok != RPAREN {
			args = append(args, p.expr())
		}
		p.expect(RPAREN)
		return &CallExpr{F_SRAND, args}
	case F_LENGTH:
		p.next()
		var args []Expr
		// AWK quirk: "length" is allowed to be called without parens
		if p.tok == LPAREN {
			p.next()
			if p.tok != RPAREN {
				args = append(args, p.expr())
			}
			p.expect(RPAREN)
		}
		return &CallExpr{F_LENGTH, args}
	case F_SUBSTR:
		p.next()
		p.expect(LPAREN)
		str := p.expr()
		p.commaNewlines()
		start := p.expr()
		args := []Expr{str, start}
		if p.tok == COMMA {
			p.commaNewlines()
			args = append(args, p.expr())
		}
		p.expect(RPAREN)
		return &CallExpr{F_SUBSTR, args}
	case F_SPRINTF:
		p.next()
		p.expect(LPAREN)
		args := []Expr{p.expr()}
		for p.tok == COMMA {
			p.commaNewlines()
			args = append(args, p.expr())
		}
		p.expect(RPAREN)
		return &CallExpr{F_SPRINTF, args}
	case F_FFLUSH:
		p.next()
		p.expect(LPAREN)
		var args []Expr
		if p.tok != RPAREN {
			args = append(args, p.expr())
		}
		p.expect(RPAREN)
		return &CallExpr{F_FFLUSH, args}
	case F_COS, F_SIN, F_EXP, F_LOG, F_SQRT, F_INT, F_TOLOWER, F_TOUPPER, F_SYSTEM, F_CLOSE:
		// Simple 1-argument functions
		op := p.tok
		p.next()
		p.expect(LPAREN)
		arg := p.expr()
		p.expect(RPAREN)
		return &CallExpr{op, []Expr{arg}}
	case F_ATAN2, F_INDEX:
		// Simple 2-argument functions
		op := p.tok
		p.next()
		p.expect(LPAREN)
		arg1 := p.expr()
		p.commaNewlines()
		arg2 := p.expr()
		p.expect(RPAREN)
		return &CallExpr{op, []Expr{arg1, arg2}}
	default:
		panic(p.error("expected expression instead of %s", p.tok))
	}
}

// Parse /.../ regex or generic expression:
//
//     REGEX | expr
//
func (p *parser) regexStr(parse func() Expr) Expr {
	if p.matches(DIV, DIV_ASSIGN) {
		regex := p.nextRegex()
		return &StrExpr{regex}
	}
	return parse()
}

// Parse left-associative binary operator. Allow newlines after
// operator if allowNewline is true.
//
//     parse [op parse] [op parse] ...
//
func (p *parser) binaryLeft(higher func() Expr, allowNewline bool, ops ...Token) Expr {
	expr := higher()
	for p.matches(ops...) {
		op := p.tok
		p.next()
		if allowNewline {
			p.optionalNewlines()
		}
		right := higher()
		expr = &BinaryExpr{expr, op, right}
	}
	return expr
}

// Parse comma followed by optional newlines:
//
//     COMMA NEWLINE*
//
func (p *parser) commaNewlines() {
	p.expect(COMMA)
	p.optionalNewlines()
}

// Parse zero or more optional newlines:
//
//    [NEWLINE] [NEWLINE] ...
//
func (p *parser) optionalNewlines() {
	for p.tok == NEWLINE {
		p.next()
	}
}

// Parse next token into p.tok (and set p.pos and p.val).
func (p *parser) next() {
	p.pos, p.tok, p.val = p.lexer.Scan()
	if p.tok == ILLEGAL {
		panic(p.error("%s", p.val))
	}
}

// Parse next regex and return it (must only be called after DIV or
// DIV_ASSIGN token).
func (p *parser) nextRegex() string {
	p.pos, p.tok, p.val = p.lexer.ScanRegex()
	if p.tok == ILLEGAL {
		panic(p.error("%s", p.val))
	}
	regex := p.val
	_, err := regexp.Compile(regex)
	if err != nil {
		panic(p.error("%v", err))
	}
	p.next()
	return regex
}

// Ensure current token is tok, and parse next token into p.tok.
func (p *parser) expect(tok Token) {
	if p.tok != tok {
		panic(p.error("expected %s instead of %s", tok, p.tok))
	}
	p.next()
}

// Return true iff current token matches one of the given operators,
// but don't parse next token.
func (p *parser) matches(operators ...Token) bool {
	for _, operator := range operators {
		if p.tok == operator {
			return true
		}
	}
	return false
}

// Format given string and args with Sprintf and return *ParseError
// with that message and the current position.
func (p *parser) error(format string, args ...interface{}) error {
	message := fmt.Sprintf(format, args...)
	return &ParseError{p.pos, message}
}

// Parse call to a user-defined function (and record call site for
// resolving later).
func (p *parser) userCall(name string, pos Position) *UserCallExpr {
	p.expect(LPAREN)
	args := []Expr{}
	i := 0
	for !p.matches(NEWLINE, RPAREN) {
		if i > 0 {
			p.commaNewlines()
		}
		arg := p.expr()
		p.processUserCallArg(name, arg, i)
		args = append(args, arg)
		i++
	}
	p.expect(RPAREN)
	call := &UserCallExpr{false, -1, name, args} // index is resolved later
	p.recordUserCall(call, pos)
	return call
}
