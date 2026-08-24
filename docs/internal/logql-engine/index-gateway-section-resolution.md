# Index-gateway data-object section resolution (v1 engine)

Internal design documentation for resolving data-object sections in the index-gateway, so the
classic (v1) engine's stream-first metric queries stop resolving sections redundantly on every
querier subquery.

## Why

A stream-first metric query on the v1 engine reads data-object *sections*. To find them the querier
calls `metastore.Sections()` — it lists the index objects for the query's time range, reads each
one's postings section, and returns the matching sections and their source-object stream IDs.

The query-frontend fans a query into legs on two axes: time split, then query shard. Each shard leg
is a separate `SelectSamples` on a querier, and each calls `metastore.Sections()` independently with
no shard. So a query sharded N ways resolves the *same* sections N times. On production load this was
~256x redundant index reads and a ~5s p100 resolution latency that floored every sharded query,
because resolution is uncached and every shard re-reads the same index objects from object storage.

The metastore table-of-contents is partitioned in **12h UTC-aligned windows**
(`metastore.MetastoreWindowSize`). Resolving at that granularity, once per window, and reusing the
result across every shard and time split removes the redundancy.

## What

Resolution moves to the **index-gateway**, behind a new unary RPC:

```
rpc ResolveDataObjectSections(ResolveDataObjectSectionsRequest) returns (ResolveDataObjectSectionsResponse)
```

- The request carries a single 12h UTC-aligned window `[from, through)` and the selector's matchers
  (a matcher string, as with `GetChunkRef`). The tenant travels in the gRPC context.
- The response is the matching sections grouped by object: `objects[] { objectPath, sections[] {
  sectionIdx, streamIds } }`. `streamIds` are the matching source-object stream IDs.

The querier calls it once per 12h window (splitting its subquery range), routed by `tenant + window`,
and unions the results. The rest of the read path is unchanged — the querier still decodes stream
labels, computes fingerprints, and applies its shard filter exactly as before. Only the *source* of
the section list changes.

This is Phase 1: it does not group stream IDs by shard bucket, so a window's resolution is identical
for every shard. It replaces the 256x object-storage read storm with 256x small, cache-served RPC
responses.

## Server-side components (`pkg/indexgateway`)

Each is a single responsibility and unit-testable with fakes.

- **`DataObjectSectionsResolver`** (`dataobj_sections_resolver.go`) — the RPC handler logic. It
  validates the window, derives the cache key, serves from cache or resolves, and deduplicates
  concurrent identical resolutions with a `singleflight.Group`. It reuses the existing
  `metastore.Metastore` for index access: `GetIndexes` for the key, `Sections` for the resolution.
- **`dataObjectSectionsCache`** (`dataobj_sections_cache.go`) — a serialization-aware wrapper over a
  single `cache.Cache`. It also derives the key (free functions in the same file): a 12h window is not
  immutable (data arrives late), but an index object is, so the key hashes the window start, the
  matchers, and the immutable **set of index objects** the metastore lists for the window. Late data
  adds an index object, which changes the set and therefore the key, so a stale entry is never served.
  The window start is in the key because two adjacent windows can list the same straddling object.

The metastore itself is reused as-is; there is no new lister.

### Caching

The cache is built by `cache.New` from a single `cache.Config`, which already tiers its layers: the
embedded (in-process, `MaxSizeMB`-bounded) cache is L1 and memcached, if configured, is L2 with
asynchronous (background write-back) stores. `NewTiered` handles L1 → L2 reads and back-fill, so the
wrapper only adds the `[]byte` get/put boundary and treats any cache fault as a miss (degrade to
recompute, never fail the request). The embedded cache is on by default.

Both layers are keyed by (window start + matchers + object set), so entries never need invalidation.
`GetIndexes` runs per request to compute the key (it is the cheap ToC listing); only the expensive
`Sections` result is cached. A cached entry that fails to decode, or whose stored inputs do not match
the request (a hash collision), is treated as a miss and recomputed, so a poisoned entry self-heals.

### Singleflight

One `singleflight.Group`, keyed by the cache key (tenant + window + matchers + index-object set),
wraps the whole lookup: the single tiered cache read, then on a miss `Sections`, then the put.
Concurrent requests for the same key collapse onto one leader and share its result. The shared work
runs on a context detached from the leader's cancellation, so a leader that disconnects mid-flight
does not fail the waiters. With `tenant + window` routing, a window's concurrent requests land on one
gateway, so they collapse to a single `Sections` resolution.

### Unaligned windows

The handler validates `from == truncate(from, 12h).UTC()` and `through == from + 12h`; an unaligned
window returns `codes.InvalidArgument`. Alignment is the querier's responsibility (it derives windows
from `metastore.IterTableOfContentsPaths`, which yields only aligned windows), so an unaligned request
is a client bug and fails loudly.

## Client-side routing (`pkg/indexgateway/client.go`)

`GatewayClient.ResolveDataObjectSections` routes via `poolDoConsistent`, which — unlike `poolDo` —
does not shuffle. It orders the tenant's gateways by a jump hash of `tenant + window`, tries the
primary first, and falls back to the rest on a transient failure. Non-retryable replies (feature
disabled, unaligned window) and a cancelled request return immediately, since every gateway shares the
same config and would answer identically. This gives per-window cache affinity so the resolution cache
and singleflight are effective, while any gateway can still serve on failover.

## Querier integration (`pkg/querier`)

`dataObjReadPlanner` resolves sections through a `dataObjSectionsResolver` interface
(`dataobj_sections_resolver.go`) instead of calling the metastore directly:

- **`metastoreSectionsResolver`** — the default; wraps `metastore.Sections`, unchanged behaviour.
- **`indexGatewaySectionsResolver`** — splits the subquery range into 12h windows
  (`IterTableOfContentsPaths`), calls the gateway per window, and merges the responses **deduplicated
  by section key** (a data object straddling a 12h boundary is listed in both windows, so the same
  `(object, section)` can return twice; stream IDs are unioned, mirroring the metastore's own
  per-`SectionKey` dedup). On any gateway error it **falls back** to the metastore for the whole
  request, so a query never fails or under-resolves.

The resolver is chosen by whether a gateway client was wired in; the querier's read path downstream is
untouched.

## Wiring

`NewIndexGateway` auto-creates the resolver when the feature is enabled; callers do not pass it in.
Building the metastore needs a data-object bucket, which only the module wiring can construct, so
`initIndexGateway` builds the metastore and injects it into the index-gateway config
(`t.Cfg.IndexGateway.DataObjectSections.Metastore`, a non-YAML field). `NewIndexGateway` then builds
the cache and resolver from that config.

## Configuration

- Gateway: `-index-gateway.dataobject-sections.enabled` turns the API on (and requires data-object
  storage so a metastore can be built), with `-index-gateway.dataobject-sections.cache.*` for the
  cache (embedded L1 under `...cache.embedded-cache.*`, memcached L2 under `...cache.memcached.*`).
- Querier: `-querier.dataobjects-section-resolution-via-index-gateway-enabled` routes resolution to
  the gateway. Both default off; the querier falls back to local resolution when the gateway is
  unavailable, so partial rollout and mixed fleets are safe.

## Correctness

- The gateway result is equivalent to the local `metastore.Sections` result for the same query: same
  matching sections and stream IDs, and therefore the same samples. Resolving full 12h windows can
  return sections just outside the subquery's `[start, end]`; the read path time-filters them, so
  they contribute no samples.
- Late data is handled by the object-set cache key.
- A missing section on the querier stays corruption (the existing existence check), not silent data
  loss.

## Phase 2 (not implemented)

Group stream IDs by shard bucket so the gateway returns only the caller's buckets and the querier can
skip the fingerprint recheck. This needs the per-stream shard bucket, which is not in the postings
section — it requires a postings→pointers→streams join in the resolver or a per-stream bucket added
to the postings section — plus a `shard` field on the request and an `exact` flag on the response.
