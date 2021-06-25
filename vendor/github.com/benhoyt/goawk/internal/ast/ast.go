// GoAWK parser - abstract syntax tree structs

package ast

import (
	"fmt"
	"strconv"
	"strings"

	. "github.com/benhoyt/goawk/lexer"
)

// Stmts is a block containing multiple statements.
type Stmts []Stmt

func (ss Stmts) String() string {
	lines := []string{}
	for _, s := range ss {
		subLines := strings.Split(s.String(), "\n")
		for _, sl := range subLines {
			lines = append(lines, "    "+sl+"\n")
		}
	}
	return strings.Join(lines, "")
}

// Action is pattern-action section of a program.
type Action struct {
	Pattern []Expr
	Stmts   Stmts
}

func (a *Action) String() string {
	patterns := make([]string, len(a.Pattern))
	for i, p := range a.Pattern {
		patterns[i] = p.String()
	}
	sep := ""
	if len(patterns) > 0 && a.Stmts != nil {
		sep = " "
	}
	stmtsStr := ""
	if a.Stmts != nil {
		stmtsStr = "{\n" + a.Stmts.String() + "}"
	}
	return strings.Join(patterns, ", ") + sep + stmtsStr
}

// Expr is the abstract syntax tree for any AWK expression.
type Expr interface {
	expr()
	String() string
}

// All these types implement the Expr interface.
func (e *FieldExpr) expr()     {}
func (e *UnaryExpr) expr()     {}
func (e *BinaryExpr) expr()    {}
func (e *ArrayExpr) expr()     {}
func (e *InExpr) expr()        {}
func (e *CondExpr) expr()      {}
func (e *NumExpr) expr()       {}
func (e *StrExpr) expr()       {}
func (e *RegExpr) expr()       {}
func (e *VarExpr) expr()       {}
func (e *IndexExpr) expr()     {}
func (e *AssignExpr) expr()    {}
func (e *AugAssignExpr) expr() {}
func (e *IncrExpr) expr()      {}
func (e *CallExpr) expr()      {}
func (e *UserCallExpr) expr()  {}
func (e *MultiExpr) expr()     {}
func (e *GetlineExpr) expr()   {}

// Field expression like $0.
type FieldExpr struct {
	Index Expr
}

func (e *FieldExpr) String() string {
	return "$" + e.Index.String()
}

// Unary expression like -1234.
type UnaryExpr struct {
	Op    Token
	Value Expr
}

func (e *UnaryExpr) String() string {
	return e.Op.String() + e.Value.String()
}

// Binary expression like 1 + 2.
type BinaryExpr struct {
	Left  Expr
	Op    Token
	Right Expr
}

func (e *BinaryExpr) String() string {
	var opStr string
	if e.Op == CONCAT {
		opStr = " "
	} else {
		opStr = " " + e.Op.String() + " "
	}
	return "(" + e.Left.String() + opStr + e.Right.String() + ")"
}

// Array reference. Not really a stand-alone expression, except as
// an argument to split() or a user function call.
type ArrayExpr struct {
	Scope VarScope
	Index int
	Name  string
}

func (e *ArrayExpr) String() string {
	return e.Name
}

// In expression like (index in array).
type InExpr struct {
	Index []Expr
	Array *ArrayExpr
}

func (e *InExpr) String() string {
	if len(e.Index) == 1 {
		return "(" + e.Index[0].String() + " in " + e.Array.String() + ")"
	}
	indices := make([]string, len(e.Index))
	for i, index := range e.Index {
		indices[i] = index.String()
	}
	return "((" + strings.Join(indices, ", ") + ") in " + e.Array.String() + ")"
}

// Conditional expression like cond ? 1 : 0.
type CondExpr struct {
	Cond  Expr
	True  Expr
	False Expr
}

func (e *CondExpr) String() string {
	return "(" + e.Cond.String() + " ? " + e.True.String() + " : " + e.False.String() + ")"
}

// Literal number like 1234.
type NumExpr struct {
	Value float64
}

func (e *NumExpr) String() string {
	return fmt.Sprintf("%.6g", e.Value)
}

// Literal string like "foo".
type StrExpr struct {
	Value string
}

func (e *StrExpr) String() string {
	return strconv.Quote(e.Value)
}

// Stand-alone regex expression, equivalent to: $0 ~ /regex/.
type RegExpr struct {
	Regex string
}

func (e *RegExpr) String() string {
	escaped := strings.Replace(e.Regex, "/", `\/`, -1)
	return "/" + escaped + "/"
}

type VarScope int

const (
	ScopeSpecial VarScope = iota
	ScopeGlobal
	ScopeLocal
)

// Variable reference (special var, global, or local). Index is the
// resolved variable index used by the interpreter; Name is the
// original name used by String().
type VarExpr struct {
	Scope VarScope
	Index int
	Name  string
}

func (e *VarExpr) String() string {
	return e.Name
}

// Index expression like a[k] (rvalue or lvalue).
type IndexExpr struct {
	Array *ArrayExpr
	Index []Expr
}

func (e *IndexExpr) String() string {
	indices := make([]string, len(e.Index))
	for i, index := range e.Index {
		indices[i] = index.String()
	}
	return e.Array.String() + "[" + strings.Join(indices, ", ") + "]"
}

// Assignment expression like x = 1234.
type AssignExpr struct {
	Left  Expr // can be one of: var, array[x], $n
	Right Expr
}

func (e *AssignExpr) String() string {
	return e.Left.String() + " = " + e.Right.String()
}

// Augmented assignment expression like x += 5.
type AugAssignExpr struct {
	Left  Expr // can be one of: var, array[x], $n
	Op    Token
	Right Expr
}

func (e *AugAssignExpr) String() string {
	return e.Left.String() + " " + e.Op.String() + "= " + e.Right.String()
}

// Increment or decrement expression like x++ or --y.
type IncrExpr struct {
	Expr Expr
	Op   Token
	Pre  bool
}

func (e *IncrExpr) String() string {
	if e.Pre {
		return e.Op.String() + e.Expr.String()
	} else {
		return e.Expr.String() + e.Op.String()
	}
}

// Builtin function call like length($1).
type CallExpr struct {
	Func Token
	Args []Expr
}

func (e *CallExpr) String() string {
	args := make([]string, len(e.Args))
	for i, a := range e.Args {
		args[i] = a.String()
	}
	return e.Func.String() + "(" + strings.Join(args, ", ") + ")"
}

// User-defined function call like my_func(1, 2, 3). Index is the
// resolved function index used by the interpreter; Name is the
// original name used by String().
type UserCallExpr struct {
	Native bool // false = AWK-defined function, true = native Go func
	Index  int
	Name   string
	Args   []Expr
}

func (e *UserCallExpr) String() string {
	args := make([]string, len(e.Args))
	for i, a := range e.Args {
		args[i] = a.String()
	}
	return e.Name + "(" + strings.Join(args, ", ") + ")"
}

// MultiExpr isn't an interpretable expression, but it's used as a
// pseudo-expression for print[f] parsing.
type MultiExpr struct {
	Exprs []Expr
}

func (e *MultiExpr) String() string {
	exprs := make([]string, len(e.Exprs))
	for i, e := range e.Exprs {
		exprs[i] = e.String()
	}
	return "(" + strings.Join(exprs, ", ") + ")"
}

// Getline expression (read from file or pipe input).
type GetlineExpr struct {
	Command Expr
	Var     *VarExpr
	File    Expr
}

func (e *GetlineExpr) String() string {
	s := ""
	if e.Command != nil {
		s += e.Command.String() + " |"
	}
	s += "getline"
	if e.Var != nil {
		s += " " + e.Var.String()
	}
	if e.File != nil {
		s += " <" + e.File.String()
	}
	return s
}

// IsLValue returns true if the given expression can be used as an
// lvalue (on the left-hand side of an assignment, in a ++ or --
// operation, or as the third argument to sub or gsub).
func IsLValue(expr Expr) bool {
	switch expr.(type) {
	case *VarExpr, *IndexExpr, *FieldExpr:
		return true
	default:
		return false
	}
}

// Stmt is the abstract syntax tree for any AWK statement.
type Stmt interface {
	stmt()
	String() string
}

// All these types implement the Stmt interface.
func (s *PrintStmt) stmt()    {}
func (s *PrintfStmt) stmt()   {}
func (s *ExprStmt) stmt()     {}
func (s *IfStmt) stmt()       {}
func (s *ForStmt) stmt()      {}
func (s *ForInStmt) stmt()    {}
func (s *WhileStmt) stmt()    {}
func (s *DoWhileStmt) stmt()  {}
func (s *BreakStmt) stmt()    {}
func (s *ContinueStmt) stmt() {}
func (s *NextStmt) stmt()     {}
func (s *ExitStmt) stmt()     {}
func (s *DeleteStmt) stmt()   {}
func (s *ReturnStmt) stmt()   {}
func (s *BlockStmt) stmt()    {}

// Print statement like print $1, $3.
type PrintStmt struct {
	Args     []Expr
	Redirect Token
	Dest     Expr
}

func (s *PrintStmt) String() string {
	return printString("print", s.Args, s.Redirect, s.Dest)
}

func printString(f string, args []Expr, redirect Token, dest Expr) string {
	parts := make([]string, len(args))
	for i, a := range args {
		parts[i] = a.String()
	}
	str := f + " " + strings.Join(parts, ", ")
	if dest != nil {
		str += " " + redirect.String() + dest.String()
	}
	return str
}

// Printf statement like printf "%3d", 1234.
type PrintfStmt struct {
	Args     []Expr
	Redirect Token
	Dest     Expr
}

func (s *PrintfStmt) String() string {
	return printString("printf", s.Args, s.Redirect, s.Dest)
}

// Expression statement like a bare function call: my_func(x).
type ExprStmt struct {
	Expr Expr
}

func (s *ExprStmt) String() string {
	return s.Expr.String()
}

// If or if-else statement.
type IfStmt struct {
	Cond Expr
	Body Stmts
	Else Stmts
}

func (s *IfStmt) String() string {
	str := "if (" + trimParens(s.Cond.String()) + ") {\n" + s.Body.String() + "}"
	if len(s.Else) > 0 {
		str += " else {\n" + s.Else.String() + "}"
	}
	return str
}

// C-like for loop: for (i=0; i<10; i++) print i.
type ForStmt struct {
	Pre  Stmt
	Cond Expr
	Post Stmt
	Body Stmts
}

func (s *ForStmt) String() string {
	preStr := ""
	if s.Pre != nil {
		preStr = s.Pre.String()
	}
	condStr := ""
	if s.Cond != nil {
		condStr = " " + trimParens(s.Cond.String())
	}
	postStr := ""
	if s.Post != nil {
		postStr = " " + s.Post.String()
	}
	return "for (" + preStr + ";" + condStr + ";" + postStr + ") {\n" + s.Body.String() + "}"
}

// For-in loop: for (k in a) print k, a[k].
type ForInStmt struct {
	Var   *VarExpr
	Array *ArrayExpr
	Body  Stmts
}

func (s *ForInStmt) String() string {
	return "for (" + s.Var.String() + " in " + s.Array.String() + ") {\n" + s.Body.String() + "}"
}

// While loop.
type WhileStmt struct {
	Cond Expr
	Body Stmts
}

func (s *WhileStmt) String() string {
	return "while (" + trimParens(s.Cond.String()) + ") {\n" + s.Body.String() + "}"
}

// Do-while loop.
type DoWhileStmt struct {
	Body Stmts
	Cond Expr
}

func (s *DoWhileStmt) String() string {
	return "do {\n" + s.Body.String() + "} while (" + trimParens(s.Cond.String()) + ")"
}

// Break statement.
type BreakStmt struct{}

func (s *BreakStmt) String() string {
	return "break"
}

// Continue statement.
type ContinueStmt struct{}

func (s *ContinueStmt) String() string {
	return "continue"
}

// Next statement.
type NextStmt struct{}

func (s *NextStmt) String() string {
	return "next"
}

// Exit statement.
type ExitStmt struct {
	Status Expr
}

func (s *ExitStmt) String() string {
	var statusStr string
	if s.Status != nil {
		statusStr = " " + s.Status.String()
	}
	return "exit" + statusStr
}

// Delete statement like delete a[k].
type DeleteStmt struct {
	Array *ArrayExpr
	Index []Expr
}

func (s *DeleteStmt) String() string {
	indices := make([]string, len(s.Index))
	for i, index := range s.Index {
		indices[i] = index.String()
	}
	return "delete " + s.Array.String() + "[" + strings.Join(indices, ", ") + "]"
}

// Return statement.
type ReturnStmt struct {
	Value Expr
}

func (s *ReturnStmt) String() string {
	var valueStr string
	if s.Value != nil {
		valueStr = " " + s.Value.String()
	}
	return "return" + valueStr
}

// Stand-alone block like { print "x" }.
type BlockStmt struct {
	Body Stmts
}

func (s *BlockStmt) String() string {
	return "{\n" + s.Body.String() + "}"
}

// Function is the AST for a user-defined function.
type Function struct {
	Name   string
	Params []string
	Arrays []bool
	Body   Stmts
}

func (f *Function) String() string {
	return "function " + f.Name + "(" + strings.Join(f.Params, ", ") + ") {\n" +
		f.Body.String() + "}"
}

func trimParens(s string) string {
	if strings.HasPrefix(s, "(") && strings.HasSuffix(s, ")") {
		s = s[1 : len(s)-1]
	}
	return s
}
