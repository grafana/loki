# pkg/logline/regexliteral/

Literal substrings every match of a regex contains, used as logline index needles.

One walker implementation, two entry points:

- `Needles` returns an uncompiled, factored `needle.Expr` that keeps
  alternation. `foo|bar` gives {foo} or {bar}. Small finite alternations are
  joined to the literals next to them (exact-set crossing), so
  `user_id=(123|456)` gives {user_id=123} or {user_id=456}.
- `Literals` is `Needles(pattern, 1).Compile(1).Conjunctions()`, returned as
  sorted plain literals. With an enumeration cap of one every exact set is
  one string; compiling with a DNF cap of one keeps only requirements all
  branches imply. It is the behaviour at `max_alternatives` 1.

Keep the current two walks: a basic walk with enumeration capped at one, and
an OR-aware walk when the requested enumeration cap is larger. Both use the
same walker implementation, and their requirements are ANDed without
compiling. Do not introduce deferred strings or bottom-up regex analysis as
part of the needle API migration. `TestLiterals` pins the one-conjunction
behaviour.

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
   nothing. Both caps are lowered to `max(maxConjunctions, 1)`, the argument
   to `Needles`. This is an **enumeration cap**, distinct from the **DNF cap**
   passed to `Expr.Compile`; it does not bound expression alternatives.
   Raising enumeration caps costs planning time on the query frontend.
4. **Class runes are uppercased.** ASCII letters in a small class come back
   uppercase, so `[aA]` is one string. This is only safe because every
   extractor uppercases ASCII, and callers must uppercase literals before
   lookup.
5. **Runs are bounded; expressions stay factored.** A run never has more
   strings than the normalized enumeration cap. Flushing uses uncapped
   `needle.And`/`needle.Or`, as does combining branches and the two walks.
   Non-exact alternations survive even an enumeration cap of one. The basic
   walk preserves plain text next to an alternation: compiling
   `Needles("(a|b)verylongliteral", 1)` with a DNF cap of one gives
   {verylongliteral}.
6. **Compile and prune only at the end.** `Needles` never calls `Compile`.
   Callers map literals and combine requirements before calling
   `Expr.Compile(maxConjunctions)` on the final expression. Compile applies
   the DNF cap and prunes internally, producing a `needle.Plan`. Keeping
   redundant substrings until then lets a bounded plan retain requirements
   implied by every branch. There are no exported `Prune`/`Collapse` calls.
   `Literals` compiles because its result is final.
