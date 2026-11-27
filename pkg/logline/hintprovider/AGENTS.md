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
   - Values are split at `/` and spaces, because JSON/logfmt decode `\/` to
     `/` and replace U+FFFD and invalid UTF-8 with a space. `MapAll` keeps each
     literal's fragments ANDed within its original OR branch, before capping.
   - Bare `| json | field="literal"` can also match stream/SM labels of that
     name; that residual is accepted. We cannot tell parsed-field vs stream
     vs SM, so we do not re-escape needles — only values already in the line.
6. **Hint cache keys are day-aligned and keyed on needles**
   - `CachingHintProvider` stores one entry per UTC day touched by `[from, through]`.
   - Logical format: `logline:2:<tenant>:<key>:<min-date>:<YYYY-MM-DD>`.
   - `<key>` is built by `cacheKeyQuery` from the delegate's `Needles(expr)`:
     the needle sets it plans (`needle.Conjunctions`, already sorted and
     normalized, formatted by its `String`) and the n-gram length. Queries with
     the same needle sets share an entry whatever else they contain. A metric
     query shares with the log query it wraps, and Grafana's logs volume query,
     which appends `| drop __error__`, shares with its log query. Stream matchers
     are not part of the key.
   - With no needles, or when the delegate has no `Needles` method (like
     `MinDate()`), the key is `expr.String()`.
   - `PlanHints` plans with the same `Needles` call, and its result is what the
     adapter sends to the querier (invariant 18). The key and the lookup
     therefore come from one planner and one set of settings.
   - **Everything the lookup reads must be in the key.** The delegate is asked
     about the expression as written, so equal keys give equal hints only while
     the lookup reads nothing but the needle sets and the n-gram length. If it starts
     to read anything else, such as stream matchers, other pipeline stages or new
     config, add that to `cacheKeyQuery` and bump `hintCacheGeneration`.
     `TestNeedleKey_EqualKeysGetEqualHints` fails when equal keys stop giving
     equal hints.
   - Bump `hintCacheGeneration` when the key format or the meaning of a cached
     value changes.
   - On cache miss, delegate lookups are also day-aligned: each missed day calls the delegate with that full day window (`[day_start, day_end)`).
   - Responses are post-filtered back to the caller's `[from, through]` overlap window so API behavior stays window-scoped while cache payloads stay day-complete.
7. **Cache API keys must be hashed**
   - Always call `cache.HashKey(logicalKey)` before `Fetch`/`Store`.
   - Loki cache backends do not hash for callers.
8. **Only successful results are cached**
   - `ErrUnsupported` and all other errors are passthrough and never stored.
9. **Cached payload is minimal**
   - JSON stores only time ranges (`s`/`e` epoch millis) and their document
     shard (`d`/`n`, omitted for ranges that cover every stream).
   - A range without `d`/`n` decodes as covering every stream, so an entry
     written without them reads as a superset under the same generation.
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
      `regexliteral.Needles(pattern)`, never a local AST walk. Regex extraction
      uses fixed internal enumeration limits, not the query's DNF cap. It owns the rules that keep regex needles free of false
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
14. **A shared day lookup runs under the context of the caller that started it**
    - Concurrent lookups of the same tenant, key (invariant 6) and day share one
      delegate call through singleflight. A log query and a metric query with the
      same needles therefore share a call.
    - Each caller stops waiting when its own context ends. The query frontend
      cancels a lookup when its query returns first, and the shared call keeps
      running for the callers still waiting on it.
    - Canceling the caller that started the call ends it for every caller. A
      caller that only joined it and whose own context is still live retries,
      up to `maxSharedLookupAttempts` times, and the retry runs under its own
      context. Retries are counted in
      `logline_hint_cache_singleflight_retries_total`.
15. **v5 ranges come from the footer's layout, one per document shard**
    - v5 indexes have no document table. `rangesForCells` maps each matching
      cell to its bucket's range and its document shard through
      `logline.DocumentLayout`. Other ranges (v3/v4 documents, `MatchesAll`,
      passthrough) have zero shard bits and cover every stream.
    - `NormalizeRanges` merges ranges only within one document shard.
      `intersectRanges` intersects two shards' ranges only when one shard holds
      the other, and keeps the narrower shard.
    - `QueryHints` sends the shards in `logproto.HintTimeRange` too. The query
      frontend gives each shard sub-request the ranges of its document shards,
      and queriers prune each chunk by its stream's shard
      (`iter.HintTimeRanges.ForFingerprint`). A fingerprint the ingester may
      have remapped (`logline.MaxMappedFingerprint`) gets every shard's
      ranges, because chunks and TSDB store the remapped fingerprint while the
      builder shards by the stream's own. Dry-run uses their time union
      (`TimeUnion`), and correctness checks them per stream.
    - `checkLayoutDay` requires the footer's day to be the meta's date. A
      mismatch fails the lookup, so the query runs without hints instead of
      getting ranges on the wrong day.
    - The interval and shard bits are not compared with `meta.json`. Metas
      reach the provider through `logproto.IndexMeta`, which carries only the
      date (inside the index ID), and ranges take both values from the footer.
      The v5 merge guard is where `meta.json` must agree with the footer.
16. **Shards are intersected per conjunction, never across conjunctions**
    - Lookups consume compiled `needle.Conjunctions`, a named `[][]string`.
      An empty outer slice means unconstrained; callers must not mutate slices
      shared between goroutines. `executeQuery` keeps
      state per (reader, conjunction) and returns ranges per conjunction.
      `aggregateConjunctionRanges` runs `aggregateShardRanges` on each
      conjunction and unions the results.
    - Intersecting shard values across conjunctions turns an OR into an AND:
      with `abc` on shard 1 and `def` on shard 2, it computes `abc and def`.
    - A term shared by several conjunctions is one job and is read once. Its
      bitmap is cloned for every conjunction after the first, because
      `Bitmap.And` can return its argument and later Ands change a result in
      place.
17. **A leaf we cannot look up is TRUE, never dropped**
    - Under AND, dropping a filter and treating it as TRUE are the same thing.
      Under OR they are not: `|= "abcdef" or "ab"` must pass through, because
      lines with only `ab` are real results. A TRUE branch makes its whole OR
      TRUE.
    - TRUE leaves: `ip()`, negative filters, pattern filters, literals shorter
      than `ngramLength`, `""`, non-verbatim post-parser values, matchers other
      than `=` and `=~`, reserved `__*` names, numeric, duration and bytes label
      filters, and regexes with no required literal.
    - An or chain with a `""` branch anywhere is skipped. Loki runs it
      differently depending on where `""` sits (`|= "" or "" or "a"` keeps
      every line, `|= "a" or "" or ""` does not), so we plan nothing for it
      until LogQL defines the semantic.
18. **`maxConjunctions` bounds the needle sets**
    - `SupportedQuery` builds a factored `needle.Expr` with uncapped `And` and
      `Or`, maps literals before collapse, and calls `Compile(maxConjunctions)`
      once on the complete requirement. Compilation bounds DNF expansion and
      prunes at the end. Over the limit an operand or union collapses to its
      implied existing literals. 1 gives at most one set.
    - The query-frontend plans the needles once, with its own cap, and sends
      them in `LoglineIndexRequest.conjunctions`. The querier looks up those sets
      and does not plan or take a cap. The hint cache key is built from the same
      plan, so the key and the lookup cannot disagree. During a rollout that
      holds only once every component on the path (frontend, query-scheduler
      and querier) runs this version. A request without `conjunctions` is from
      an older frontend, or passed through an older scheduler, which drops the
      field. The querier plans `expr` with one set, which is what an older
      frontend's default planned.
    - The frontend owns the configuration default of 1; multiple conjunctions
      require explicit opt-in. Examples are pinned in `TestSupportedQuery_Or`.
19. **The metadata cache is an LRU, and the querier does not poll**
    - `metadataCache` keys parsed index metadata by index ID. An index and its
      `meta.json` never change once written, so an entry cannot go stale. It
      only stops being asked for, when the index is compacted or deleted, and
      then it ages out of the LRU. Nothing evicts from a catalog snapshot.
    - It is bounded by `maxMetadataCacheEntries` and by a byte budget of the
      `estimateBytes` sizes (`logline.query.metadata_cache_max_size_mb` on the
      querier, `DefaultMetadataCacheMaxBytes` on correctness). An entry larger
      than the whole budget is not cached.
    - The querier answers `QueryHints` from the `Indexes` the frontend planned
      and never reads the store's snapshot, so it does not poll the catalog. Do
      not add a querier code path that calls `PlanHints`, `ProvideHints` or
      `IndexesForRange`: on the querier they would see an empty catalog.
