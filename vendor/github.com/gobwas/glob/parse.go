package glob

import (
	"slices"
	"unicode/utf8"

	"github.com/gobwas/glob/internal/debug"
	"github.com/gobwas/glob/syntax"
)

// compile parses the pattern into a matcher tree (see below), simplifies and
// specializes it, and computes the match-time hints and preconditions; see
// [simplify], [specialize], [annotateStars], [needsState], [minLength] and
// [requiredSuffix]. It is what [Compile] wraps.
func compile(str string, sep []rune) (*Pattern, error) {
	if debug.Enabled {
		debug.Printf("compiling %#q\n", str)
	}
	// The matchers keep sep and read it while matching, and the variadic slice
	// may alias an array owned by the caller: give them a copy of their own.
	//
	// The pattern itself keeps the slice as given, to return it from
	// Separators() without cloning.
	var (
		sepCopy = slices.Clone(sep)
		sepStr  = string(sep)
	)

	type operator struct {
		kind  int
		index int
	}
	const (
		opTerms = iota
		opList
	)
	/*
		Stack-based parsing is a technique used to evaluate mathematical
		expressions by leveraging the properties of the LIFO (Last-In,
		First-Out) data structure, the stack. It involves using two stacks: one
		for operands (numbers) and one for operators. By processing the
		expression from left to right and strategically pushing and popping
		elements from the stacks, the expression can be effectively evaluated.

		https://cp-algorithms.com/string/expression_parsing.html

		Here the operands are matchers and the only operators are the braces
		and the commas inside them, so it goes as follows:

		  - a leaf token (text, `?`, `*`, `**`, `[...]`) pushes its matcher
		    onto the stack;

		  - `{` pushes two operators, both remembering the current stack
		    length: opTerms marks where the alternatives of the group will
		    be collected, opList marks where the terms of the current
		    alternative begin;

		  - `,` pops the opList, collapses the terms above its index into a
		    single multiMatcher (or a voidMatcher when there are none, as in
		    `{,a}`), and pushes a fresh opList for the next alternative;

		  - `}` pops the opList and collapses the last alternative the same
		    way, then pops the opTerms and collapses everything above its
		    index -- one matcher per alternative by now -- into an
		    altMatcher;

		  - at the EOF whatever is left on the stack is the top-level
		    sequence; a leftover operator means an unclosed `{`.

		For example, `a{b*,c}d` goes like this (list@i is an opList with
		index i, likewise terms@i):

			token  stack                        operators
			a      "a"
			{      "a"                          terms@1 list@1
			b      "a" "b"                      terms@1 list@1
			*      "a" "b" *                    terms@1 list@1
			,      "a" ["b"·*]                  terms@1 list@2
			c      "a" ["b"·*] "c"              terms@1 list@2
			}      "a" ["b"·*] ["c"]            terms@1
			       "a" {["b"·*]|["c"]}
			d      "a" {["b"·*]|["c"]} "d"
			EOF    ["a"·{["b"·*]|["c"]}·"d"]

		The result is then simplified (["c"] becomes "c") and specialized;
		see [simplify] and [specialize].
	*/
	var (
		stack     []matcher
		operators []operator
	)
	lex := syntax.NewLexer(str)
parsing:
	for {
		token := lex.Next()
		if debug.Enabled {
			debug.Printf("token: %s\n", token)
		}
		switch token.Type {
		case syntax.EOF:
			break parsing

		case syntax.Error:
			return nil, &SyntaxError{
				Offset: lex.Offset(),
				Reason: token.Data,
			}

		case syntax.Single:
			stack = append(stack, &charMatcher{
				Sep: sepCopy,
			})

		case syntax.Text:
			stack = append(stack, &textMatcher{
				Text: token.Data,
			})

		case syntax.RangeOpen:
			m, err := parseRange(lex)
			if err != nil {
				return nil, err
			}
			stack = append(stack, m)

		case syntax.Any:
			stack = append(stack, &starMatcher{
				Sep:    sepCopy,
				SepStr: sepStr,
			})

		case syntax.Super:
			stack = append(stack, &starMatcher{
				Sep: nil,
			})

		case syntax.TermsOpen:
			// Note that the `{` opens both the group and its first
			// alternative: every alternative is delimited by an opList
			// operator. This way TermsClose always collapses the trailing
			// alternative into a single matcher first, even when the group
			// has no commas at all, e.g. `{ab*}`.
			operators = append(operators,
				operator{kind: opTerms, index: len(stack)},
				operator{kind: opList, index: len(stack)},
			)
			if debug.Enabled {
				debug.Printf("terms enter: %d\n", len(stack))
			}

		case syntax.TermSeparator:
			k := len(operators) - 1
			if k < 0 {
				return nil, &SyntaxError{
					Offset: lex.Offset(),
					Reason: "unexpected `,`",
				}
			}
			x := operators[k]
			if x.kind == opList {
				// Remove the most recent "comma" operator.
				// Note that the previous one is the terms operator.
				operators = operators[:k]
			}
			i := x.index
			// Handle the `{,a}` case.
			if i == len(stack) {
				// Empty matchers.
				stack = append(stack, &voidMatcher{})
			} else {
				stack[i] = multiMatcher(slices.Clone(stack[i:]))
				stack = stack[:i+1]
				if debug.Enabled {
					debug.Printf("terms next: %d: %s\n", i, stack[i])
				}
			}
			operators = append(operators, operator{
				kind:  opList,
				index: len(stack),
			})
			if debug.Enabled {
				debug.Printf("terms separator: %d\n", len(stack))
			}

		case syntax.TermsClose:
			for {
				k := len(operators) - 1
				if k < 0 {
					return nil, &SyntaxError{
						Offset: lex.Offset(),
						Reason: "unexpected `}`",
					}
				}
				x := operators[k]
				operators = operators[:k]

				i := x.index
				c := slices.Clone(stack[i:])
				var m matcher
				switch x.kind {
				case opTerms:
					m = altMatcher(c)
				case opList:
					m = multiMatcher(c)
				}
				// Handle the `{a,}` case.
				if i == len(stack) {
					stack = append(stack, m)
				} else {
					stack = stack[:i+1]
					stack[i] = m
				}

				if debug.Enabled {
					debug.Printf(
						"terms leave(%d): %d: %s\n",
						x.kind, i, stack[i],
					)
				}
				if x.kind == opTerms {
					break
				}
			}

		default:
			return nil, &SyntaxError{
				Offset: lex.Offset(),
				Reason: "unexpected token " + token.String(),
			}
		}
	}
	if len(operators) != 0 {
		return nil, &SyntaxError{
			Offset: lex.Offset(),
			Reason: "unclosed `{`",
		}
	}
	m := simplify(multiMatcher(stack))
	m = specialize(m, true)
	annotateStars(m, true)
	if debug.Enabled {
		debug.Printf("compiled %#q: %s\n", str, m)
	}
	p := &Pattern{
		str:   str,
		sep:   sep,
		m:     m,
		state: needsState(m),
	}
	if p.state {
		p.minLen = minLength(m)
		p.suffix = requiredSuffix(m)
	}
	return p, nil
}

// parseRange parses a character class, called right after its opening `[`
// was read; it consumes the tokens up to and including the closing `]`. The
// class is either a range, `[a-c]`, or a set, `[abc]`, either possibly
// negated with a leading `!`; see [runeRangeMatcher] and [runeSetMatcher].
func parseRange(lex *syntax.Lexer) (matcher, error) {
	// -1 marks a range boundary as unset: any decoded rune, including
	// U+0000, is non-negative.
	var (
		not    bool
		lo, hi rune = -1, -1
		chars  map[rune]struct{}
	)
	for {
		token := lex.Next()
		switch token.Type {
		case syntax.EOF:
			return nil, &SyntaxError{
				Offset: lex.Offset(),
				Reason: "unclosed `[`",
			}

		case syntax.Error:
			return nil, &SyntaxError{
				Offset: lex.Offset(),
				Reason: token.Data,
			}

		case syntax.Not:
			not = true

		case syntax.RangeLo:
			r, w := utf8.DecodeRuneInString(token.Data)
			if len(token.Data) > w {
				return nil, &SyntaxError{
					Offset: lex.Offset(),
					Reason: "unexpected length of range lo character",
				}
			}
			lo = r

		case syntax.RangeBetween:
			// The `-` between lo and hi: nothing to do.

		case syntax.RangeHi:
			r, w := utf8.DecodeRuneInString(token.Data)
			if len(token.Data) > w {
				return nil, &SyntaxError{
					Offset: lex.Offset(),
					Reason: "unexpected length of range hi character",
				}
			}
			hi = r

			if hi < lo {
				return nil, &SyntaxError{
					Offset: lex.Offset(),
					Reason: "range hi character is less than lo",
				}
			}

		case syntax.Text:
			chars = make(map[rune]struct{})
			for _, r := range token.Data {
				chars[r] = struct{}{}
			}

		case syntax.RangeClose:
			isRange := lo >= 0 && hi >= 0
			isChars := chars != nil

			if isChars == isRange {
				return nil, &SyntaxError{
					Offset: lex.Offset(),
					Reason: "could not parse range",
				}
			}
			if isRange {
				return &runeRangeMatcher{
					Lo:  lo,
					Hi:  hi,
					Not: not,
				}, nil
			}
			return &runeSetMatcher{
				Set: chars,
				Not: not,
			}, nil
		}
	}
}

// simplify rewrites the freshly parsed tree into its canonical shape, bottom
// up: the sequences are normalized (see [normalizeSequence]), and a sequence
// or a group of alternatives with a single child is replaced by the child,
// with none -- by a void.
func simplify(m matcher) matcher {
	var (
		ms      []matcher
		isMulti bool
	)
	switch v := m.(type) {
	case multiMatcher:
		ms, isMulti = v, true
	case altMatcher:
		ms = v
	default:
		return m
	}
	for i, m := range ms {
		ms[i] = simplify(m)
	}
	if isMulti {
		ms = normalizeSequence(ms)
	}
	switch len(ms) {
	case 0:
		return &voidMatcher{}
	case 1:
		return ms[0]
	}
	if isMulti {
		return multiMatcher(ms)
	}
	return altMatcher(ms)
}

// normalizeSequence rewrites a sequence of (already simplified) matchers
// into a simpler equivalent one:
//
//	["a"·["b"·"c"]·"d"] => ["a"·"b"·"c"·"d"]  inline the nested sequences
//	["a"·void]          => ["a"]              drop the void matchers
//	["a"·"b"]           => ["ab"]             merge the adjacent literals
//	[*·**]              => [**]               coalesce the adjacent stars
//
// Longer literals also make better star jumps; see [annotateStars].
func normalizeSequence(ms []matcher) []matcher {
	if !needsNormalize(ms) {
		// The common case: nothing to rewrite, no copy needed.
		return ms
	}
	out := make([]matcher, 0, len(ms))
	var push func(m matcher)
	push = func(m matcher) {
		switch v := m.(type) {
		case multiMatcher:
			for _, c := range v {
				push(c)
			}
			return
		case *voidMatcher:
			return
		case *textMatcher:
			if len(out) > 0 {
				if prev, ok := out[len(out)-1].(*textMatcher); ok {
					out[len(out)-1] = &textMatcher{Text: prev.Text + v.Text}
					return
				}
			}
		case *starMatcher:
			if len(out) > 0 {
				if prev, ok := out[len(out)-1].(*starMatcher); ok {
					// Adjacent stars are equivalent to the most general
					// of them: the one not limited by separators, if any.
					if len(prev.Sep) > 0 && len(v.Sep) == 0 {
						out[len(out)-1] = v
					}
					return
				}
			}
		}
		out = append(out, m)
	}
	for _, m := range ms {
		push(m)
	}
	return out
}

// needsNormalize reports whether [normalizeSequence] would change ms, so
// that the common case skips the copy.
func needsNormalize(ms []matcher) bool {
	for i, m := range ms {
		switch m.(type) {
		case multiMatcher, *voidMatcher:
			return true
		case *textMatcher:
			if i > 0 {
				if _, ok := ms[i-1].(*textMatcher); ok {
					return true
				}
			}
		case *starMatcher:
			if i > 0 {
				if _, ok := ms[i-1].(*starMatcher); ok {
					return true
				}
			}
		}
	}
	return false
}

// specialize rewrites the terminal sub-sequences of the simplified matcher
// tree into the shaped matchers -- [prefixMatcher], [suffixMatcher],
// [prefixSuffixMatcher] and [containsMatcher]; see [foldTail] for the
// rewrites. The tail flag tells whether nothing follows m in the pattern;
// only there the rewrites apply, since a shaped matcher consumes the whole
// remainder of the input.
func specialize(m matcher, tail bool) matcher {
	switch v := m.(type) {
	case altMatcher:
		// Every alternative ends where the alt ends.
		for i, c := range v {
			v[i] = specialize(c, tail)
		}
		return v

	case multiMatcher:
		for i, c := range v {
			v[i] = specialize(c, tail && i == len(v)-1)
		}
		if !tail {
			return v
		}
		ms := foldTail([]matcher(v))
		if len(ms) == 1 {
			return ms[0]
		}
		return multiMatcher(ms)
	}
	return m
}

// foldTail repeatedly folds the two trailing matchers of the terminal
// sequence ms into a shaped one, while possible:
//
//	[..·"abc"·*]              => [..·prefix("abc")]
//	[..·*·"abc"]              => [..·suffix("abc")]
//	[..·"abc"·prefix("def")]  => [..·prefix("abcdef")]
//	[..·*·prefix("abc")]      => [..·contains("abc")]      (separator-free)
//	[..·"abc"·suffix("def")]  => [..·prefix_suffix("abc","def")]
//	[..·*·contains("abc")]    => [..·contains("abc")]      (separator-free)
func foldTail(ms []matcher) []matcher {
	for len(ms) >= 2 {
		var (
			prev   = ms[len(ms)-2]
			folded matcher
		)
		switch last := ms[len(ms)-1].(type) {
		case *starMatcher:
			if t, ok := prev.(*textMatcher); ok {
				folded = &prefixMatcher{Text: t.Text, Sep: last.SepStr}
			}

		case *textMatcher:
			if star, ok := prev.(*starMatcher); ok {
				folded = &suffixMatcher{Text: last.Text, Sep: star.SepStr}
			}

		case *prefixMatcher:
			switch p := prev.(type) {
			case *textMatcher:
				folded = &prefixMatcher{Text: p.Text + last.Text, Sep: last.Sep}
			case *starMatcher:
				if p.SepStr == "" && last.Sep == "" {
					folded = &containsMatcher{Text: last.Text}
				}
			}

		case *suffixMatcher:
			if t, ok := prev.(*textMatcher); ok {
				folded = &prefixSuffixMatcher{
					Prefix: t.Text,
					Suffix: last.Text,
					Sep:    last.Sep,
				}
			}

		case *containsMatcher:
			if star, ok := prev.(*starMatcher); ok && star.SepStr == "" {
				folded = last
			}
		}
		if folded == nil {
			break
		}
		ms = ms[:len(ms)-1]
		ms[len(ms)-1] = folded
	}
	return ms
}

// annotateStars computes the compile-time hints for the star matchers, in
// order to keep the number of restart points they store at match time low:
//
//   - a star directly followed by a literal jumps between the literal
//     occurrences instead of retrying at every rune (see
//     [starMatcher.storeSkip]);
//
//   - a star with nothing after it anywhere in the pattern (tail is true
//     for m and the star closes it) consumes its whole reach at once and
//     stores no restart points at all.
func annotateStars(m matcher, tail bool) {
	switch v := m.(type) {
	case multiMatcher:
		for i, c := range v {
			last := i == len(v)-1
			star, ok := c.(*starMatcher)
			if !ok {
				annotateStars(c, tail && last)
				continue
			}
			star.Terminal = tail && last
			if !last {
				star.Next = leadingLiteral(v[i+1])
			}
		}
	case altMatcher:
		for _, c := range v {
			annotateStars(c, tail)
		}
	case *starMatcher:
		v.Terminal = tail
	}
}

// leadingLiteral returns the literal the given matcher is guaranteed to
// begin its match with, if any.
func leadingLiteral(m matcher) string {
	switch v := m.(type) {
	case *textMatcher:
		return v.Text
	case *prefixMatcher:
		return v.Text
	case *prefixSuffixMatcher:
		return v.Prefix
	}
	return ""
}

// minLength returns the minimum length in bytes of a string m can match.
func minLength(m matcher) (n int) {
	switch v := m.(type) {
	case *textMatcher:
		return len(v.Text)
	case *charMatcher, *runeRangeMatcher, *runeSetMatcher:
		return 1
	case *prefixMatcher:
		return len(v.Text)
	case *suffixMatcher:
		return len(v.Text)
	case *prefixSuffixMatcher:
		return len(v.Prefix) + len(v.Suffix)
	case *containsMatcher:
		return len(v.Text)
	case multiMatcher:
		for _, c := range v {
			n += minLength(c)
		}
		return n
	case altMatcher:
		n = minLength(v[0])
		for _, c := range v[1:] {
			n = min(n, minLength(c))
		}
		return n
	}
	return 0 // A star or a void.
}

// requiredSuffix returns the literal every string m matches must end with.
func requiredSuffix(m matcher) string {
	switch v := m.(type) {
	case *textMatcher:
		return v.Text
	case *suffixMatcher:
		return v.Text
	case *prefixSuffixMatcher:
		return v.Suffix
	case multiMatcher:
		return requiredSuffix(v[len(v)-1])
	case altMatcher:
		s := requiredSuffix(v[0])
		for _, c := range v[1:] {
			s = commonSuffix(s, requiredSuffix(c))
			if s == "" {
				break
			}
		}
		return s
	}
	return "" // A star, a single-character matcher or a void.
}

// commonSuffix returns the longest common suffix of a and b, never splitting
// a multi-byte rune.
func commonSuffix(a, b string) string {
	i := 0
	for i < len(a) && i < len(b) {
		ra, wa := utf8.DecodeLastRuneInString(a[:len(a)-i])
		rb, wb := utf8.DecodeLastRuneInString(b[:len(b)-i])
		if ra != rb || wa != wb {
			break
		}
		i += wa
	}
	return a[len(a)-i:]
}

// needsState reports whether matching m may save a checkpoint. Only the
// alts and the non-terminal stars do; a pattern without them is matched
// with a plain call chain -- see [Pattern.Match].
func needsState(m matcher) bool {
	switch v := m.(type) {
	case altMatcher:
		return true
	case multiMatcher:
		return slices.ContainsFunc(v, needsState)
	case *starMatcher:
		return !v.Terminal
	}
	return false
}
