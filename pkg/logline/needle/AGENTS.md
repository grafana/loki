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
- `Expr.Compile(maxConjunctions int) Plan` bounds lowering and prunes at the end.
- `Plan` is a distinct immutable DNF result with `IsTrue`, `Conjunctions` and
  `String`. Its zero value is TRUE. `Conjunctions` returns a deep copy.
- There are no public expression inspection, collapse or prune methods, nor
  plan transformation methods. Inspect the compiled plan instead.

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
   decision is complete. Never mutate source nodes or shared plan slices.
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

Use deterministic tests for caps, substring candidates, mapping, immutability,
long chains and shared DAGs. `BenchmarkNestedOverCap` covers nested source proofs;
`BenchmarkSharedConjunctions` covers the exact-membership fast path.
