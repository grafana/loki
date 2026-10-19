# pkg/logline/needle/

Boolean algebra over the literals a log line must contain. `Expr` is an OR of
AND sets of literals (disjunctive normal form). A line can match only if it
contains every literal of at least one set.

The package is pure. It must not import other logline packages. Callers own
n-gram length, case folding and index versions, and apply them with `Map`.

## Invariants

1. **No false negatives.** A line that satisfies the exact formula must
   satisfy the result of every operation. Weakening a result (collapsing
   sets into their shared literals, dropping literals, TRUE) is always safe.
   Strengthening it silently drops results.
2. **There is no FALSE.** The zero value is TRUE. A leaf the caller cannot
   look up becomes TRUE, never "dropped", and a TRUE operand makes its whole
   `Or` TRUE. `Or()` with no operands is TRUE.
3. **`maxConjunctions` bounds the set count.** `And` and `Or` never return
   more than `maxConjunctions` sets. Over it, an operand (`And`) or the union
   (`Or`) is replaced by its `Collapse`, the literals present in every set. A
   value below 1 is treated as 1, and 1 always gives at most one set.
4. **Normal form.** Each set is sorted, deduplicated and non-empty. Sets are
   sorted and deduplicated, and absorption drops a set that implies another
   one (every literal of the weaker set is a substring of a literal of the
   stronger). Tests compare `String()` output, so keep the order stable.
5. **`Prune` is last.** It drops literals contained in another literal of the
   same set. Call it only on the final `Expr`, because `Collapse` relies on
   shared literals that `Prune` could remove.
6. **Immutable.** Operations return new values and never write to an
   operand's slices. `Conjunctions` returns a copy.
