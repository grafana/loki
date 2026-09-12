# pkg/logline/internal/v4/

v4 n-gram extraction. Registered with the `pkg/logline` shim as version `"v4"`.

## What v4 changes

Two rules, both pure functions of the candidate gram's own bytes:

1. A text n-gram whose bytes are **all digits** is not emitted.
2. Every window of **`NumericNgramLength` (9) consecutive digits** is emitted as a
   packed numeric key.

Everything else is byte-for-byte identical to v3.

## Why 9

At v3's 6 bytes an all-digit gram lives in a 10^6 space. ops-002 ingests ~2.9M lines/s, so
a 1s document holds ~3M lines and every 6-digit value occurs in every document. The density
filter then stores each as a `MatchesAll` sentinel, so an integer query of *any* length is
decomposed into saturated 6-grams and the AND narrows nothing. A 19-digit id, inherently
one of the most selective things a user can search, scans everything.

At 9 digits the space is 10^9. Measured on ops-002: nanosecond fractions (exactly 9 digits,
19,839 distinct out of 20,050 sampled) sit at ~2.5% document frequency, far under the 20%
density threshold, so they become useful terms rather than noise. No filtering is needed;
the entropy does it.

9 is also the largest length that still serves 9-digit queries, since a needle must contain
`NumericNgramLength` consecutive digits to produce any numeric gram.

Full evidence: `.private/tasks/noisy-timestamps-selectivity/task3-adaptive-ngram-length/`.

## Key encoding

The term dictionary key and the radix sort both use **6 bytes**
(`v3.NgramLength`, `radixSortByNgram`). Nine ASCII digits need nine bytes and cannot fit, so
digits are packed as a base-10 integer instead:

```
byte 0    numericTag (0x01)
byte 1-5  value, 40-bit big-endian
byte 6-7  zero (radixSortByNgram orders bytes 0-5 and assumes the rest are zero)
```

9 digits need 30 bits, so they fit with room to spare. The 40-bit payload holds up to
`maxNumericDigits` (12) digits, so `NumericNgramLength` can be raised to 12 with no format
change (but it is still a new index version, because the emitted set changes).

### Why the tag byte

Text and numeric terms share one flat, exact-match keyspace. Without a discriminator a
packed value could equal a text gram, merging unrelated postings in both directions.

Text grams only ever contain the transformed alphabet: space, `.`, `0-9`, `A-Z`. So any byte
below 0x20 is impossible in a text gram and is safe as a tag. `0x01` is used; `0x00` is left
free as an "empty key" sentinel.

### Adding future gram classes

Tag values 0x02..0x1F are free. A future hex or fixed-length class takes a new tag and the
same 5-byte payload. Note that a *single* index only ever emits one numeric length, so the
tag does not need to encode the length: the length is a property of the index version, which
`meta.json` already records and which drives extractor dispatch.

## Invariants

1. **Bytes 6-7 stay zero.** `radixSortByNgram` orders only bytes 0-5 and assumes the rest are
   zero; violating this mis-sorts runs and wedges the writer's ascending-term check.
2. **The packed key occupies all six bytes.** `ExtractQueryNgrams` passes a packed key whole
   (`logline.IsPackedTermKey`) instead of slicing it to `ngram_length`, so numeric lookups work
   at any `ngram_length`; consumers slice it back down (`FindTerm` to six bytes,
   `filterNgramsForShard` to eight). Slicing by `ngram_length` would truncate the low value
   bytes and the lookup would miss. Text grams are still sliced to `ngram_length`, so their
   terms are unchanged.
3. **Rules are context-free.** Both read only the candidate gram's own bytes, so build and
   query always agree. This is what keeps recall exact: a numeric needle shorter than
   `NumericNgramLength` produces no grams, `ExtractQueryNgrams` returns nothing,
   `batch_query` returns `ErrUnsupported`, and the query passes through to a full Loki scan
   instead of silently skipping data. Guarded by `TestExtractFeatures_BuildQuerySymmetry`.
4. **Text output equals v3** for any input without an all-digit window. Guarded by
   `TestExtractFeatures_TextParityWithV3`, whose expectations are copied from v3's golden.
5. **Frozen.** Do not change the emitted n-gram set. Add a v5 instead.

## On-disk format: shared with v3

v4 defines no file format. The `.lidx` layout, footer, term dictionary and postings are
v3's, so the shim dispatches all format operations for `"v4"` to v3 and only
`ExtractorForVersion` differs. Consequence: footer auto-detection (`OpenFile`,
`OpenReaderAt`) cannot distinguish a v4 file and reports `"v3"`. That is safe because only
format-only tooling auto-detects; the query path takes the version from `meta.json`.
