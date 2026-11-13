# pkg/logline/regexliteral/

Literal substrings every match of a regex contains, used as logline index needles.

One walker implementation, two entry points:

- `Needles` returns an uncompiled, factored `needle.Expr` that keeps
  alternation. `foo|bar` gives {foo} or {bar}. Small finite alternations are
  joined to the literals next to them (exact-set crossing), so
  `user_id=(123|456)` gives {user_id=123} or {user_id=456}.
- `Literals` returns the first conjunction of `Needles(pattern).Compile(1)`
  as sorted plain literals (or nil when nothing is required). Compiling with
  a DNF cap of one keeps only requirements all branches imply. It is the
  behaviour at `max_alternatives` 1.

`Needles(pattern string)` takes no cap. Keep the current two walks: a basic
walk with enumeration capped at one, and a rich OR-aware walk with fixed
`maxExact`/`maxClass` enumeration limits. Both use the same walker
implementation, and their requirements are ANDed without compiling. The
basic walk preserves fallback literals, including suffixes joined into the
rich walk's alternatives. Only `Compile` takes the query's DNF cap.
`TestLiterals` pins the one-conjunction behaviour. Exact attempts and runs
now use deferred text, but keep the two walks and the existing exact-attempt
control flow. A single bottom-up regex analysis is a separate, unapproved
change.

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
3. **Exact sets are small.** `exact` gives the finite set of strings a node
   matches only up to `maxExact` (16) strings, and a character class only up
   to `maxClass` (4) runes. A run that would cross past `maxExact` is flushed
   and restarts. Anything larger is walked branch by branch, or required
   nothing. These fixed **enumeration limits** are independent of the query's
   **DNF cap**, passed only to `Expr.Compile`. The basic walk uses one for
   both enumeration limits; the rich walk always uses 16 and 4, regardless
   of the DNF cap. Raising enumeration limits costs planning time on the
   query frontend.
4. **Class runes are uppercased.** ASCII letters in a small class come back
   uppercase, so `[aA]` is one string. This is only safe because every
   extractor uppercases ASCII, and callers must uppercase literals before
   lookup.
5. **Runs are bounded; expressions stay factored.** A run never has more
   strings than its walk's exact-set limit. Flushing uses uncapped
   `needle.And`/`needle.Or`, as does combining branches and the two walks.
   Non-exact alternations survive even the basic walk. That walk preserves
   plain text next to an alternation:
   `Needles("(a|b)verylongliteral").Compile(1)` gives {verylongliteral}.
6. **Compile and prune only at the end.** `Needles` never calls `Compile`.
   Callers map literals and combine requirements before calling
   `Expr.Compile(maxConjunctions)` on the final expression. Compile applies
   the DNF cap and prunes internally, returning `needle.Conjunctions`, a named
   `[][]string` with `String` for diagnostics (empty means nothing is required).
   Keeping redundant substrings until then lets a bounded result retain
   requirements implied by every branch. There is no
   public plan wrapper or exported `Prune`/`Collapse` operation. `Literals`
   compiles because its result is final.
7. **Defer text, not requirements.** `deferred_text.go` stores exact alternatives
   and runs as immutable ropes. Concatenation shares children and caches byte
   length and a compositional fingerprint; it never copies a growing prefix
   or a growing fragment array. `flush` materializes each final literal once.
   Singleton exact repeats share doubled subtrees. There is no new text
   budget or truncation: large adjacent captures still produce full literals.
8. **Deduplication and repeat accounting are unchanged.** Deferred sets are
   unordered to avoid sorting and rescanning shared prefixes after every
   cross. Fingerprints only reject unequal text; equal fingerprints and byte
   lengths require byte comparisons, which skip identical shared subtrees.
   Deduplicate unions and cross products as before, but retain the existing
   product-size check *before* crossing, even when duplicates would make the
   result fit. Repeat limits use the byte length of the **lexicographically
   first** alternative, not the shortest or first-inserted one. Preserve the
   old check-before-append boundary for exact repeats: the final copy can
   cross `maxRepeatBytes`. Omitted or variable copies still break adjacency.
   `deferred_text_test.go` covers these storage and boundary invariants;
   leave the existing test and benchmark files to the parent workflow.
