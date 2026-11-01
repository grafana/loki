package glob

import (
	"fmt"

	"github.com/gobwas/glob/internal/debug"
	"github.com/gobwas/glob/syntax"
)

// SyntaxError is returned by [Compile] when the given pattern can not be
// parsed. Offset points at the place in the pattern the error was detected
// at, so the tooling can do things like:
//
//	{a,b
//	----^ unclosed `{`
type SyntaxError struct {
	// Offset is a byte offset in the pattern.
	Offset int
	// Reason describes the error.
	Reason string
}

func (s *SyntaxError) Error() string {
	return fmt.Sprintf("glob: syntax error at %d: %s", s.Offset, s.Reason)
}

// Pattern represents a compiled glob pattern.
//
// A pattern is compiled into a tree of matchers:
//
//	`a`      => "a"
//	`a*`     => ["a"·*]
//	`{a*,b}` => {["a"·*]|"b"}
//
// Matching is a backtracking walk over that tree; see [Pattern.Match].
type Pattern struct {
	// str is the pattern text the Pattern was compiled from; see
	// [Pattern.String].
	str string

	// sep are the separators the Pattern was compiled with; see
	// [Pattern.Separators].
	sep []rune

	// globstar tells whether the Pattern was compiled in the globstar mode;
	// see [Compiler.Globstar].
	globstar bool

	// m is the root of the matcher tree; see [matcher].
	m matcher

	// state tells whether matching m needs the backtracking state, that
	// is, whether it may save checkpoints; see [needsState]. A pattern
	// without them is matched with a plain call chain.
	state bool

	// The match preconditions: every matching string is at least minLen
	// bytes and ends with suffix. They fail the obvious mismatches in O(1)
	// instead of a backtracking walk -- e.g. `a*a*a*b` requires the
	// trailing `b`, no matter how the stars go.
	//
	// A precondition pays off only when it catches a mismatch earlier than
	// the walk would, which is why:
	//
	//   - there is no required prefix: the walk is left-to-right, so a
	//     leading literal is the first thing checked anyway, while a bad
	//     suffix or length is discovered last, after the whole
	//     backtracking exploration;
	//
	//   - they are computed for the stateful patterns only: a stateless
	//     pattern is a plain call chain whose matchers perform these very
	//     checks themselves (e.g. suffixMatcher is a HasSuffix), so the
	//     precondition would only duplicate them.
	minLen int
	suffix string
}

// String returns the source text used to compile the pattern, the same way
// [regexp.Regexp.String] does.
//
// Note that separators are not part of String: they are given to Compile
// alongside the pattern text.
func (p *Pattern) String() string {
	return p.str
}

// Separators returns the separators the pattern was compiled with, in the
// order they were given to Compile; nil when there are none.
//
// The returned slice is the very one given to Compile, sharing its backing
// array: it is not copied on the way in or out. Matching does not use it.
func (p *Pattern) Separators() []rune {
	return p.sep
}

// Globstar reports whether the pattern was compiled with [Compiler.Globstar]
// set. Like the separators, it is not part of String.
func (p *Pattern) Globstar() bool {
	return p.globstar
}

func init() {
	// The matcher tree is unexported; hand its rendering to the in-module
	// tooling (cmd/globtest -v) without widening the public API.
	debug.Tree = func(p any) string {
		return p.(*Pattern).m.String()
	}
}

// Compile compiles the glob pattern. The separators, if given, are the
// characters `*` and `?` do not match (`**` does); they can not be changed
// after the compilation, see [Pattern.Separators]. A malformed pattern is
// reported with a [*SyntaxError]. Compile is a shorthand for
// [Compiler.Compile] with the Separators set and no other options, the way
// [net.Dial] is for [net.Dialer.Dial].
//
// The pattern syntax is:
//
//	pattern:
//	    { term }
//
//	term:
//	    `*`         matches any sequence of non-separator characters
//	    `**`        matches any sequence of characters
//	    `?`         matches any single non-separator character
//	    `[` [ `!` ] class `]`
//	                character class; `!` negates it
//	    `{` pattern-list `}`
//	                pattern alternatives
//	    c           matches character c (c != `*`, `**`, `?`, `\`, `[`, `{`, `}`)
//	    `\` c       matches character c
//
//	class:
//	    lo `-` hi   matches character c for lo <= c <= hi
//	    { c }       matches any of the listed characters (c != `\`, `]`;
//	                `\` c matches c, `-` is literal here); must be non-empty
//
//	pattern-list:
//	    pattern { `,` pattern }
//	                comma-separated (without spaces) patterns
func Compile(pattern string, separators ...rune) (*Pattern, error) {
	c := Compiler{
		Separators: separators,
	}
	return c.Compile(pattern)
}

// MustCompile is the same as Compile, except that if Compile returns error,
// this will panic.
func MustCompile(pattern string, separators ...rune) *Pattern {
	g, err := Compile(pattern, separators...)
	if err != nil {
		panic(err)
	}
	return g
}

// Compiler holds the options a pattern is compiled with; see
// [Compiler.Compile].
//
// The zero value compiles patterns the way [Compile] does with no separators.
type Compiler struct {
	// Separators are the characters `*` and `?` do not match; see [Compile].
	Separators []rune

	// Globstar makes `**/` match zero or more whole segments, as in bash with
	// `shopt -s globstar` or in gitignore: `**/x` matches `x` as well as
	// `a/b/x`, and `a/**/b` matches `a/b`.
	//
	// With no globstar, `**/x` requires the `/`, // and `{**/,}x` is the way
	// to spell this.
	//
	// Only a `**` that is a whole segment and is followed by a separator is
	// affected; `a**/b` and a trailing `a/**` keep their meaning.
	//
	// It has no effect without Separators, which define the segments.
	Globstar bool
}

// QuoteMeta returns a string that escapes all glob meta characters inside s;
// the returned string is a glob pattern matching the literal text.
//
// Characters that are special only in a particular context, such as `,` inside
// `{...}` or `-` and `!` inside `[...]`, are not escaped.
func QuoteMeta(s string) string {
	// 2 is a pessimistic way of allocating an extra byte per each byte in s.
	b := make([]byte, 2*len(s))
	j := 0
	// A byte loop is correct here because all meta characters are ASCII.
	for i := range len(s) {
		if syntax.IsSpecial(s[i]) {
			b[j] = '\\'
			j++
		}
		b[j] = s[i]
		j++
	}
	return string(b[0:j])
}
