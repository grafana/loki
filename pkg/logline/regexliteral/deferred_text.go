package regexliteral

import (
	"slices"
	"strings"
)

// deferredText is an immutable rope. nil is the empty string. Length and a
// compositional fingerprint make concatenation independent of prefix length.
// Fingerprints only reject unequal text; equality is always verified.
type deferredText struct {
	left, right *deferredText
	leaf        string
	bytes       int
	hash, power uint64
}

const textHashBase = 1099511628211

func textLeaf(s string) *deferredText {
	if s == "" {
		return nil
	}
	t := &deferredText{leaf: s, bytes: len(s), power: 1}
	for i := range len(s) {
		t.hash = t.hash*textHashBase + uint64(s[i]) + 1
		t.power *= textHashBase
	}
	return t
}

func textBytes(t *deferredText) int {
	if t == nil {
		return 0
	}
	return t.bytes
}

func concatText(a, b *deferredText) *deferredText {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	return &deferredText{
		left: a, right: b,
		bytes: a.bytes + b.bytes,
		hash:  a.hash*b.power + b.hash,
		power: a.power * b.power,
	}
}

// repeatText shares doubled subtrees rather than allocating a node per copy.
// Its caller has already applied the existing exact-repeat limit.
func repeatText(t *deferredText, n int) *deferredText {
	var out *deferredText
	for n > 0 {
		if n&1 != 0 {
			out = concatText(out, t)
		}
		n >>= 1
		if n > 0 {
			t = concatText(t, t)
		}
	}
	return out
}

func materializeText(t *deferredText) string {
	if t == nil {
		return ""
	}
	if t.left == nil {
		return t.leaf
	}
	var out strings.Builder
	out.Grow(t.bytes)
	stack := []*deferredText{t}
	for len(stack) > 0 {
		i := len(stack) - 1
		x := stack[i]
		stack = stack[:i]
		if x.left == nil {
			out.WriteString(x.leaf)
		} else {
			stack = append(stack, x.right, x.left)
		}
	}
	return out.String()
}

// compareText compares bytes, just like strings.Compare. Pending identical
// subtrees are skipped without visiting their leaves. Offsets are needed only
// for leaves when equal text has been split at different boundaries.
func compareText(a, b *deferredText) int {
	type part struct {
		text   *deferredText
		offset int
	}
	var left, right []part
	if a != nil {
		left = append(left, part{text: a})
	}
	if b != nil {
		right = append(right, part{text: b})
	}
	for len(left) > 0 && len(right) > 0 {
		i, j := len(left)-1, len(right)-1
		x, y := left[i], right[j]
		if x == y {
			left, right = left[:i], right[:j]
			continue
		}
		// Expand the larger pending subtree first, so shared prefixes with
		// different surrounding concatenation shapes can line up and skip.
		if x.text.left != nil && (y.text.left == nil || x.text.bytes >= y.text.bytes) {
			left = append(left[:i], part{text: x.text.right}, part{text: x.text.left})
			continue
		}
		if y.text.left != nil {
			right = append(right[:j], part{text: y.text.right}, part{text: y.text.left})
			continue
		}
		xs, ys := x.text.leaf[x.offset:], y.text.leaf[y.offset:]
		n := min(len(xs), len(ys))
		if order := strings.Compare(xs[:n], ys[:n]); order != 0 {
			return order
		}
		if n == len(xs) {
			left = left[:i]
		} else {
			left[i].offset += n
		}
		if n == len(ys) {
			right = right[:j]
		} else {
			right[j].offset += n
		}
	}
	switch {
	case len(left) > 0:
		return 1
	case len(right) > 0:
		return -1
	default:
		return 0
	}
}

// textSet is deduplicated but not sorted: sorting after every cross would
// repeatedly compare growing shared prefixes. Only repeat accounting needs
// the lexicographically first member, not the shortest member.
type textSet []*deferredText

func (set textSet) firstBytes() int {
	first := set[0]
	for _, t := range set[1:] {
		if compareText(t, first) < 0 {
			first = t
		}
	}
	return textBytes(first)
}

func (set textSet) with(t *deferredText) textSet {
	for _, existing := range set {
		if existing == t {
			return set
		}
		if existing != nil && t != nil && existing.bytes == t.bytes && existing.hash == t.hash && compareText(existing, t) == 0 {
			return set
		}
	}
	return append(set, t)
}

func unionText(a, b textSet) textSet {
	out := slices.Clone(a)
	for _, t := range b {
		out = out.with(t)
	}
	return out
}

// crossText preserves the existing product-before-dedup enumeration policy;
// the caller checks the product size before calling it.
func crossText(a, b textSet) textSet {
	out := make(textSet, 0, len(a)*len(b))
	for _, x := range a {
		for _, y := range b {
			out = out.with(concatText(x, y))
		}
	}
	return out
}
