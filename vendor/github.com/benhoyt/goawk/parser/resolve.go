// Resolve function calls and variable types

package parser

import (
	"fmt"
	"reflect"
	"sort"

	. "github.com/benhoyt/goawk/internal/ast"
	. "github.com/benhoyt/goawk/lexer"
)

type varType int

const (
	typeUnknown varType = iota
	typeScalar
	typeArray
)

func (t varType) String() string {
	switch t {
	case typeScalar:
		return "Scalar"
	case typeArray:
		return "Array"
	default:
		return "Unknown"
	}
}

// typeInfo records type information for a single variable
type typeInfo struct {
	typ      varType
	ref      *VarExpr
	scope    VarScope
	index    int
	callName string
	argIndex int
}

// Used by printVarTypes when debugTypes is turned on
func (t typeInfo) String() string {
	var scope string
	switch t.scope {
	case ScopeGlobal:
		scope = "Global"
	case ScopeLocal:
		scope = "Local"
	default:
		scope = "Special"
	}
	return fmt.Sprintf("typ=%s ref=%p scope=%s index=%d callName=%q argIndex=%d",
		t.typ, t.ref, scope, t.index, t.callName, t.argIndex)
}

// A single variable reference (normally scalar)
type varRef struct {
	funcName string
	ref      *VarExpr
	isArg    bool
	pos      Position
}

// A single array reference
type arrayRef struct {
	funcName string
	ref      *ArrayExpr
	pos      Position
}

// Initialize the resolver
func (p *parser) initResolve() {
	p.varTypes = make(map[string]map[string]typeInfo)
	p.varTypes[""] = make(map[string]typeInfo) // globals
	p.functions = make(map[string]int)
	p.arrayRef("ARGV", Position{1, 1}) // interpreter relies on ARGV being present
	p.multiExprs = make(map[*MultiExpr]Position, 3)
}

// Signal the start of a function
func (p *parser) startFunction(name string, params []string) {
	p.funcName = name
	p.varTypes[name] = make(map[string]typeInfo)
}

// Signal the end of a function
func (p *parser) stopFunction() {
	p.funcName = ""
}

// Add function by name with given index
func (p *parser) addFunction(name string, index int) {
	p.functions[name] = index
}

// Records a call to a user function (for resolving indexes later)
type userCall struct {
	call   *UserCallExpr
	pos    Position
	inFunc string
}

// Record a user call site
func (p *parser) recordUserCall(call *UserCallExpr, pos Position) {
	p.userCalls = append(p.userCalls, userCall{call, pos, p.funcName})
}

// After parsing, resolve all user calls to their indexes. Also
// ensures functions called have actually been defined, and that
// they're not being called with too many arguments.
func (p *parser) resolveUserCalls(prog *Program) {
	// Number the native funcs (order by name to get consistent order)
	nativeNames := make([]string, 0, len(p.nativeFuncs))
	for name := range p.nativeFuncs {
		nativeNames = append(nativeNames, name)
	}
	sort.Strings(nativeNames)
	nativeIndexes := make(map[string]int, len(nativeNames))
	for i, name := range nativeNames {
		nativeIndexes[name] = i
	}

	for _, c := range p.userCalls {
		// AWK-defined functions take precedence over native Go funcs
		index, ok := p.functions[c.call.Name]
		if !ok {
			f, haveNative := p.nativeFuncs[c.call.Name]
			if !haveNative {
				panic(&ParseError{c.pos, fmt.Sprintf("undefined function %q", c.call.Name)})
			}
			typ := reflect.TypeOf(f)
			if !typ.IsVariadic() && len(c.call.Args) > typ.NumIn() {
				panic(&ParseError{c.pos, fmt.Sprintf("%q called with more arguments than declared", c.call.Name)})
			}
			c.call.Native = true
			c.call.Index = nativeIndexes[c.call.Name]
			continue
		}
		function := prog.Functions[index]
		if len(c.call.Args) > len(function.Params) {
			panic(&ParseError{c.pos, fmt.Sprintf("%q called with more arguments than declared", c.call.Name)})
		}
		c.call.Index = index
	}
}

// For arguments that are variable references, we don't know the
// type based on context, so mark the types for these as unknown.
func (p *parser) processUserCallArg(funcName string, arg Expr, index int) {
	if varExpr, ok := arg.(*VarExpr); ok {
		scope, varFuncName := p.getScope(varExpr.Name)
		ref := p.varTypes[varFuncName][varExpr.Name].ref
		if ref == varExpr {
			// Only applies if this is the first reference to this
			// variable (otherwise we know the type already)
			p.varTypes[varFuncName][varExpr.Name] = typeInfo{typeUnknown, ref, scope, 0, funcName, index}
		}
		// Mark the last related varRef (the most recent one) as a
		// call argument for later error handling
		p.varRefs[len(p.varRefs)-1].isArg = true
	}
}

// Determine scope of given variable reference (and funcName if it's
// a local, otherwise empty string)
func (p *parser) getScope(name string) (VarScope, string) {
	switch {
	case p.locals[name]:
		return ScopeLocal, p.funcName
	case SpecialVarIndex(name) > 0:
		return ScopeSpecial, ""
	default:
		return ScopeGlobal, ""
	}
}

// Record a variable (scalar) reference and return the *VarExpr (but
// VarExpr.Index won't be set till later)
func (p *parser) varRef(name string, pos Position) *VarExpr {
	scope, funcName := p.getScope(name)
	expr := &VarExpr{scope, 0, name}
	p.varRefs = append(p.varRefs, varRef{funcName, expr, false, pos})
	info := p.varTypes[funcName][name]
	if info.typ == typeUnknown {
		p.varTypes[funcName][name] = typeInfo{typeScalar, expr, scope, 0, info.callName, 0}
	}
	return expr
}

// Record an array reference and return the *ArrayExpr (but
// ArrayExpr.Index won't be set till later)
func (p *parser) arrayRef(name string, pos Position) *ArrayExpr {
	scope, funcName := p.getScope(name)
	if scope == ScopeSpecial {
		panic(p.error("can't use scalar %q as array", name))
	}
	expr := &ArrayExpr{scope, 0, name}
	p.arrayRefs = append(p.arrayRefs, arrayRef{funcName, expr, pos})
	info := p.varTypes[funcName][name]
	if info.typ == typeUnknown {
		p.varTypes[funcName][name] = typeInfo{typeArray, nil, scope, 0, info.callName, 0}
	}
	return expr
}

// Print variable type information (for debugging) on p.debugWriter
func (p *parser) printVarTypes(prog *Program) {
	fmt.Fprintf(p.debugWriter, "scalars: %v\n", prog.Scalars)
	fmt.Fprintf(p.debugWriter, "arrays: %v\n", prog.Arrays)
	funcNames := []string{}
	for funcName := range p.varTypes {
		funcNames = append(funcNames, funcName)
	}
	sort.Strings(funcNames)
	for _, funcName := range funcNames {
		if funcName != "" {
			fmt.Fprintf(p.debugWriter, "function %s\n", funcName)
		} else {
			fmt.Fprintf(p.debugWriter, "globals\n")
		}
		varNames := []string{}
		for name := range p.varTypes[funcName] {
			varNames = append(varNames, name)
		}
		sort.Strings(varNames)
		for _, name := range varNames {
			info := p.varTypes[funcName][name]
			fmt.Fprintf(p.debugWriter, "  %s: %s\n", name, info)
		}
	}
}

// If we can't finish resolving after this many iterations, give up
const maxResolveIterations = 10000

// Resolve unknown variables types and generate variable indexes and
// name-to-index mappings for interpreter
func (p *parser) resolveVars(prog *Program) {
	// First go through all unknown types and try to determine the
	// type from the parameter type in that function definition. May
	// need multiple passes depending on the order of functions. This
	// is not particularly efficient, but on realistic programs it's
	// not an issue.
	for i := 0; ; i++ {
		progressed := false
		for funcName, infos := range p.varTypes {
			for name, info := range infos {
				if info.scope == ScopeSpecial || info.typ != typeUnknown {
					// It's a special var or type is already known
					continue
				}
				funcIndex, ok := p.functions[info.callName]
				if !ok {
					// Function being called is a native function
					continue
				}
				// Determine var type based on type of this parameter
				// in the called function (if we know that)
				paramName := prog.Functions[funcIndex].Params[info.argIndex]
				typ := p.varTypes[info.callName][paramName].typ
				if typ != typeUnknown {
					if p.debugTypes {
						fmt.Fprintf(p.debugWriter, "resolving %s:%s to %s\n",
							funcName, name, typ)
					}
					info.typ = typ
					p.varTypes[funcName][name] = info
					progressed = true
				}
			}
		}
		if !progressed {
			// If we didn't progress we're done (or trying again is
			// not going to help)
			break
		}
		if i >= maxResolveIterations {
			panic(p.error("too many iterations trying to resolve variable types"))
		}
	}

	// Resolve global variables (iteration order is undefined, so
	// assign indexes basically randomly)
	prog.Scalars = make(map[string]int)
	prog.Arrays = make(map[string]int)
	for name, info := range p.varTypes[""] {
		_, isFunc := p.functions[name]
		if isFunc {
			// Global var can't also be the name of a function
			panic(p.error("global var %q can't also be a function", name))
		}
		var index int
		if info.scope == ScopeSpecial {
			index = SpecialVarIndex(name)
		} else if info.typ == typeArray {
			index = len(prog.Arrays)
			prog.Arrays[name] = index
		} else {
			index = len(prog.Scalars)
			prog.Scalars[name] = index
		}
		info.index = index
		p.varTypes[""][name] = info
	}

	// Resolve local variables (assign indexes in order of params).
	// Also patch up Function.Arrays (tells interpreter which args
	// are arrays).
	for funcName, infos := range p.varTypes {
		if funcName == "" {
			continue
		}
		scalarIndex := 0
		arrayIndex := 0
		functionIndex := p.functions[funcName]
		function := prog.Functions[functionIndex]
		arrays := make([]bool, len(function.Params))
		for i, name := range function.Params {
			info := infos[name]
			var index int
			if info.typ == typeArray {
				index = arrayIndex
				arrayIndex++
				arrays[i] = true
			} else {
				// typeScalar or typeUnknown: variables may still be
				// of unknown type if they've never been referenced --
				// default to scalar in that case
				index = scalarIndex
				scalarIndex++
			}
			info.index = index
			p.varTypes[funcName][name] = info
		}
		prog.Functions[functionIndex].Arrays = arrays
	}

	// Check that variables passed to functions are the correct type
	for _, c := range p.userCalls {
		// Check native function calls
		if c.call.Native {
			for _, arg := range c.call.Args {
				varExpr, ok := arg.(*VarExpr)
				if !ok {
					// Non-variable expression, must be scalar
					continue
				}
				funcName := p.getVarFuncName(prog, varExpr.Name, c.inFunc)
				info := p.varTypes[funcName][varExpr.Name]
				if info.typ == typeArray {
					message := fmt.Sprintf("can't pass array %q to native function", varExpr.Name)
					panic(&ParseError{c.pos, message})
				}
			}
			continue
		}

		// Check AWK function calls
		function := prog.Functions[c.call.Index]
		for i, arg := range c.call.Args {
			varExpr, ok := arg.(*VarExpr)
			if !ok {
				if function.Arrays[i] {
					message := fmt.Sprintf("can't pass scalar %s as array param", arg)
					panic(&ParseError{c.pos, message})
				}
				continue
			}
			funcName := p.getVarFuncName(prog, varExpr.Name, c.inFunc)
			info := p.varTypes[funcName][varExpr.Name]
			if info.typ == typeArray && !function.Arrays[i] {
				message := fmt.Sprintf("can't pass array %q as scalar param", varExpr.Name)
				panic(&ParseError{c.pos, message})
			}
			if info.typ != typeArray && function.Arrays[i] {
				message := fmt.Sprintf("can't pass scalar %q as array param", varExpr.Name)
				panic(&ParseError{c.pos, message})
			}
		}
	}

	if p.debugTypes {
		p.printVarTypes(prog)
	}

	// Patch up variable indexes (interpreter uses an index instead
	// of name for more efficient lookups)
	for _, varRef := range p.varRefs {
		info := p.varTypes[varRef.funcName][varRef.ref.Name]
		if info.typ == typeArray && !varRef.isArg {
			message := fmt.Sprintf("can't use array %q as scalar", varRef.ref.Name)
			panic(&ParseError{varRef.pos, message})
		}
		varRef.ref.Index = info.index
	}
	for _, arrayRef := range p.arrayRefs {
		info := p.varTypes[arrayRef.funcName][arrayRef.ref.Name]
		if info.typ == typeScalar {
			message := fmt.Sprintf("can't use scalar %q as array", arrayRef.ref.Name)
			panic(&ParseError{arrayRef.pos, message})
		}
		arrayRef.ref.Index = info.index
	}
}

// If name refers to a local (in function inFunc), return that
// function's name, otherwise return "" (meaning global).
func (p *parser) getVarFuncName(prog *Program, name, inFunc string) string {
	if inFunc == "" {
		return ""
	}
	for _, param := range prog.Functions[p.functions[inFunc]].Params {
		if name == param {
			return inFunc
		}
	}
	return ""
}

// Record a "multi expression" (comma-separated pseudo-expression
// used to allow commas around print/printf arguments).
func (p *parser) multiExpr(exprs []Expr, pos Position) Expr {
	expr := &MultiExpr{exprs}
	p.multiExprs[expr] = pos
	return expr
}

// Mark the multi expression as used (by a print/printf statement).
func (p *parser) useMultiExpr(expr *MultiExpr) {
	delete(p.multiExprs, expr)
}

// Check that there are no unused multi expressions (syntax error).
func (p *parser) checkMultiExprs() {
	if len(p.multiExprs) == 0 {
		return
	}
	// Show error on first comma-separated expression
	min := Position{1000000000, 1000000000}
	for _, pos := range p.multiExprs {
		if pos.Line < min.Line || (pos.Line == min.Line && pos.Column < min.Column) {
			min = pos
		}
	}
	message := fmt.Sprintf("unexpected comma-separated expression")
	panic(&ParseError{min, message})
}
