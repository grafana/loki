package glob

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"
	"unsafe"

	"github.com/gobwas/glob/internal/debug"
)

// Match reports whether s matches the pattern.
func (p *Pattern) Match(s string) bool {
	var x matchContext
	if p.state {
		if len(s) < p.minLen || !strings.HasSuffix(s, p.suffix) {
			return false
		}
		state := acquireState()
		defer releaseState(state)
		x.state = state
	}
	for {
		n, match := p.m.Match(x, s[x.offset:])
		// Note: debug.Enabled is a build-tag constant; when it is false the
		// whole block (including the argument evaluation) is compiled away.
		if debug.Enabled && x.state != nil {
			debug.Printf("stack: %s\n", formatStack(x.state.stack))
			debug.Printf("stars: %s\n", formatStack(x.state.stars))
		}
		if match && n == len(s[x.offset:]) {
			if debug.Enabled {
				debug.Printf("match!\n")
			}
			return true
		}
		if x.state == nil {
			// The pattern never saves checkpoints (see [needsState]):
			// nothing to backtrack to.
			return false
		}
		var (
			c checkpoint
			k checkpointKind
		)
		switch {
		case len(x.state.stars) > 0:
			if debug.Enabled {
				debug.Printf("has star\n")
			}
			c = popLast(&x.state.stars)
			k = checkpointStars

		case len(x.state.stack) > 0:
			if debug.Enabled {
				debug.Printf("has stack\n")
			}
			c = popLast(&x.state.stack)
			k = checkpointStack

		default:
			if debug.Enabled {
				debug.Printf("no match\n")
			}
			return false
		}
		x.offset = c.offset
		x.kind = k
		x.frame = frame{
			path:  c.path,
			depth: 0,
		}
	}
}

// matcher is a node of the tree a pattern compiles into.
//
// Match matches the beginning of s and reports how many bytes it consumed;
// the caller goes on with the rest. A matcher may consume nothing and still
// match: a void does, and so does a non-terminal star -- it stores its
// restart points instead and lets the walk continue, to be resumed from one
// of them on a mismatch later; see [Pattern.Match]. The context tells where
// in the input and in the tree the matcher is; see [matchContext].
//
// String renders the node for the debug output and cmd/globtest -v; the
// notation is described at [Pattern].
type matcher interface {
	Match(matchContext, string) (n int, matched bool)
	String() string
}

// frame tells where in the matcher tree the walk currently is: the path from
// the root down to the current node, and the depth of the node.
//
// A checkpoint stores the frame's path; resuming it re-enters the tree from
// the root and follows the path back to the very node that saved it (an
// alternative to try, or a star to restart) -- see [Pattern.Match],
// [multiMatcher.Match] and [altMatcher.Match].
//
// For `{a*b,c}y`, compiled into [{["a"·*·"b"]|"c"}·"y"], the frame at each
// node the walk visits is:
//
//	node                     path      depth
//	[{["a"·*·"b"]|"c"}·"y"]  []        0      the root
//	{["a"·*·"b"]|"c"}        [0]*      1
//	["a"·*·"b"]              [0 0]*    2
//	"a"                      [0 0 0]*  3
//	*                        [0 0 1]   3
//	"b"                      [0 0 2]   3
//	"c"                      [0 1]     2      reached only by resuming the
//	                                          checkpoint the alt saved
//	"y"                      [1]       1
//
//	* the zeros are virtual: turning to child #0 records nothing, so the
//	  path actually stored is [] -- see path below.
type frame struct {
	// path addresses the current node: path[d] is the index of the child taken
	// at depth d, that is, in the multiMatcher or altMatcher d levels below
	// the root.
	//
	// For `{a*b,c}y`, compiled into [{["a"·*·"b"]|"c"}·"y"]:
	//
	//	[0 1]    the "c" alternative -- what the alt checkpoints when it
	//	         enters ["a"·*·"b"], to be tried on a mismatch
	//
	//	[0 0 1]  the star: child #1 of ["a"·*·"b"], which is alternative #0
	//	         of the alt, which is child #0 of the root sequence -- what
	//	         the star's restart points carry
	//
	// It is recorded lazily: an index is stored only when the walk turns to a
	// child other than #0 (see [matchContext.branch]), so the path may be
	// shorter than the current depth -- the missing trailing entries are
	// implicitly zero, and [frame.index] reads them as such. In the example
	// above, at "a" the path is still empty, not [0 0 0]: the alt, alternative
	// #0 and "a" were all entered as child #0.
	path []int

	// depth is the depth of the current node (the one [frame.path] leads to):
	// 0 at the root, 1 at its children, and so on; [matchContext.next]
	// increments it on every descent. It is also where the node's own entry in
	// path lives: path[depth] is the child to take next -- [frame.index] reads
	// it, [matchContext.branch] writes it.
	//
	// It is not len(path): the path is recorded lazily, so it may fall short
	// of the depth, and, when resuming a checkpoint, it is the whole path of
	// the checkpoint, reaching beyond the depth all the way down to the node
	// to resume at.
	//
	// To say it the other way, depth is what len(path) would be were the path
	// always recorded in full -- the virtual zeros included -- and cut at the
	// current node: the length of the node's full address in the tree.
	//
	// For `{a*b,c}y`, compiled into [{["a"·*·"b"]|"c"}·"y"], the nodes at
	// each depth are:
	//
	//	0  [{["a"·*·"b"]|"c"}·"y"]   the root sequence
	//	1  {["a"·*·"b"]|"c"}, "y"    its children
	//	2  ["a"·*·"b"], "c"          the alternatives
	//	3  "a", *, "b"               the children of ["a"·*·"b"]
	depth int
}

// index returns the index of the child to take at the current node: the one
// the path leads to when resuming a checkpoint, #0 otherwise.
func (v frame) index() int {
	if v.depth >= len(v.path) {
		return 0
	}
	return v.path[v.depth]
}

// checkpoint is a place to resume the walk from on a mismatch.
type checkpoint struct {
	// offset is the position in the input to resume at.
	offset int

	// path leads to the node that saved the checkpoint; see [frame].
	//
	// Unlike a live frame's, it is always at full length -- the virtual zeros
	// written out -- so len(path) is the depth of that node, and a checkpoint
	// needs no depth of its own: resuming starts at the root and tells it has
	// arrived by comparing the walk's depth against len(path); see
	// [matchContext.branch], [matchContext.storeStar] and [altMatcher.Match].
	path []int
}

// checkpointKind tells which pile a checkpoint was taken from during the
// backtracking in [Pattern.Match].
type checkpointKind int

const (
	// checkpointStack is an alternative checkpoint saved by altMatcher.
	checkpointStack checkpointKind = iota
	// checkpointStars is a star restart point saved by starMatcher.
	checkpointStars
)

// matchContext is what a matcher is called with: where in the input and in
// the tree it is, plus the backtracking state shared by the whole walk. It
// is passed by value, so a matcher's changes to it are seen by its
// descendants only.
type matchContext struct {
	// offset is the position in the whole input the current node matches from.
	// The matchers see only the remainder of the input, so it is what a
	// checkpoint records to resume at the same place; see [checkpoint].
	offset int

	// frame is where in the matcher tree the current node is; see [frame].
	frame frame

	// state holds the checkpoint piles and the path arena shared by the
	// whole walk.
	state *matchState

	// kind tells which pile the checkpoint being resumed was taken from.
	// See [altMatcher.Match] for its use.
	kind checkpointKind

	// starsFloor is the number of star restart points that existed when
	// the walk entered the current alternative. The entries below it were
	// born outside of the alternative and must not be discarded by the
	// stars inside it; see [matchContext.storeStar].
	starsFloor int
}

// push saves an alternative checkpoint at the current offset for the node f
// leads to; see [altMatcher.Match].
func (x matchContext) push(f frame) {
	x.state.stack = append(x.state.stack, checkpoint{
		offset: x.offset,
		path:   f.path,
	})
	if debug.Enabled {
		debug.Printf(
			"checkpoint offset=%d path=%v\n",
			x.offset, f.path,
		)
	}
}

// storeStar saves a restart point for the current star at offset bytes
// further in the input; reset tells whether the star may discard the pending
// restart points first, see below.
func (x matchContext) storeStar(offset int, reset bool) {
	path := x.frame.path
	if d := x.frame.depth; len(path) < d {
		// The walk records an index in path only when it turns to a child
		// other than the first one; levels entered at child #0 are implicit.
		// Store the path at its full length (the missing entries are always
		// zeros) so that the alts above can tell this checkpoint from their
		// own. See [altMatcher.Match].
		p := x.state.allocPath(d)
		copy(p, path)
		path = p
	}
	if reset {
		// This star can extend over anything the pending restart points could
		// reach -- they are redundant, discard them. See
		// research.swtch.com/glob.
		//
		// However, only the restart points born inside the current alternative
		// may be discarded. An outer star, when resumed, re-enters the
		// enclosing alt and may pick another alternative -- something this
		// star, locked inside its own alternative, can not absorb. See the
		// `*{*0,}` test: the outer star must survive the inner one to reach
		// the empty alternative.
		x.state.stars = x.state.stars[:x.starsFloor]
	}
	x.state.stars = append(x.state.stars, checkpoint{
		offset: x.offset + offset,
		path:   path,
	})
	if debug.Enabled {
		debug.Printf(
			"star offset=%d path=%v reset=%t\n",
			x.offset+offset, path, reset,
		)
	}
}

// next returns the context for a child of the current node, matching offset
// bytes further in the input: one level deeper in the tree.
func (x matchContext) next(offset int) matchContext {
	x.offset = x.offset + offset
	x.frame.depth += 1
	return x
}

// branch returns a copy of the current frame with its path turned to child i
// at the current level, discarding the deeper levels. The new path is
// allocated from the state's arena.
func (x matchContext) branch(i int) frame {
	f := x.frame
	path := x.state.allocPath(f.depth + 1)
	copy(path, f.path)
	path[f.depth] = i
	f.path = path
	return f
}

// matchState holds the backtracking state of a single [Pattern.Match] call.
// The states are pooled globally: the buffers keep their grown capacity
// between the matches, so a steady-state Match does not allocate them.
type matchState struct {
	// stars are the star restart points and stack the alternative
	// checkpoints, both LIFO. On a mismatch the walk resumes from the most
	// recent restart point, if any, before the most recent alternative; see
	// [Pattern.Match].
	stars []checkpoint
	stack []checkpoint

	// arena is the buffer the checkpoint paths are allocated from; see
	// [matchState.allocPath]. It is bulk-freed when the match ends, which
	// spares the per-path lifetime reasoning: a path may be shared between
	// the current frame and several checkpoints.
	arena []int
}

// allocPath returns a zeroed []int of length n allocated from the state's
// arena. When the arena runs out of capacity, a fresh chunk is started; the
// paths allocated from the previous chunks stay valid, since the chunks are
// kept alive by the paths referencing them.
func (st *matchState) allocPath(n int) []int {
	if cap(st.arena)-len(st.arena) < n {
		st.arena = make([]int, 0, max(2*cap(st.arena), n, 32))
	}
	p := st.arena[len(st.arena) : len(st.arena)+n : len(st.arena)+n]
	st.arena = st.arena[:len(st.arena)+n]
	clear(p)
	return p
}

var statePool sync.Pool // Pool[*matchState]

// acquireState takes a state from the pool, or makes a new one.
func acquireState() *matchState {
	if st, _ := statePool.Get().(*matchState); st != nil {
		return st
	}
	return &matchState{}
}

// releaseState empties the state and puts it back to the pool.
func releaseState(st *matchState) {
	resetCheckpoints(&st.stars)
	resetCheckpoints(&st.stack)
	// The arena holds no references; keep the (largest) chunk as is.
	st.arena = st.arena[:0]
	statePool.Put(st)
}

// resetCheckpoints empties s keeping its capacity. The whole backing array
// is zeroed (not only the live part) to drop the references to the
// checkpoint paths popped during the match.
func resetCheckpoints(s *[]checkpoint) {
	full := (*s)[:cap(*s)]
	clear(full)
	*s = full[:0]
}

// multiMatcher is a sequence, ["a"·*·"b"]: it matches its children one
// after another, each on the input the previous ones left.
type multiMatcher []matcher

func (ms multiMatcher) String() string {
	var sb strings.Builder
	sb.WriteByte('[')
	for i, m := range ms {
		if i > 0 {
			sb.WriteString("·")
		}
		sb.WriteString(m.String())
	}
	sb.WriteByte(']')
	return sb.String()
}

func (ms multiMatcher) Match(x matchContext, s string) (n int, ok bool) {
	for i := x.frame.index(); i < len(ms); i++ {
		if i != x.frame.index() && x.state != nil {
			// The path is recorded for the checkpoints the descendants may
			// save; in a stateless walk (see [needsState]) there are none
			// and nobody would ever read it.
			x.frame = x.branch(i)
		}
		child := x.next(n)
		k, ok := ms[i].Match(child, s[n:])
		if debug.Enabled {
			debug.Printf(
				"[%T@%p] #%d match %#q against %[5]T(%[5]s) at path=%v depth=%d => %d %t\n",
				ms, unsafe.SliceData(ms), i, s[n:], ms[i],
				child.frame.path, child.frame.depth, k, ok,
			)
		}
		if !ok {
			return 0, false
		}
		n += k
	}
	return n, true
}

// altMatcher is a group of alternatives, {"a"|"b"}: it matches the one the
// walk is at (the first one when entered anew), having saved a checkpoint
// for the next one to be tried on a mismatch later.
type altMatcher []matcher

func (ms altMatcher) String() string {
	var sb strings.Builder
	sb.WriteByte('{')
	for i, m := range ms {
		if i > 0 {
			sb.WriteString("|")
		}
		sb.WriteString(m.String())
	}
	sb.WriteByte('}')
	return sb.String()
}

func (ms altMatcher) Match(x matchContext, s string) (int, bool) {
	i := x.frame.index()
	// Save a checkpoint for the next alternative to consider it in case of
	// a mismatch later (if any). This must be done only when:
	//
	//   - the alt is entered for the first time (the resume path ends above
	//     this level, or there is none);
	//
	//   - the resume path ends exactly at this level with an alternative
	//     checkpoint -- its job is "try alternative #i", so the one for the
	//     next alternative must be saved now. Note that a star restart point
	//     may end at this level too (a star being a direct child of the alt,
	//     as in `{*,b}`) -- it must not trigger a save.
	//
	// Otherwise the walk is merely passing through this alt on its way to
	// resume a deeper checkpoint -- the one for the next alternative was
	// already saved when the alt was entered for the first time, and saving
	// it again on every star restart would blow the stack up exponentially.
	// See the "alternatives" tests.
	if next := i + 1; next < len(ms) {
		d := len(x.frame.path)
		if x.frame.depth >= d || (x.frame.depth == d-1 && x.kind == checkpointStack) {
			x.push(x.branch(next))
		}
	}
	// The stars below this level may only discard the restart points born
	// inside the same alternative; see [matchContext.storeStar].
	x.starsFloor = len(x.state.stars)
	child := x.next(0)
	n, match := ms[i].Match(child, s)
	if debug.Enabled {
		debug.Printf(
			"[%T@%p] #%d match %#q against %[5]T(%[5]s) at path=%v depth=%d => %d %t\n",
			ms, unsafe.SliceData(ms), i, s, ms[i],
			child.frame.path, child.frame.depth, n, match,
		)
	}
	return n, match
}

// textMatcher is a literal, "abc".
type textMatcher struct {
	Text string
}

func (m *textMatcher) String() string {
	return strconv.Quote(m.Text)
}

func (m *textMatcher) Match(_ matchContext, s string) (int, bool) {
	if strings.HasPrefix(s, m.Text) {
		return len(m.Text), true
	}
	return 0, false
}

// charMatcher is a `?`: any single character but a separator.
type charMatcher struct {
	Sep []rune
}

func (m *charMatcher) String() string {
	var sb strings.Builder
	sb.WriteByte('?')
	if len(m.Sep) > 0 {
		sb.WriteByte('(')
		formatRunes(&sb, m.Sep)
		sb.WriteByte(')')
	}
	return sb.String()
}

func (m *charMatcher) Match(_ matchContext, s string) (int, bool) {
	if len(s) == 0 {
		return 0, false
	}
	r, n := utf8.DecodeRuneInString(s)
	if slices.Contains(m.Sep, r) {
		return 0, false
	}
	return n, true
}

// starMatcher is a `*` or a `**`: any sequence of characters, but for the
// separators in the former case.
type starMatcher struct {
	// Sep are the separators the star may not extend over; empty for `**`.
	Sep []rune
	// SepStr is Sep as a string, for the byte-wise scans below.
	SepStr string

	// Next is the literal the matcher right after this star begins with
	// (when it is a textMatcher), set by [annotateStars]. A restart point
	// at a position where the literal does not occur is a guaranteed
	// mismatch, so the star jumps between its occurrences instead of
	// retrying at every rune.
	Next string
	// Terminal is set by [annotateStars] when nothing follows this star
	// anywhere in the pattern: the star then consumes everything in its
	// reach at once, and no restart point can change the outcome.
	Terminal bool
}

// reach returns the length of the prefix of s the star may extend over:
// everything up to the nearest separator.
func (m *starMatcher) reach(s string) int {
	if m.SepStr == "" {
		return len(s)
	}
	if e := strings.IndexAny(s, m.SepStr); e >= 0 {
		return e
	}
	return len(s)
}

// storeSkip stores the restart point at the next occurrence of the m.Next
// literal instead of the next rune.
func (m *starMatcher) storeSkip(x matchContext, s string) {
	reach := m.reach(s)
	// Look for the occurrences starting within the star's reach; the
	// literal itself may extend past it (it may contain the separators).
	// Note that a valid UTF-8 literal can not match at a mid-rune
	// position, so the one-byte skip below is rune-safe.
	end := min(reach+len(m.Next), len(s))
	j := strings.Index(s[1:end], m.Next)
	if j < 0 || 1+j > reach {
		return
	}
	x.storeStar(1+j, len(m.Sep) == 0)
}

func (m *starMatcher) String() string {
	var sb strings.Builder
	sb.WriteByte('*')
	if len(m.Sep) > 0 {
		sb.WriteByte('(')
		formatRunes(&sb, m.Sep)
		sb.WriteByte(')')
	}
	return sb.String()
}

func (m *starMatcher) Match(x matchContext, s string) (int, bool) {
	if m.Terminal {
		// Nothing follows this star in the pattern: either it consumes
		// the whole remainder within its reach, or the match fails.
		return m.reach(s), true
	}
	if len(s) == 0 {
		return 0, true
	}
	if m.Next != "" {
		m.storeSkip(x, s)
		return 0, true
	}
	r, n := utf8.DecodeRuneInString(s)
	if !slices.Contains(m.Sep, r) {
		// The star may extend over the rune: save the restart point past
		// it. A separator-free star (`**`) can extend over anything the
		// pending restart points could reach, so it may discard them; see
		// [matchContext.storeStar].
		x.storeStar(n, len(m.Sep) == 0)
	}
	return 0, true
}

// runeRangeMatcher is a character range class, `[a-z]` or `[!a-z]`.
type runeRangeMatcher struct {
	Lo  rune
	Hi  rune
	Not bool
}

func (m *runeRangeMatcher) String() string {
	var sb strings.Builder
	if m.Not {
		sb.WriteByte('!')
	}
	sb.WriteByte('[')
	sb.WriteRune(m.Lo)
	sb.WriteByte('-')
	sb.WriteRune(m.Hi)
	sb.WriteByte(']')
	return sb.String()
}

func (m *runeRangeMatcher) Match(_ matchContext, s string) (int, bool) {
	// Note that an invalid byte decodes as U+FFFD, and is matched as such,
	// the same way regexp does; only the empty input is a mismatch.
	r, n := utf8.DecodeRuneInString(s)
	if n == 0 {
		return 0, false
	}
	ok := m.Lo <= r && r <= m.Hi
	if ok != m.Not {
		return n, true
	}
	return 0, false
}

// runeSetMatcher is a character set class, `[abc]` or `[!abc]`.
type runeSetMatcher struct {
	Set map[rune]struct{}
	Not bool
}

// formatRunes writes rs, sorted and comma-separated, to sb.
func formatRunes(sb *strings.Builder, rs []rune) {
	rs = slices.Clone(rs) // Not to reorder the caller's, e.g. a matcher's Sep.
	slices.Sort(rs)
	for i, r := range rs {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteRune(r)
	}
}

func (m *runeSetMatcher) String() string {
	rs := make([]rune, 0, len(m.Set))
	for r := range m.Set {
		rs = append(rs, r)
	}
	var sb strings.Builder
	if m.Not {
		sb.WriteByte('!')
	}
	sb.WriteByte('[')
	formatRunes(&sb, rs)
	sb.WriteByte(']')
	return sb.String()
}

func (m *runeSetMatcher) Match(_ matchContext, s string) (int, bool) {
	// See the note in runeRangeMatcher.Match.
	r, n := utf8.DecodeRuneInString(s)
	if n == 0 {
		return 0, false
	}
	if _, has := m.Set[r]; has != m.Not {
		return n, true
	}
	return 0, false
}

// voidMatcher matches the empty string: an empty alternative, `{a,}`. In a
// sequence it is dropped by [normalizeSequence].
type voidMatcher struct{}

func (*voidMatcher) String() string {
	return "void"
}

func (*voidMatcher) Match(matchContext, string) (int, bool) {
	return 0, true
}

// The shaped matchers below are the compile-time rewrites of the common
// terminal sub-sequences; see [specialize]. Nothing may follow them in the
// pattern, so each one either consumes the whole remainder of the input or
// fails -- deterministically, storing no checkpoints.

// prefixMatcher is a terminal `abc*`.
type prefixMatcher struct {
	Text string
	Sep  string
}

func (m *prefixMatcher) String() string {
	return "prefix(" + strconv.Quote(m.Text) + ")"
}

func (m *prefixMatcher) Match(_ matchContext, s string) (int, bool) {
	if strings.HasPrefix(s, m.Text) && noSep(s[len(m.Text):], m.Sep) {
		return len(s), true
	}
	return 0, false
}

// suffixMatcher is a terminal `*abc`.
type suffixMatcher struct {
	Text string
	Sep  string
}

func (m *suffixMatcher) String() string {
	return "suffix(" + strconv.Quote(m.Text) + ")"
}

func (m *suffixMatcher) Match(_ matchContext, s string) (int, bool) {
	if strings.HasSuffix(s, m.Text) && noSep(s[:len(s)-len(m.Text)], m.Sep) {
		return len(s), true
	}
	return 0, false
}

// prefixSuffixMatcher is a terminal `abc*def`.
type prefixSuffixMatcher struct {
	Prefix string
	Suffix string
	Sep    string
}

func (m *prefixSuffixMatcher) String() string {
	return "prefix_suffix(" + strconv.Quote(m.Prefix) + "," + strconv.Quote(m.Suffix) + ")"
}

func (m *prefixSuffixMatcher) Match(_ matchContext, s string) (int, bool) {
	// The length check keeps the prefix and the suffix from overlapping:
	// `a*ant` must not match `ant`.
	if len(s) >= len(m.Prefix)+len(m.Suffix) &&
		strings.HasPrefix(s, m.Prefix) &&
		strings.HasSuffix(s, m.Suffix) &&
		noSep(s[len(m.Prefix):len(s)-len(m.Suffix)], m.Sep) {
		return len(s), true
	}
	return 0, false
}

// containsMatcher is a terminal `*abc*` with the separator-free stars.
type containsMatcher struct {
	Text string
}

func (m *containsMatcher) String() string {
	return "contains(" + strconv.Quote(m.Text) + ")"
}

func (m *containsMatcher) Match(_ matchContext, s string) (int, bool) {
	if strings.Contains(s, m.Text) {
		return len(s), true
	}
	return 0, false
}

// noSep reports whether s contains none of the separators.
func noSep(s, sep string) bool {
	return sep == "" || !strings.ContainsAny(s, sep)
}

// formatCheckpoint renders c as path@offset for the debug output.
func formatCheckpoint(c checkpoint) string {
	return fmt.Sprintf("%v@%d", c.path, c.offset)
}

// formatStack renders the checkpoint pile s for the debug output, the most
// recent one last.
func formatStack(s []checkpoint) string {
	var sb strings.Builder
	for i, c := range s {
		if i > 0 {
			sb.WriteString(" -> ")
		}
		sb.WriteString(formatCheckpoint(c))
	}
	return sb.String()
}

// popLast panics if s is empty.
func popLast[T any, E ~[]T](s *E) T {
	n := len(*s)
	r := (*s)[n-1]
	*s = (*s)[:n-1]
	return r
}
