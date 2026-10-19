# pkg/logline/regexliteral/

Literal substrings every match of a regex contains, used as logline index needles.

One walker, two entry points:

- `Needles` returns a `needle.Expr` that keeps alternation. `foo|bar`
  gives {foo} or {bar}. Small finite alternations are joined to the literals
  next to them (exact-set crossing), so `user_id=(123|456)` gives
  {user_id=123} or {user_id=456}.
- `Literals` is `Needles` with `maxConjunctions` 1, pruned and returned as
  sorted plain literals. With one conjunction every exact set is one string,
  so an alternation contributes only the literals all its branches share. It
  is the behaviour at `max_alternatives` 1.

Do not add a second walker. A change to the walk changes both entry points,
and `TestLiterals` pins the one-conjunction behaviour.

## Invariants

1. **No false negatives.** Every literal returned must produce only n-grams that every
   line the regex matches also produces, for every index version. Returning fewer or
   shorter literals is always safe. Returning a literal that is not in every match, or
   that a match contains in a different byte form, silently drops results.
   For `Needles` the rule is per set: every match must produce all the
   n-grams of at least one set.
   Guarded by `TestLiterals_NoFalseNegatives` and
   `TestNeedles_NoFalseNegatives`, which run through
   `logline.ExtractorForVersion`, so a new version is covered automatically.
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
   nothing. Both caps are lowered to `maxConjunctions`. Raising them costs
   planning time on the query frontend.
4. **Class runes are uppercased.** ASCII letters in a small class come back
   uppercase, so `[aA]` is one string. This is only safe because every
   extractor uppercases ASCII, and callers must uppercase literals before
   lookup.
5. **A run never has more strings than `maxConjunctions`.** Because the caps
   are lowered to it, flushing a run never collapses, and the plain text next
   to an alternation survives a low value: `(a|b)verylongliteral` with
   `maxConjunctions` 1 gives {verylongliteral}. Products of several runs in
   `And` can still collapse, which only drops precision.
6. **Not pruned.** `Needles` does not call `Prune`. A later collapse in
   the caller keeps the literals shared by every set, and pruning first can
   remove one of them. The caller prunes the final requirement. `Literals`
   prunes, because it is final.
