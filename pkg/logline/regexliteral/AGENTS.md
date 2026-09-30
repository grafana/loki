# pkg/logline/regexliteral/

Required literal substrings of a regex, used as logline index needles.

## Invariants

1. **No false negatives.** Every literal returned must produce only n-grams that every
   line the regex matches also produces, for every index version. Returning fewer or
   shorter literals is always safe. Returning a literal that is not in every match, or
   that a match contains in a different byte form, silently drops results.
   Guarded by `TestRequired_NoFalseNegatives`, which runs through
   `logline.ExtractorForVersion`, so a new version is covered automatically.
   One accepted exception: under `(?i)` a few non-ASCII runes match an ASCII
   letter (U+017F for s and U+212A for k in the regexp. Loki's simplified
   filters also fold U+0130 and U+0131 to i). The index splits tokens on them,
   so lines that spell a letter that way are missed. Needles stay whole
   instead, and the property test does not generate those substitutions.
2. **No length filtering.** The caller owns the n-gram length.
