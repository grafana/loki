# pkg/logline/regexliteral/

Literal substrings every match of a regex contains, used as logline index needles.

One shared memoized bottom-up analysis on demand, two entry points:

- `Needles` returns an uncompiled, factored `needle.Expr` that keeps
  alternation. `foo|bar` gives {foo} or {bar}. Small finite alternations are
  joined to the literals next to them (exact-set crossing), so
  `user_id=(123|456)` gives {user_id=123} or {user_id=456}.
- `Literals` returns the first conjunction of `Needles(pattern).Compile(1)`
  as sorted plain literals (or nil when nothing is required). Compiling with
  a DNF cap of one keeps only requirements all branches imply. It is the
  behaviour at `max_alternatives` 1.

`Needles(pattern string)` takes no cap. `analysis.go` computes paired exact-set
summaries on demand for a basic mode (cap one) and a rich mode
(`maxExact`/`maxClass`). Each requested node is analyzed at most once, with
child summaries completed bottom-up only when its exact operation needs them.
Preserve short-circuit traversal: an optional body that starts with a non-exact
node must not analyze its unused trailing captures. Basic successes can share
immutable singleton sets with the rich mode. Basic failures must be computed
independently, even when the rich result is a singleton.

`regexliteral.go` emits the basic and rich requirements using cached summaries,
ANDing them without compiling. If the root has a successful basic exact summary,
its singleton is identical in both modes and is emitted only once. Fallback
emission requests missing summaries
on demand rather than treating an unvisited child as non-exact. These
contextual emission walks keep the existing run joins, fallback boundaries,
source order, and suffix requirements; they never recompute exact analysis
for a cached node. An exact ancestor consumes its rope directly, without
emitting its children's standalone
requirements. Analysis never creates needle expressions or materializes
intermediate text. Only `Compile` takes the query's DNF cap.
`TestLiterals` pins the one-conjunction behaviour.

## Invariants

1. **No false negatives.** Every literal returned must produce only n-grams that every
   line the regex matches also produces, for every index version. Returning fewer or
   shorter literals is always safe. Returning a literal that is not in every match, or
   that a match contains in a different byte form, silently drops results.
   For `Needles` every match must satisfy the expression. After compilation,
   it must produce all the n-grams of at least one conjunction.
   Guarded by `TestLiterals_NoFalseNegatives`, which runs through
   `logline.ExtractorForVersion`, so a new version is covered automatically.
   `Needles` cases are pinned in `TestNeedles`.
   One accepted exception: under `(?i)` a few non-ASCII runes match an ASCII
   letter (U+017F for s and U+212A for k in the regexp. Loki's simplified
   filters also fold U+0130 and U+0131 to i). The index splits tokens on them,
   so lines that spell a letter that way are missed. Needles stay whole
   instead, and the property test does not generate those substitutions.
2. **No length filtering.** The caller owns the n-gram length.
3. **Exact sets are small.** Cached exact summaries give the finite set of
   strings a node matches only up to `maxExact` (16) strings, and a character
   class only up to `maxClass` (4) runes. A run that would cross past `maxExact` is flushed
   and restarts. Anything larger is walked branch by branch, or required
   nothing. These fixed **enumeration limits** are independent of the query's
   **DNF cap**, passed only to `Expr.Compile`. The basic mode uses one for
   both enumeration limits; the rich mode always uses 16 and 4, regardless
   of the DNF cap. Raising enumeration limits costs planning time on the
   query frontend.
4. **Class runes are uppercased.** ASCII letters in a small class come back
   uppercase, so `[aA]` is one string. This is only safe because every
   extractor uppercases ASCII, and callers must uppercase literals before
   lookup.
5. **Runs are bounded; expressions stay factored.** A run never has more
   strings than its walk's exact-set limit. Flushing uses uncapped
   `needle.And`/`needle.Or`, as does combining branches and the two modes.
   Non-exact alternations survive even basic emission. That mode preserves
   plain text next to an alternation:
   `Needles("(a|b)verylongliteral").Compile(1)` gives {verylongliteral}.
6. **Compile and prune only at the end.** `Needles` never calls `Compile`.
   Callers map literals and combine requirements before calling
   `Expr.Compile(maxConjunctions)` on the final expression. Compile applies
   the DNF cap and prunes internally, returning `needle.Conjunctions`, a named
   `[][]string` with `String` for diagnostics (empty means nothing is required).
   Keeping redundant substrings until then lets a bounded result retain
   requirements implied by every branch. There is no public plan wrapper or
   exported `Prune`/`Collapse` operation. `Literals`
   compiles because its result is final.
7. **Defer text, not requirements.** `deferred_text.go` stores exact alternatives
   and runs as immutable ropes. Concatenation shares children and caches byte
   length and a compositional fingerprint; it never copies a growing prefix
   or a growing fragment array. `flush` materializes each final literal once
   and sorts the final strings before creating the OR. Preserve this order:
   bounded OR absorption can lose precision if absorbing literals arrive late.
   Singleton exact repeats share doubled subtrees. There is no new text
   budget or truncation: large adjacent captures still produce full literals.
8. **Deduplication and repeat accounting.** Deferred sets are
   unordered to avoid sorting and rescanning shared prefixes after every
   cross. Fingerprints only reject unequal text; equal fingerprints and byte
   lengths require byte comparisons, which skip identical shared subtrees.
   Deduplicate unions and cross products, but check the product size
   *before* crossing, even when duplicates would make the
   result fit. Repeat limits use the byte length of the **lexicographically
   first** alternative, not the shortest or first-inserted one. Check the byte
   limit before appending each exact repeat: the final copy can
   cross `maxRepeatBytes`. Omitted or variable copies still break adjacency.
9. **Exact failure is sticky.** Analyze each mode with its own intermediate
   product/union checks; do not infer exactness from the final language alone.
   In particular, a zero-count repeat still requires an exact child. A rich
   singleton may coexist with a basic failure. Store nil for non-exact and
   `{nil}` for the exact set containing the empty string. Stars and variable
   zero-min repeats other than `{0,1}` need neither child analysis nor fallback
   emission. Concat and alternate exact analysis stop at their first failure
   for each mode. If contextual fallback later needs a skipped child, it must
   request that child's summary, not read the cache as if it were complete.

`analysis_reference_test.go` provides a recursive test-only oracle independent
of the memoized analysis.
`analysis_test.go` compares exact summaries and compiled results over a
deterministic syntax/context/cap/mapping matrix, and covers nested non-exact
nodes, optional-body short-circuiting, on-demand fallback, and deferred exact
ancestors. Parity checks assert exact precision, not merely the absence of false
negatives. `deferred_text_test.go` covers rope storage and boundary invariants.
