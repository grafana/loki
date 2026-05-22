package log

import "github.com/grafana/regexp/syntax"

// isCaseInsensitive reports whether the regexp has the FoldCase flag set.
// Copied from pkg/util to avoid pkg/util's transitive server-side dependencies.
func isCaseInsensitive(reg *syntax.Regexp) bool {
	return (reg.Flags & syntax.FoldCase) != 0
}

// allNonGreedy turns greedy quantifiers such as `.*` and `.+` into non-greedy ones.
// Copied from pkg/util to avoid pkg/util's transitive server-side dependencies.
func allNonGreedy(regs ...*syntax.Regexp) {
	clearCapture(regs...)
	for _, re := range regs {
		switch re.Op {
		case syntax.OpCapture, syntax.OpConcat, syntax.OpAlternate:
			allNonGreedy(re.Sub...)
		case syntax.OpStar, syntax.OpPlus:
			re.Flags = re.Flags | syntax.NonGreedy
		default:
			continue
		}
	}
}

// clearCapture removes capture operations as they are not used for filtering.
// Copied from pkg/util to avoid pkg/util's transitive server-side dependencies.
func clearCapture(regs ...*syntax.Regexp) {
	for _, r := range regs {
		if r.Op == syntax.OpCapture {
			*r = *r.Sub[0]
		}
	}
}
