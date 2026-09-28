# pkg/logline/regexliteral/

Required literal substrings of a regex, used as logline index needles.

## Invariants

1. **No false negatives.** Every literal returned must produce only n-grams that every
   line the regex matches also produces, for every index version. Returning fewer or
   shorter literals is always safe. Returning a literal that is not in every match, or
   that a match contains in a different byte form, silently drops results.
   Guarded by `TestRequired_NoFalseNegatives`, which runs through
   `logline.ExtractorForVersion`, so a new version is covered automatically.
2. **No length filtering.** The caller owns the n-gram length.
