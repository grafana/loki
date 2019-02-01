// Copyright 2011 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package template

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

var debug = flag.Bool("debug", false, "show the errors produced by the tests")

// T has lots of interesting pieces to use to test execution.
type T struct {
	// Basics
	True        bool
	I           int
	U16         uint16
	X           string
	FloatZero   float64
	ComplexZero complex128
	// Nested structs.
	U *U
	// Struct with String method.
	V0     V
	V1, V2 *V
	// Struct with Error method.
	W0     W
	W1, W2 *W
	// Slices
	SI      []int
	SIEmpty []int
	SB      []bool
	// Maps
	MSI      map[string]int
	MSIone   map[string]int // one element, for deterministic output
	MSIEmpty map[string]int
	MXI      map[interface{}]int
	MII      map[int]int
	SMSI     []map[string]int
	// Empty interfaces; used to see if we can dig inside one.
	Empty0 interface{} // nil
	Empty1 interface{}
	Empty2 interface{}
	Empty3 interface{}
	Empty4 interface{}
	// Non-empty interface.
	NonEmptyInterface I
	// Stringer.
	Str fmt.Stringer
	Err error
	// Pointers
	PI  *int
	PS  *string
	PSI *[]int
	NIL *int
	// Function (not method)
	BinaryFunc      func(string, string) string
	VariadicFunc    func(...string) string
	VariadicFuncInt func(int, ...string) string
	NilOKFunc       func(*int) bool
	ErrFunc         func() (string, error)
	// Template to test evaluation of templates.
	Tmpl *Template
	// Unexported field; cannot be accessed by template.
	unexported int
}

type U struct {
	V string
}

type V struct {
	j int
}

func (v *V) String() string {
	if v == nil {
		return "nilV"
	}
	return fmt.Sprintf("<%d>", v.j)
}

type W struct {
	k int
}

func (w *W) Error() string {
	if w == nil {
		return "nilW"
	}
	return fmt.Sprintf("[%d]", w.k)
}

var tVal = &T{
	True:   true,
	I:      17,
	U16:    16,
	X:      "x",
	U:      &U{"v"},
	V0:     V{6666},
	V1:     &V{7777}, // leave V2 as nil
	W0:     W{888},
	W1:     &W{999}, // leave W2 as nil
	SI:     []int{3, 4, 5},
	SB:     []bool{true, false},
	MSI:    map[string]int{"one": 1, "two": 2, "three": 3},
	MSIone: map[string]int{"one": 1},
	MXI:    map[interface{}]int{"one": 1},
	MII:    map[int]int{1: 1},
	SMSI: []map[string]int{
		{"one": 1, "two": 2},
		{"eleven": 11, "twelve": 12},
	},
	Empty1:            3,
	Empty2:            "empty2",
	Empty3:            []int{7, 8},
	Empty4:            &U{"UinEmpty"},
	NonEmptyInterface: new(T),
	Str:               bytes.NewBuffer([]byte("foozle")),
	Err:               errors.New("erroozle"),
	PI:                newInt(23),
	PS:                newString("a string"),
	PSI:               newIntSlice(21, 22, 23),
	BinaryFunc:        func(a, b string) string { return fmt.Sprintf("[%s=%s]", a, b) },
	VariadicFunc:      func(s ...string) string { return fmt.Sprint("<", strings.Join(s, "+"), ">") },
	VariadicFuncInt:   func(a int, s ...string) string { return fmt.Sprint(a, "=<", strings.Join(s, "+"), ">") },
	NilOKFunc:         func(s *int) bool { return s == nil },
	ErrFunc:           func() (string, error) { return "bla", nil },
	Tmpl:              Must(New("x").Parse("test template")), // "x" is the value of .X
}

// A non-empty interface.
type I interface {
	Method0() string
}

var iVal I = tVal

// Helpers for creation.
func newInt(n int) *int {
	return &n
}

func newString(s string) *string {
	return &s
}

func newIntSlice(n ...int) *[]int {
	p := new([]int)
	*p = make([]int, len(n))
	copy(*p, n)
	return p
}

// Simple methods with and without arguments.
func (t *T) Method0() string {
	return "M0"
}

func (t *T) Method1(a int) int {
	return a
}

func (t *T) Method2(a uint16, b string) string {
	return fmt.Sprintf("Method2: %d %s", a, b)
}

func (t *T) Method3(v interface{}) string {
	return fmt.Sprintf("Method3: %v", v)
}

func (t *T) Copy() *T {
	n := new(T)
	*n = *t
	return n
}

func (t *T) MAdd(a int, b []int) []int {
	v := make([]int, len(b))
	for i, x := range b {
		v[i] = x + a
	}
	return v
}

var myError = errors.New("my error")

// MyError returns a value and an error according to its argument.
func (t *T) MyError(error bool) (bool, error) {
	if error {
		return true, myError
	}
	return false, nil
}

// A few methods to test chaining.
func (t *T) GetU() *U {
	return t.U
}

func (u *U) TrueFalse(b bool) string {
	if b {
		return "true"
	}
	return ""
}

func typeOf(arg interface{}) string {
	return fmt.Sprintf("%T", arg)
}

type execTest struct {
	name   string
	input  string
	output string
	data   interface{}
	ok     bool
}

// bigInt and bigUint are hex string representing numbers either side
// of the max int boundary.
// We do it this way so the test doesn't depend on ints being 32 bits.
var (
	bigInt  = fmt.Sprintf("0x%x", int(1<<uint(reflect.TypeOf(0).Bits()-1)-1))
	bigUint = fmt.Sprintf("0x%x", uint(1<<uint(reflect.TypeOf(0).Bits()-1)))
)

var execTests = []execTest{
	// Trivial cases.
	{"empty", "", "", nil, true},
	{"text", "some text", "some text", nil, true},
	{"nil action", "{{nil}}", "", nil, false},

	// Ideal constants.
	{"ideal int", "{{typeOf 3}}", "int", 0, true},
	{"ideal float", "{{typeOf 1.0}}", "float64", 0, true},
	{"ideal exp float", "{{typeOf 1e1}}", "float64", 0, true},
	{"ideal complex", "{{typeOf 1i}}", "complex128", 0, true},
	{"ideal int", "{{typeOf " + bigInt + "}}", "int", 0, true},
	{"ideal too big", "{{typeOf " + bigUint + "}}", "", 0, false},
	{"ideal nil without type", "{{nil}}", "", 0, false},

	// Fields of structs.
	{".X", "-{{.X}}-", "-x-", tVal, true},
	{".U.V", "-{{.U.V}}-", "-v-", tVal, true},
	{".unexported", "{{.unexported}}", "", tVal, false},

	// Fields on maps.
	{"map .one", "{{.MSI.one}}", "1", tVal, true},
	{"map .two", "{{.MSI.two}}", "2", tVal, true},
	{"map .NO", "{{.MSI.NO}}", "<no value>", tVal, true},
	{"map .one interface", "{{.MXI.one}}", "1", tVal, true},
	{"map .WRONG args", "{{.MSI.one 1}}", "", tVal, false},
	{"map .WRONG type", "{{.MII.one}}", "", tVal, false},

	// Dots of all kinds to test basic evaluation.
	{"dot int", "<{{.}}>", "<13>", 13, true},
	{"dot uint", "<{{.}}>", "<14>", uint(14), true},
	{"dot float", "<{{.}}>", "<15.1>", 15.1, true},
	{"dot bool", "<{{.}}>", "<true>", true, true},
	{"dot complex", "<{{.}}>", "<(16.2-17i)>", 16.2 - 17i, true},
	{"dot string", "<{{.}}>", "<hello>", "hello", true},
	{"dot slice", "<{{.}}>", "<[-1 -2 -3]>", []int{-1, -2, -3}, true},
	{"dot map", "<{{.}}>", "<map[two:22]>", map[string]int{"two": 22}, true},
	{"dot struct", "<{{.}}>", "<{7 seven}>", struct {
		a int
		b string
	}{7, "seven"}, true},

	// Variables.
	{"$ int", "{{$}}", "123", 123, true},
	{"$.I", "{{$.I}}", "17", tVal, true},
	{"$.U.V", "{{$.U.V}}", "v", tVal, true},
	{"declare in action", "{{$x := $.U.V}}{{$x}}", "v", tVal, true},

	// Type with String method.
	{"V{6666}.String()", "-{{.V0}}-", "-<6666>-", tVal, true},
	{"&V{7777}.String()", "-{{.V1}}-", "-<7777>-", tVal, true},
	{"(*V)(nil).String()", "-{{.V2}}-", "-nilV-", tVal, true},

	// Type with Error method.
	{"W{888}.Error()", "-{{.W0}}-", "-[888]-", tVal, true},
	{"&W{999}.Error()", "-{{.W1}}-", "-[999]-", tVal, true},
	{"(*W)(nil).Error()", "-{{.W2}}-", "-nilW-", tVal, true},

	// Pointers.
	{"*int", "{{.PI}}", "23", tVal, true},
	{"*string", "{{.PS}}", "a string", tVal, true},
	{"*[]int", "{{.PSI}}", "[21 22 23]", tVal, true},
	{"*[]int[1]", "{{index .PSI 1}}", "22", tVal, true},
	{"NIL", "{{.NIL}}", "<nil>", tVal, true},

	// Empty interfaces holding values.
	{"empty nil", "{{.Empty0}}", "<no value>", tVal, true},
	{"empty with int", "{{.Empty1}}", "3", tVal, true},
	{"empty with string", "{{.Empty2}}", "empty2", tVal, true},
	{"empty with slice", "{{.Empty3}}", "[7 8]", tVal, true},
	{"empty with struct", "{{.Empty4}}", "{UinEmpty}", tVal, true},
	{"empty with struct, field", "{{.Empty4.V}}", "UinEmpty", tVal, true},

	// Method calls.
	{".Method0", "-{{.Method0}}-", "-M0-", tVal, true},
	{".Method1(1234)", "-{{.Method1 1234}}-", "-1234-", tVal, true},
	{".Method1(.I)", "-{{.Method1 .I}}-", "-17-", tVal, true},
	{".Method2(3, .X)", "-{{.Method2 3 .X}}-", "-Method2: 3 x-", tVal, true},
	{".Method2(.U16, `str`)", "-{{.Method2 .U16 `str`}}-", "-Method2: 16 str-", tVal, true},
	{".Method2(.U16, $x)", "{{if $x := .X}}-{{.Method2 .U16 $x}}{{end}}-", "-Method2: 16 x-", tVal, true},
	{".Method3(nil constant)", "-{{.Method3 nil}}-", "-Method3: <nil>-", tVal, true},
	{".Method3(nil value)", "-{{.Method3 .MXI.unset}}-", "-Method3: <nil>-", tVal, true},
	{"method on var", "{{if $x := .}}-{{$x.Method2 .U16 $x.X}}{{end}}-", "-Method2: 16 x-", tVal, true},
	{"method on chained var",
		"{{range .MSIone}}{{if $.U.TrueFalse $.True}}{{$.U.TrueFalse $.True}}{{else}}WRONG{{end}}{{end}}",
		"true", tVal, true},
	{"chained method",
		"{{range .MSIone}}{{if $.GetU.TrueFalse $.True}}{{$.U.TrueFalse $.True}}{{else}}WRONG{{end}}{{end}}",
		"true", tVal, true},
	{"chained method on variable",
		"{{with $x := .}}{{with .SI}}{{$.GetU.TrueFalse $.True}}{{end}}{{end}}",
		"true", tVal, true},
	{".NilOKFunc not nil", "{{call .NilOKFunc .PI}}", "false", tVal, true},
	{".NilOKFunc nil", "{{call .NilOKFunc nil}}", "true", tVal, true},

	// Function call builtin.
	{".BinaryFunc", "{{call .BinaryFunc `1` `2`}}", "[1=2]", tVal, true},
	{".VariadicFunc0", "{{call .VariadicFunc}}", "<>", tVal, true},
	{".VariadicFunc2", "{{call .VariadicFunc `he` `llo`}}", "<he+llo>", tVal, true},
	{".VariadicFuncInt", "{{call .VariadicFuncInt 33 `he` `llo`}}", "33=<he+llo>", tVal, true},
	{"if .BinaryFunc call", "{{ if .BinaryFunc}}{{call .BinaryFunc `1` `2`}}{{end}}", "[1=2]", tVal, true},
	{"if not .BinaryFunc call", "{{ if not .BinaryFunc}}{{call .BinaryFunc `1` `2`}}{{else}}No{{end}}", "No", tVal, true},
	{"Interface Call", `{{stringer .S}}`, "foozle", map[string]interface{}{"S": bytes.NewBufferString("foozle")}, true},
	{".ErrFunc", "{{call .ErrFunc}}", "bla", tVal, true},

	// Erroneous function calls (check args).
	{".BinaryFuncTooFew", "{{call .BinaryFunc `1`}}", "", tVal, false},
	{".BinaryFuncTooMany", "{{call .BinaryFunc `1` `2` `3`}}", "", tVal, false},
	{".BinaryFuncBad0", "{{call .BinaryFunc 1 3}}", "", tVal, false},
	{".BinaryFuncBad1", "{{call .BinaryFunc `1` 3}}", "", tVal, false},
	{".VariadicFuncBad0", "{{call .VariadicFunc 3}}", "", tVal, false},
	{".VariadicFuncIntBad0", "{{call .VariadicFuncInt}}", "", tVal, false},
	{".VariadicFuncIntBad`", "{{call .VariadicFuncInt `x`}}", "", tVal, false},
	{".VariadicFuncNilBad", "{{call .VariadicFunc nil}}", "", tVal, false},

	// Pipelines.
	{"pipeline", "-{{.Method0 | .Method2 .U16}}-", "-Method2: 16 M0-", tVal, true},
	{"pipeline func", "-{{call .VariadicFunc `llo` | call .VariadicFunc `he` }}-", "-<he+<llo>>-", tVal, true},

	// Parenthesized expressions
	{"parens in pipeline", "{{printf `%d %d %d` (1) (2 | add 3) (add 4 (add 5 6))}}", "1 5 15", tVal, true},

	// Parenthesized expressions with field accesses
	{"parens: $ in paren", "{{($).X}}", "x", tVal, true},
	{"parens: $.GetU in paren", "{{($.GetU).V}}", "v", tVal, true},
	{"parens: $ in paren in pipe", "{{($ | echo).X}}", "x", tVal, true},
	{"parens: spaces and args", `{{(makemap "up" "down" "left" "right").left}}`, "right", tVal, true},

	// If.
	{"if true", "{{if true}}TRUE{{end}}", "TRUE", tVal, true},
	{"if false", "{{if false}}TRUE{{else}}FALSE{{end}}", "FALSE", tVal, true},
	{"if nil", "{{if nil}}TRUE{{end}}", "", tVal, false},
	{"if 1", "{{if 1}}NON-ZERO{{else}}ZERO{{end}}", "NON-ZERO", tVal, true},
	{"if 0", "{{if 0}}NON-ZERO{{else}}ZERO{{end}}", "ZERO", tVal, true},
	{"if 1.5", "{{if 1.5}}NON-ZERO{{else}}ZERO{{end}}", "NON-ZERO", tVal, true},
	{"if 0.0", "{{if .FloatZero}}NON-ZERO{{else}}ZERO{{end}}", "ZERO", tVal, true},
	{"if 1.5i", "{{if 1.5i}}NON-ZERO{{else}}ZERO{{end}}", "NON-ZERO", tVal, true},
	{"if 0.0i", "{{if .ComplexZero}}NON-ZERO{{else}}ZERO{{end}}", "ZERO", tVal, true},
	{"if emptystring", "{{if ``}}NON-EMPTY{{else}}EMPTY{{end}}", "EMPTY", tVal, true},
	{"if string", "{{if `notempty`}}NON-EMPTY{{else}}EMPTY{{end}}", "NON-EMPTY", tVal, true},
	{"if emptyslice", "{{if .SIEmpty}}NON-EMPTY{{else}}EMPTY{{end}}", "EMPTY", tVal, true},
	{"if slice", "{{if .SI}}NON-EMPTY{{else}}EMPTY{{end}}", "NON-EMPTY", tVal, true},
	{"if emptymap", "{{if .MSIEmpty}}NON-EMPTY{{else}}EMPTY{{end}}", "EMPTY", tVal, true},
	{"if map", "{{if .MSI}}NON-EMPTY{{else}}EMPTY{{end}}", "NON-EMPTY", tVal, true},
	{"if map unset", "{{if .MXI.none}}NON-ZERO{{else}}ZERO{{end}}", "ZERO", tVal, true},
	{"if map not unset", "{{if not .MXI.none}}ZERO{{else}}NON-ZERO{{end}}", "ZERO", tVal, true},
	{"if $x with $y int", "{{if $x := true}}{{with $y := .I}}{{$x}},{{$y}}{{end}}{{end}}", "true,17", tVal, true},
	{"if $x with $x int", "{{if $x := true}}{{with $x := .I}}{{$x}},{{end}}{{$x}}{{end}}", "17,true", tVal, true},
	{"if else if", "{{if false}}FALSE{{else if true}}TRUE{{end}}", "TRUE", tVal, true},
	{"if else chain", "{{if eq 1 3}}1{{else if eq 2 3}}2{{else if eq 3 3}}3{{end}}", "3", tVal, true},

	// Print etc.
	{"print", `{{print "hello, print"}}`, "hello, print", tVal, true},
	{"print 123", `{{print 1 2 3}}`, "1 2 3", tVal, true},
	{"print nil", `{{print nil}}`, "<nil>", tVal, true},
	{"println", `{{println 1 2 3}}`, "1 2 3\n", tVal, true},
	{"printf int", `{{printf "%04x" 127}}`, "007f", tVal, true},
	{"printf float", `{{printf "%g" 3.5}}`, "3.5", tVal, true},
	{"printf complex", `{{printf "%g" 1+7i}}`, "(1+7i)", tVal, true},
	{"printf string", `{{printf "%s" "hello"}}`, "hello", tVal, true},
	{"printf function", `{{printf "%#q" zeroArgs}}`, "`zeroArgs`", tVal, true},
	{"printf field", `{{printf "%s" .U.V}}`, "v", tVal, true},
	{"printf method", `{{printf "%s" .Method0}}`, "M0", tVal, true},
	{"printf dot", `{{with .I}}{{printf "%d" .}}{{end}}`, "17", tVal, true},
	{"printf var", `{{with $x := .I}}{{printf "%d" $x}}{{end}}`, "17", tVal, true},
	{"printf lots", `{{printf "%d %s %g %s" 127 "hello" 7-3i .Method0}}`, "127 hello (7-3i) M0", tVal, true},

	// HTML.
	{"html", `{{html "<script>alert(\"XSS\");</script>"}}`,
		"&lt;script&gt;alert(&#34;XSS&#34;);&lt;/script&gt;", nil, true},
	{"html pipeline", `{{printf "<script>alert(\"XSS\");</script>" | html}}`,
		"&lt;script&gt;alert(&#34;XSS&#34;);&lt;/script&gt;", nil, true},
	{"html", `{{html .PS}}`, "a string", tVal, true},

	// JavaScript.
	{"js", `{{js .}}`, `It\'d be nice.`, `It'd be nice.`, true},

	// URL query.
	{"urlquery", `{{"http://www.example.org/"|urlquery}}`, "http%3A%2F%2Fwww.example.org%2F", nil, true},

	// Booleans
	{"not", "{{not true}} {{not false}}", "false true", nil, true},
	{"and", "{{and false 0}} {{and 1 0}} {{and 0 true}} {{and 1 1}}", "false 0 0 1", nil, true},
	{"or", "{{or 0 0}} {{or 1 0}} {{or 0 true}} {{or 1 1}}", "0 1 true 1", nil, true},
	{"boolean if", "{{if and true 1 `hi`}}TRUE{{else}}FALSE{{end}}", "TRUE", tVal, true},
	{"boolean if not", "{{if and true 1 `hi` | not}}TRUE{{else}}FALSE{{end}}", "FALSE", nil, true},

	// Indexing.
	{"slice[0]", "{{index .SI 0}}", "3", tVal, true},
	{"slice[1]", "{{index .SI 1}}", "4", tVal, true},
	{"slice[HUGE]", "{{index .SI 10}}", "", tVal, false},
	{"slice[WRONG]", "{{index .SI `hello`}}", "", tVal, false},
	{"map[one]", "{{index .MSI `one`}}", "1", tVal, true},
	{"map[two]", "{{index .MSI `two`}}", "2", tVal, true},
	{"map[NO]", "{{index .MSI `XXX`}}", "0", tVal, true},
	{"map[nil]", "{{index .MSI nil}}", "0", tVal, true},
	{"map[WRONG]", "{{index .MSI 10}}", "", tVal, false},
	{"double index", "{{index .SMSI 1 `eleven`}}", "11", tVal, true},

	// Len.
	{"slice", "{{len .SI}}", "3", tVal, true},
	{"map", "{{len .MSI }}", "3", tVal, true},
	{"len of int", "{{len 3}}", "", tVal, false},
	{"len of nothing", "{{len .Empty0}}", "", tVal, false},

	// With.
	{"with true", "{{with true}}{{.}}{{end}}", "true", tVal, true},
	{"with false", "{{with false}}{{.}}{{else}}FALSE{{end}}", "FALSE", tVal, true},
	{"with 1", "{{with 1}}{{.}}{{else}}ZERO{{end}}", "1", tVal, true},
	{"with 0", "{{with 0}}{{.}}{{else}}ZERO{{end}}", "ZERO", tVal, true},
	{"with 1.5", "{{with 1.5}}{{.}}{{else}}ZERO{{end}}", "1.5", tVal, true},
	{"with 0.0", "{{with .FloatZero}}{{.}}{{else}}ZERO{{end}}", "ZERO", tVal, true},
	{"with 1.5i", "{{with 1.5i}}{{.}}{{else}}ZERO{{end}}", "(0+1.5i)", tVal, true},
	{"with 0.0i", "{{with .ComplexZero}}{{.}}{{else}}ZERO{{end}}", "ZERO", tVal, true},
	{"with emptystring", "{{with ``}}{{.}}{{else}}EMPTY{{end}}", "EMPTY", tVal, true},
	{"with string", "{{with `notempty`}}{{.}}{{else}}EMPTY{{end}}", "notempty", tVal, true},
	{"with emptyslice", "{{with .SIEmpty}}{{.}}{{else}}EMPTY{{end}}", "EMPTY", tVal, true},
	{"with slice", "{{with .SI}}{{.}}{{else}}EMPTY{{end}}", "[3 4 5]", tVal, true},
	{"with emptymap", "{{with .MSIEmpty}}{{.}}{{else}}EMPTY{{end}}", "EMPTY", tVal, true},
	{"with map", "{{with .MSIone}}{{.}}{{else}}EMPTY{{end}}", "map[one:1]", tVal, true},
	{"with empty interface, struct field", "{{with .Empty4}}{{.V}}{{end}}", "UinEmpty", tVal, true},
	{"with $x int", "{{with $x := .I}}{{$x}}{{end}}", "17", tVal, true},
	{"with $x struct.U.V", "{{with $x := $}}{{$x.U.V}}{{end}}", "v", tVal, true},
	{"with variable and action", "{{with $x := $}}{{$y := $.U.V}}{{$y}}{{end}}", "v", tVal, true},

	// Range.
	{"range []int", "{{range .SI}}-{{.}}-{{end}}", "-3--4--5-", tVal, true},
	{"range empty no else", "{{range .SIEmpty}}-{{.}}-{{end}}", "", tVal, true},
	{"range []int else", "{{range .SI}}-{{.}}-{{else}}EMPTY{{end}}", "-3--4--5-", tVal, true},
	{"range empty else", "{{range .SIEmpty}}-{{.}}-{{else}}EMPTY{{end}}", "EMPTY", tVal, true},
	{"range []bool", "{{range .SB}}-{{.}}-{{end}}", "-true--false-", tVal, true},
	{"range []int method", "{{range .SI | .MAdd .I}}-{{.}}-{{end}}", "-20--21--22-", tVal, true},
	{"range map", "{{range .MSI}}-{{.}}-{{end}}", "-1--3--2-", tVal, true},
	{"range empty map no else", "{{range .MSIEmpty}}-{{.}}-{{end}}", "", tVal, true},
	{"range map else", "{{range .MSI}}-{{.}}-{{else}}EMPTY{{end}}", "-1--3--2-", tVal, true},
	{"range empty map else", "{{range .MSIEmpty}}-{{.}}-{{else}}EMPTY{{end}}", "EMPTY", tVal, true},
	{"range empty interface", "{{range .Empty3}}-{{.}}-{{else}}EMPTY{{end}}", "-7--8-", tVal, true},
	{"range empty nil", "{{range .Empty0}}-{{.}}-{{end}}", "", tVal, true},
	{"range $x SI", "{{range $x := .SI}}<{{$x}}>{{end}}", "<3><4><5>", tVal, true},
	{"range $x $y SI", "{{range $x, $y := .SI}}<{{$x}}={{$y}}>{{end}}", "<0=3><1=4><2=5>", tVal, true},
	{"range $x MSIone", "{{range $x := .MSIone}}<{{$x}}>{{end}}", "<1>", tVal, true},
	{"range $x $y MSIone", "{{range $x, $y := .MSIone}}<{{$x}}={{$y}}>{{end}}", "<one=1>", tVal, true},
	{"range $x PSI", "{{range $x := .PSI}}<{{$x}}>{{end}}", "<21><22><23>", tVal, true},
	{"declare in range", "{{range $x := .PSI}}<{{$foo:=$x}}{{$x}}>{{end}}", "<21><22><23>", tVal, true},
	{"range count", `{{range $i, $x := count 5}}[{{$i}}]{{$x}}{{end}}`, "[0]a[1]b[2]c[3]d[4]e", tVal, true},
	{"range nil count", `{{range $i, $x := count 0}}{{else}}empty{{end}}`, "empty", tVal, true},

	// Cute examples.
	{"or as if true", `{{or .SI "slice is empty"}}`, "[3 4 5]", tVal, true},
	{"or as if false", `{{or .SIEmpty "slice is empty"}}`, "slice is empty", tVal, true},

	// Error handling.
	{"error method, error", "{{.MyError true}}", "", tVal, false},
	{"error method, no error", "{{.MyError false}}", "false", tVal, true},

	// Fixed bugs.
	// Must separate dot and receiver; otherwise args are evaluated with dot set to variable.
	{"bug0", "{{range .MSIone}}{{if $.Method1 .}}X{{end}}{{end}}", "X", tVal, true},
	// Do not loop endlessly in indirect for non-empty interfaces.
	// The bug appears with *interface only; looped forever.
	{"bug1", "{{.Method0}}", "M0", &iVal, true},
	// Was taking address of interface field, so method set was empty.
	{"bug2", "{{$.NonEmptyInterface.Method0}}", "M0", tVal, true},
	// Struct values were not legal in with - mere oversight.
	{"bug3", "{{with $}}{{.Method0}}{{end}}", "M0", tVal, true},
	// Nil interface values in if.
	{"bug4", "{{if .Empty0}}non-nil{{else}}nil{{end}}", "nil", tVal, true},
	// Stringer.
	{"bug5", "{{.Str}}", "foozle", tVal, true},
	{"bug5a", "{{.Err}}", "erroozle", tVal, true},
	// Args need to be indirected and dereferenced sometimes.
	{"bug6a", "{{vfunc .V0 .V1}}", "vfunc", tVal, true},
	{"bug6b", "{{vfunc .V0 .V0}}", "vfunc", tVal, true},
	{"bug6c", "{{vfunc .V1 .V0}}", "vfunc", tVal, true},
	{"bug6d", "{{vfunc .V1 .V1}}", "vfunc", tVal, true},
	// Legal parse but illegal execution: non-function should have no arguments.
	{"bug7a", "{{3 2}}", "", tVal, false},
	{"bug7b", "{{$x := 1}}{{$x 2}}", "", tVal, false},
	{"bug7c", "{{$x := 1}}{{3 | $x}}", "", tVal, false},
	// Pipelined arg was not being type-checked.
	{"bug8a", "{{3|oneArg}}", "", tVal, false},
	{"bug8b", "{{4|dddArg 3}}", "", tVal, false},
	// A bug was introduced that broke map lookups for lower-case names.
	{"bug9", "{{.cause}}", "neglect", map[string]string{"cause": "neglect"}, true},
	// Field chain starting with function did not work.
	{"bug10", "{{mapOfThree.three}}-{{(mapOfThree).three}}", "3-3", 0, true},
	// Dereferencing nil pointer while evaluating function arguments should not panic. Issue 7333.
	{"bug11", "{{valueString .PS}}", "", T{}, false},
	// 0xef gave constant type float64. Issue 8622.
	{"bug12xe", "{{printf `%T` 0xef}}", "int", T{}, true},
	{"bug12xE", "{{printf `%T` 0xEE}}", "int", T{}, true},
	{"bug12Xe", "{{printf `%T` 0Xef}}", "int", T{}, true},
	{"bug12XE", "{{printf `%T` 0XEE}}", "int", T{}, true},
	// Chained nodes did not work as arguments. Issue 8473.
	{"bug13", "{{print (.Copy).I}}", "17", tVal, true},
}

func zeroArgs() string {
	return "zeroArgs"
}

func oneArg(a string) string {
	return "oneArg=" + a
}

func dddArg(a int, b ...string) string {
	return fmt.Sprintln(a, b)
}

// count returns a channel that will deliver n sequential 1-letter strings starting at "a"
func count(n int) chan string {
	if n == 0 {
		return nil
	}
	c := make(chan string)
	go func() {
		for i := 0; i < n; i++ {
			c <- "abcdefghijklmnop"[i : i+1]
		}
		close(c)
	}()
	return c
}

// vfunc takes a *V and a V
func vfunc(V, *V) string {
	return "vfunc"
}

// valueString takes a string, not a pointer.
func valueString(v string) string {
	return "value is ignored"
}

func add(args ...int) int {
	sum := 0
	for _, x := range args {
		sum += x
	}
	return sum
}

func echo(arg interface{}) interface{} {
	return arg
}

func makemap(arg ...string) map[string]string {
	if len(arg)%2 != 0 {
		panic("bad makemap")
	}
	m := make(map[string]string)
	for i := 0; i < len(arg); i += 2 {
		m[arg[i]] = arg[i+1]
	}
	return m
}

func stringer(s fmt.Stringer) string {
	return s.String()
}

func mapOfThree() interface{} {
	return map[string]int{"three": 3}
}

func testExecute(execTests []execTest, template *Template, t *testing.T) {
	b := new(bytes.Buffer)
	funcs := FuncMap{
		"add":         add,
		"count":       count,
		"dddArg":      dddArg,
		"echo":        echo,
		"makemap":     makemap,
		"mapOfThree":  mapOfThree,
		"oneArg":      oneArg,
		"stringer":    stringer,
		"typeOf":      typeOf,
		"valueString": valueString,
		"vfunc":       vfunc,
		"zeroArgs":    zeroArgs,
	}
	for _, test := range execTests {
		var tmpl *Template
		var err error
		if template == nil {
			tmpl, err = New(test.name).Funcs(funcs).Parse(test.input)
		} else {
			tmpl, err = template.New(test.name).Funcs(funcs).Parse(test.input)
		}
		if err != nil {
			t.Errorf("%s: parse error: %s", test.name, err)
			continue
		}
		b.Reset()
		err = tmpl.Execute(b, test.data)
		switch {
		case !test.ok && err == nil:
			t.Errorf("%s: expected error; got none", test.name)
			continue
		case test.ok && err != nil:
			t.Errorf("%s: unexpected execute error: %s", test.name, err)
			continue
		case !test.ok && err != nil:
			// expected error, got one
			if *debug {
				fmt.Printf("%s: %s\n\t%s\n", test.name, test.input, err)
			}
		}
		result := b.String()
		if result != test.output {
			t.Errorf("%s: expected\n\t%q\ngot\n\t%q", test.name, test.output, result)
		}
	}
}

func TestExecute(t *testing.T) {
	testExecute(execTests, nil, t)
}

var delimPairs = []string{
	"", "", // default
	"{{", "}}", // same as default
	"<<", ">>", // distinct
	"|", "|", // same
	"(日)", "(本)", // peculiar
}

func TestDelims(t *testing.T) {
	const hello = "Hello, world"
	var value = struct{ Str string }{hello}
	for i := 0; i < len(delimPairs); i += 2 {
		text := ".Str"
		left := delimPairs[i+0]
		trueLeft := left
		right := delimPairs[i+1]
		trueRight := right
		if left == "" { // default case
			trueLeft = "{{"
		}
		if right == "" { // default case
			trueRight = "}}"
		}
		text = trueLeft + text + trueRight
		// Now add a comment
		text += trueLeft + "/*comment*/" + trueRight
		// Now add  an action containing a string.
		text += trueLeft + `"` + trueLeft + `"` + trueRight
		// At this point text looks like `{{.Str}}{{/*comment*/}}{{"{{"}}`.
		tmpl, err := New("delims").Delims(left, right).Parse(text)
		if err != nil {
			t.Fatalf("delim %q text %q parse err %s", left, text, err)
		}
		var b = new(bytes.Buffer)
		err = tmpl.Execute(b, value)
		if err != nil {
			t.Fatalf("delim %q exec err %s", left, err)
		}
		if b.String() != hello+trueLeft {
			t.Errorf("expected %q got %q", hello+trueLeft, b.String())
		}
	}
}

// Check that an error from a method flows back to the top.
func TestExecuteError(t *testing.T) {
	b := new(bytes.Buffer)
	tmpl := New("error")
	_, err := tmpl.Parse("{{.MyError true}}")
	if err != nil {
		t.Fatalf("parse error: %s", err)
	}
	err = tmpl.Execute(b, tVal)
	if err == nil {
		t.Errorf("expected error; got none")
	} else if !strings.Contains(err.Error(), myError.Error()) {
		if *debug {
			fmt.Printf("test execute error: %s\n", err)
		}
		t.Errorf("expected myError; got %s", err)
	}
}

const execErrorText = `line 1
line 2
line 3
{{template "one" .}}
{{define "one"}}{{template "two" .}}{{end}}
{{define "two"}}{{template "three" .}}{{end}}
{{define "three"}}{{index "hi" $}}{{end}}`

// Check that an error from a nested template contains all the relevant information.
func TestExecError(t *testing.T) {
	tmpl, err := New("top").Parse(execErrorText)
	if err != nil {
		t.Fatal("parse error:", err)
	}
	var b bytes.Buffer
	err = tmpl.Execute(&b, 5) // 5 is out of range indexing "hi"
	if err == nil {
		t.Fatal("expected error")
	}
	const want = `template: top:7:20: executing "three" at <index "hi" $>: error calling index: index out of range: 5`
	got := err.Error()
	if got != want {
		t.Errorf("expected\n%q\ngot\n%q", want, got)
	}
}

func TestJSEscaping(t *testing.T) {
	testCases := []struct {
		in, exp string
	}{
		{`a`, `a`},
		{`'foo`, `\'foo`},
		{`Go "jump" \`, `Go \"jump\" \\`},
		{`Yukihiro says "今日は世界"`, `Yukihiro says \"今日は世界\"`},
		{"unprintable \uFDFF", `unprintable \uFDFF`},
		{`<html>`, `\x3Chtml\x3E`},
	}
	for _, tc := range testCases {
		s := JSEscapeString(tc.in)
		if s != tc.exp {
			t.Errorf("JS escaping [%s] got [%s] want [%s]", tc.in, s, tc.exp)
		}
	}
}

// A nice example: walk a binary tree.

type Tree struct {
	Val         int
	Left, Right *Tree
}

// Use different delimiters to test Set.Delims.
const treeTemplate = `
	(define "tree")
	[
		(.Val)
		(with .Left)
			(template "tree" .)
		(end)
		(with .Right)
			(template "tree" .)
		(end)
	]
	(end)
`

func TestTree(t *testing.T) {
	var tree = &Tree{
		1,
		&Tree{
			2, &Tree{
				3,
				&Tree{
					4, nil, nil,
				},
				nil,
			},
			&Tree{
				5,
				&Tree{
					6, nil, nil,
				},
				nil,
			},
		},
		&Tree{
			7,
			&Tree{
				8,
				&Tree{
					9, nil, nil,
				},
				nil,
			},
			&Tree{
				10,
				&Tree{
					11, nil, nil,
				},
				nil,
			},
		},
	}
	tmpl, err := New("root").Delims("(", ")").Parse(treeTemplate)
	if err != nil {
		t.Fatal("parse error:", err)
	}
	var b bytes.Buffer
	stripSpace := func(r rune) rune {
		if r == '\t' || r == '\n' {
			return -1
		}
		return r
	}
	const expect = "[1[2[3[4]][5[6]]][7[8[9]][10[11]]]]"
	// First by looking up the template.
	err = tmpl.Lookup("tree").Execute(&b, tree)
	if err != nil {
		t.Fatal("exec error:", err)
	}
	result := strings.Map(stripSpace, b.String())
	if result != expect {
		t.Errorf("expected %q got %q", expect, result)
	}
	// Then direct to execution.
	b.Reset()
	err = tmpl.ExecuteTemplate(&b, "tree", tree)
	if err != nil {
		t.Fatal("exec error:", err)
	}
	result = strings.Map(stripSpace, b.String())
	if result != expect {
		t.Errorf("expected %q got %q", expect, result)
	}
}

func TestExecuteOnNewTemplate(t *testing.T) {
	// This is issue 3872.
	_ = New("Name").Templates()
}

const testTemplates = `{{define "one"}}one{{end}}{{define "two"}}two{{end}}`

func TestMessageForExecuteEmpty(t *testing.T) {
	// Test a truly empty template.
	tmpl := New("empty")
	var b bytes.Buffer
	err := tmpl.Execute(&b, 0)
	if err == nil {
		t.Fatal("expected initial error")
	}
	got := err.Error()
	want := `template: empty: "empty" is an incomplete or empty template`
	if got != want {
		t.Errorf("expected error %s got %s", want, got)
	}
	// Add a non-empty template to check that the error is helpful.
	tests, err := New("").Parse(testTemplates)
	if err != nil {
		t.Fatal(err)
	}
	tmpl.AddParseTree("secondary", tests.Tree)
	err = tmpl.Execute(&b, 0)
	if err == nil {
		t.Fatal("expected second error")
	}
	got = err.Error()
	want = `template: empty: "empty" is an incomplete or empty template; defined templates are: "secondary"`
	if got != want {
		t.Errorf("expected error %s got %s", want, got)
	}
	// Make sure we can execute the secondary.
	err = tmpl.ExecuteTemplate(&b, "secondary", 0)
	if err != nil {
		t.Fatal(err)
	}
}

func TestFinalForPrintf(t *testing.T) {
	tmpl, err := New("").Parse(`{{"x" | printf}}`)
	if err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	err = tmpl.Execute(&b, 0)
	if err != nil {
		t.Fatal(err)
	}
}

type cmpTest struct {
	expr  string
	truth string
	ok    bool
}

var cmpTests = []cmpTest{
	{"eq true true", "true", true},
	{"eq true false", "false", true},
	{"eq 1+2i 1+2i", "true", true},
	{"eq 1+2i 1+3i", "false", true},
	{"eq 1.5 1.5", "true", true},
	{"eq 1.5 2.5", "false", true},
	{"eq 1 1", "true", true},
	{"eq 1 2", "false", true},
	{"eq `xy` `xy`", "true", true},
	{"eq `xy` `xyz`", "false", true},
	{"eq .Uthree .Uthree", "true", true},
	{"eq .Uthree .Ufour", "false", true},
	{"eq 3 4 5 6 3", "true", true},
	{"eq 3 4 5 6 7", "false", true},
	{"ne true true", "false", true},
	{"ne true false", "true", true},
	{"ne 1+2i 1+2i", "false", true},
	{"ne 1+2i 1+3i", "true", true},
	{"ne 1.5 1.5", "false", true},
	{"ne 1.5 2.5", "true", true},
	{"ne 1 1", "false", true},
	{"ne 1 2", "true", true},
	{"ne `xy` `xy`", "false", true},
	{"ne `xy` `xyz`", "true", true},
	{"ne .Uthree .Uthree", "false", true},
	{"ne .Uthree .Ufour", "true", true},
	{"lt 1.5 1.5", "false", true},
	{"lt 1.5 2.5", "true", true},
	{"lt 1 1", "false", true},
	{"lt 1 2", "true", true},
	{"lt `xy` `xy`", "false", true},
	{"lt `xy` `xyz`", "true", true},
	{"lt .Uthree .Uthree", "false", true},
	{"lt .Uthree .Ufour", "true", true},
	{"le 1.5 1.5", "true", true},
	{"le 1.5 2.5", "true", true},
	{"le 2.5 1.5", "false", true},
	{"le 1 1", "true", true},
	{"le 1 2", "true", true},
	{"le 2 1", "false", true},
	{"le `xy` `xy`", "true", true},
	{"le `xy` `xyz`", "true", true},
	{"le `xyz` `xy`", "false", true},
	{"le .Uthree .Uthree", "true", true},
	{"le .Uthree .Ufour", "true", true},
	{"le .Ufour .Uthree", "false", true},
	{"gt 1.5 1.5", "false", true},
	{"gt 1.5 2.5", "false", true},
	{"gt 1 1", "false", true},
	{"gt 2 1", "true", true},
	{"gt 1 2", "false", true},
	{"gt `xy` `xy`", "false", true},
	{"gt `xy` `xyz`", "false", true},
	{"gt .Uthree .Uthree", "false", true},
	{"gt .Uthree .Ufour", "false", true},
	{"gt .Ufour .Uthree", "true", true},
	{"ge 1.5 1.5", "true", true},
	{"ge 1.5 2.5", "false", true},
	{"ge 2.5 1.5", "true", true},
	{"ge 1 1", "true", true},
	{"ge 1 2", "false", true},
	{"ge 2 1", "true", true},
	{"ge `xy` `xy`", "true", true},
	{"ge `xy` `xyz`", "false", true},
	{"ge `xyz` `xy`", "true", true},
	{"ge .Uthree .Uthree", "true", true},
	{"ge .Uthree .Ufour", "false", true},
	{"ge .Ufour .Uthree", "true", true},
	// Mixing signed and unsigned integers.
	{"eq .Uthree .Three", "true", true},
	{"eq .Three .Uthree", "true", true},
	{"le .Uthree .Three", "true", true},
	{"le .Three .Uthree", "true", true},
	{"ge .Uthree .Three", "true", true},
	{"ge .Three .Uthree", "true", true},
	{"lt .Uthree .Three", "false", true},
	{"lt .Three .Uthree", "false", true},
	{"gt .Uthree .Three", "false", true},
	{"gt .Three .Uthree", "false", true},
	{"eq .Ufour .Three", "false", true},
	{"lt .Ufour .Three", "false", true},
	{"gt .Ufour .Three", "true", true},
	{"eq .NegOne .Uthree", "false", true},
	{"eq .Uthree .NegOne", "false", true},
	{"ne .NegOne .Uthree", "true", true},
	{"ne .Uthree .NegOne", "true", true},
	{"lt .NegOne .Uthree", "true", true},
	{"lt .Uthree .NegOne", "false", true},
	{"le .NegOne .Uthree", "true", true},
	{"le .Uthree .NegOne", "false", true},
	{"gt .NegOne .Uthree", "false", true},
	{"gt .Uthree .NegOne", "true", true},
	{"ge .NegOne .Uthree", "false", true},
	{"ge .Uthree .NegOne", "true", true},
	{"eq (index `x` 0) 'x'", "true", true}, // The example that triggered this rule.
	{"eq (index `x` 0) 'y'", "false", true},
	// Errors
	{"eq `xy` 1", "", false},    // Different types.
	{"eq 2 2.0", "", false},     // Different types.
	{"lt true true", "", false}, // Unordered types.
	{"lt 1+0i 1+0i", "", false}, // Unordered types.
}

func TestComparison(t *testing.T) {
	b := new(bytes.Buffer)
	var cmpStruct = struct {
		Uthree, Ufour uint
		NegOne, Three int
	}{3, 4, -1, 3}
	for _, test := range cmpTests {
		text := fmt.Sprintf("{{if %s}}true{{else}}false{{end}}", test.expr)
		tmpl, err := New("empty").Parse(text)
		if err != nil {
			t.Fatalf("%q: %s", test.expr, err)
		}
		b.Reset()
		err = tmpl.Execute(b, &cmpStruct)
		if test.ok && err != nil {
			t.Errorf("%s errored incorrectly: %s", test.expr, err)
			continue
		}
		if !test.ok && err == nil {
			t.Errorf("%s did not error", test.expr)
			continue
		}
		if b.String() != test.truth {
			t.Errorf("%s: want %s; got %s", test.expr, test.truth, b.String())
		}
	}
}
