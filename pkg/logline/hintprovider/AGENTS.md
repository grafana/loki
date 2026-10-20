# pkg/logline/hintprovider/

Query-shape support and hint lookup for logline index lookups.

## Invariants

1. **Hint ranges are half-open `[start, end)`**
   - `HintTimeRange` matches builder document bounds: Start inclusive, End exclusive.
   - Cross-shard intersection must use half-open overlap (`start < end`); abutting
     ranges produce empty intersection, never zero-duration instants.
   - `MatchesAll` converts inclusive `Meta.MinLogTs/MaxLogTs` to half-open by
     adding one millisecond to the end, matching cache and document precision.
2. **Unsupported is explicit**
   - `ProvideHints` returns `ErrUnsupported` when `SupportedQuery` returns
     TRUE, so the query has no needle to look up.
3. **Pre-parser label filters are needles too**
   - Literals from `ExtractLabelFiltersBeforeParser` are looked up like line
     filters (indexes include SM + stream label values).
   - `and` and `,` combine children with AND, `or` combines them with OR.
4. **Line filters are needles only while the ingested line is intact**
   - `|=` / `|~` match the current pipeline line, not extracted labels.
   - `collectLineFilters` returns whole stages, with their `Left` and `Or`
     chains, so `lineFilterStage` can mirror `LineFilterExpr.Filter`: the
     `Left` chain is ANDed, and a node with an `Or` chain is an OR of its
     branches. Branches of a chain all take the operator of the first one.
   - `collectLineFilters` walks stages left to right and stops at
     `line_format`, `decolorize`, or `unpack`. A later parser does not
     reopen: those stages rewrite the line.
   - Parsers (`json` / `logfmt` / `regexp` / `pattern`) and label stages
     (`label_format` / `keep` / `drop`) do not rewrite the line; filters
     after them stay eligible. Unknown stages fail closed.
5. **Post-parser extracted-field filters are needles only when verbatim**
   - After `json` / `logfmt` / `regexp` / `pattern` (including expression parsers),
     equality values and required regex literals (invariant 13) are looked up
     like line filters. Which stages are eligible is decided in
     `postParserWindow`.
   - `line_format` and `decolorize` do not drop filters on labels already
     extracted. A parser after either reads the rewritten line, so do not
     start collecting again.
   - Stop (and do not restart) at `label_format`, `keep`, `drop`, `unpack`, or
     an unrecognized stage. keep/drop do not invent values; the cutoff is
     fail-closed. When in doubt, skip.
   - Needles are matcher values only (never field names); prefixes are not stripped.
   - `UsableLabelName` skips reserved `__*` names such as `__error__`, before
     or after the parser. Loki sets those, so their values are not in the line.
   - `IsVerbatimLineLiteral` makes values containing `"`, `\`, or control
     characters TRUE, so JSON/logfmt unescape cannot produce false negatives.
     The check runs per regex literal, so one non-verbatim run does not drop
     the others.
   - Bare `| json | field="literal"` can also match stream/SM labels of that
     name; that residual is accepted. We cannot tell parsed-field vs stream
     vs SM, so we do not re-escape needles — only values already in the line.
6. **Hint cache keys are day-aligned**
   - `CachingHintProvider` stores one entry per UTC day touched by `[from, through]`.
   - Logical format: `logline:1:<tenant>:<expr.String()>:<min-date>:<YYYY-MM-DD>`.
   - On cache miss, delegate lookups are also day-aligned: each missed day calls the delegate with that full day window (`[day_start, day_end)`).
   - Responses are post-filtered back to the caller's `[from, through]` overlap window so API behavior stays window-scoped while cache payloads stay day-complete.
7. **Cache API keys must be hashed**
   - Always call `cache.HashKey(logicalKey)` before `Fetch`/`Store`.
   - Loki cache backends do not hash for callers.
8. **Only successful results are cached**
   - `ErrUnsupported` and all other errors are passthrough and never stored.
9. **Cached payload is minimal**
   - JSON stores only time ranges (`s`/`e` epoch millis).
   - `HintTimeRange.Source` is diagnostic-only and intentionally omitted.
10. **Per-request cache bypass is context-driven**
   - `WithSkipCache` and `SkipCache` propagate bypass state from request handling.
   - When set, the decorator skips `Fetch`, `Store`, and singleflight.
11. **Snapshot drift is an accepted TTL trade-off**
   - Cached day payloads represent the delegate snapshot at fill time.
   - New indexes that appear later in the same day are visible after cache TTL expiry or when bypassing cache.
12. **Mixed index versions are resolved per version**
   - The extractor belongs to the index version, so one needle can decompose into
     different terms per version, and those terms route to different shards.
   - Shard values are intersected only within one `(version, algorithm, count)`
     group. Intersecting across versions drops the ranges of a version that has
     no term on a shard the other version queried.
   - Literals are ANDed per conjunction and per version. A literal with no
     terms under a version (v4 emits none for a number shorter than 9 digits)
     drops out of its conjunction for that version. If some conjunction has no
     terms left for some version in the window, `buildTermJobs` returns
     `ErrUnconstrained`. The conjunctions are ORed, so one that cannot narrow
     makes the whole query unconstrained. Skipping that version's blocks would
     leave them without ranges and hide their matches.
13. **A regex contributes the needles every match satisfies**
    - `|~` and `=~` (line, pre-parser and post-parser filters alike) go through
      `regexliteral.Needles` with the query's `maxConjunctions`, never a local
      AST walk. It owns the rules that keep regex needles free of false
      negatives, including `(?i)` handling.
    - Anchoring is irrelevant here. Label regexes are anchored, line regexes are
      not, and both only need "contains".
    - Every needle leaves `SupportedQuery` with ASCII letters uppercased by
      `upperASCII`, matching the extractors. Never use `strings.ToUpper`: it
      maps U+0131 and U+017F to ASCII, which the extractors treat as
      separators.
    - Its needles are unfiltered. `SupportedQuery` maps them with the length
      and uppercase rule (`planner.keep`). A literal shorter than
      `ngramLength` becomes TRUE, so a set left empty makes the regex TRUE.
14. **Shards are intersected per conjunction, never across conjunctions**
    - Needles are a `needle.Expr`, an OR of conjunctions. `executeQuery` keeps
      state per (reader, conjunction) and returns ranges per conjunction.
      `aggregateConjunctionRanges` runs `aggregateShardRanges` on each
      conjunction and unions the results.
    - Intersecting shard values across conjunctions turns an OR into an AND:
      with `abc` on shard 1 and `def` on shard 2, it computes `abc and def`.
    - A term shared by several conjunctions is one job and is read once. Its
      bitmap is cloned for every conjunction after the first, because
      `Bitmap.And` can return its argument and later Ands change a result in
      place.
15. **A leaf we cannot look up is TRUE, never dropped**
    - Under AND, dropping a filter and treating it as TRUE are the same thing.
      Under OR they are not: `|= "abcdef" or "ab"` must pass through, because
      lines with only `ab` are real results. A TRUE branch makes its whole OR
      TRUE.
    - TRUE leaves: `ip()`, negative filters, pattern filters, literals shorter
      than `ngramLength`, `""`, non-verbatim post-parser values, matchers other
      than `=` and `=~`, reserved `__*` names, numeric, duration and bytes label
      filters, and regexes with no required literal.
    - Loki drops a TRUE branch from an or (`|= "abcdef" or ""` runs as
      `|= "abcdef"`). We plan it as TRUE, which is safe, only less narrow.
16. **`maxConjunctions` bounds the needle sets**
    - `SupportedQuery` ANDs every clause with `needle.And(maxConjunctions, ...)`
      and prunes once at the end. Over the limit a clause collapses to the
      needles all its sets share. 1 gives one set, the needles every branch
      shares.
    - The query-frontend sends its value in `LoglineIndexRequest.max_conjunctions`.
      The querier uses it when above 0, else its own default.
    - Guarded by `TestSupportedQuery_NoFalseNegatives`, which runs random
      pipelines through Loki and checks every kept line against every index
      version at 1, 4 and 64 conjunctions.
