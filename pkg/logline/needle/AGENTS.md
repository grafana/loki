# pkg/logline/needle/

Pure Boolean requirements over literals a log line must contain. Do not import
other logline packages. Callers own n-gram length, case folding and index
versions, and apply those lookup semantics with `Expr.Map` before compilation.

## API

- `Expr` is an immutable factored AND/OR/literal tree; shared nodes form a DAG.
  Its zero value is TRUE.
- `True`, `Lit`, `AllOf`, `And(...Expr)` and `Or(...Expr)` construct expressions
  without a cap or DNF expansion. Constructors remove identical node operands.
  `AllOf` stores a compact AND of leaves, retaining contained literals.
- `Expr.Map` rewrites leaves before lowering. Shared nodes are mapped once and
  their mapped output is reused. It does not collapse or prune.
- `Expr.Compile(maxConjunctions int) Conjunctions` bounds lowering and prunes at
  the end. It returns caller-owned, sorted and normalized conjunctions: all
  literals of at least one conjunction must be present. A nil or zero-length
  outer slice means TRUE; inner slices are never empty. TRUE compiles to nil.
- Both slice levels belong to the caller. Mutating a result must not affect the
  source expression, shared subtrees, another conjunction, or any other compile
  call. The final prune creates the owned slices; do not add a second copy.
- `Conjunctions` is a named `[][]string` with a diagnostic `String` method.
  Callers use `len`, `range` and indexing directly; there is no accessor or
  extra copy. DNF remains private compiler state.

## Compiler rules

1. **No false negatives.** Every exact match must satisfy the compiled plan.
   Collapse only weakens requirements. Map callbacks must return requirements
   implied under the caller's lookup semantics. There is no FALSE: rejected or
   empty mapped leaves mean TRUE. TRUE absorbs OR and is an AND identity.
   Both zero-operand constructors return TRUE.
2. **Bound expansion before allocating.** Clamp caps below 1 to 1. Check AND
   products with division to avoid overflow. OR batches use the original
   `total / 4 > cap` allowance; deduplicate and absorb each full batch before
   deciding to collapse. Raw operand counts alone must not trigger collapse.
   Do not add truncation or precision-dropping work budgets.
3. **Keep selectivity between products.** Flatten associative chains
   iteratively with a seen-node set. Batch initially mandatory literals; fold
   alternatives in stable fewest-alternatives-first order. Apply a collapsed
   operand immediately and normalize before checking the next product's cap:
   it may absorb alternatives and make that next product fit.
4. **Preserve collapse candidates.** Consider existing literals from every
   source branch, including late, absorbed and redundant ones. Do not invent
   substrings or import candidates absent from the collapsed subtree. A single
   exact source conjunction may reject impossible candidates, but surviving
   candidates still require a source implication proof. Choose cheapest OR
   witnesses using cached scalar costs: minimum for OR, saturating sum for AND.
   Do not cache growing witness slices. Proof traversal also checks a cheapest
   child first to avoid branch-order-dependent work. AND implies a literal
   if any operand does; OR only if every operand does.
5. **Reuse DAG work.** Keep source node identities stable. Cache lowered plans,
   collapse results and `(node, literal)` implication/occurrence proofs within
   each compilation. Index candidates once, lazily on the first collapse.
   Avoid rewalking every nested subtree for every candidate.
6. **Prune only at the end.** Retain contained literals until every collapse
   decision is complete. Never mutate source nodes or shared DNF slices. Final
   pruning copies both slice levels before handing the result to the caller.
   Normalize intermediate DNF with sorting, deduplication and absorption,
   preserving exact-membership fast paths before substring searches. Final
   pruning preserves implication, so only re-sort afterwards; do not repeat
   quadratic absorption.

Keep this a factored expression and bounded compiler, not a general optimizer.

## Validation

Run standalone checks while adjacent callers are being ported:

```sh
go test ./pkg/logline/needle
go test -race ./pkg/logline/needle
go vet ./pkg/logline/needle
go test ./pkg/logline/needle -run '^$' -bench . -benchmem
```

Use `Conjunctions.String()` expectations for readable behavior tests, with
`TRUE` for unconstrained results. Keep direct slice assertions for ownership
and shape, and literal slice fixtures in `TestConjunctionsString`. Preserve
deterministic tests for caps, substring candidates, mapping, immutability, long
chains and shared DAGs. `BenchmarkNestedOverCap` covers nested source proofs;
`BenchmarkSharedConjunctions` covers the exact-membership fast path.
