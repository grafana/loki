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
   - `ProvideHints` returns `ErrUnsupported` when query shape cannot be represented by the provider.
3. **Pre-parser label filters are needles too**
   - Literals from `ExtractLabelFiltersBeforeParser` are looked up like line
     filters (indexes include SM + stream label values).
4. **Line filters are needles only while the ingested line is intact**
   - `|=` / `|~` match the current pipeline line, not extracted labels.
   - `collectLineFilters` walks stages left to right and stops at
     `line_format`, `decolorize`, or `unpack`. A later parser does not
     reopen: those stages rewrite the line.
   - Parsers (`json` / `logfmt` / `regexp` / `pattern`) and label stages
     (`label_format` / `keep` / `drop`) do not rewrite the line; filters
     after them stay eligible. Unknown stages fail closed.
5. **Post-parser extracted-field filters are needles only when verbatim**
   - After `json` / `logfmt` / `regexp` / `pattern` (including expression parsers),
     equality and line-matcher-equivalent regex values are looked up like line
     filters. Which stages are eligible is decided in
     `literalsFromPostParserWindow`.
   - `line_format` and `decolorize` do not drop filters on labels already
     extracted. A parser after either reads the rewritten line, so do not
     start collecting again.
   - Stop (and do not restart) at `label_format`, `keep`, `drop`, `unpack`, or
     an unrecognized stage. keep/drop do not invent values; the cutoff is
     fail-closed. When in doubt, skip.
   - Needles are matcher values only (never field names); prefixes are not stripped.
   - `IsVerbatimLineLiteral` skips values containing `"`, `\`, or control
     characters so JSON/logfmt unescape cannot produce false negatives. N-gram
     extraction uppercases, so `(?i)` literals are the same lookup as equality.
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
