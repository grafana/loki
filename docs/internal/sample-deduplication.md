# Sample deduplication in timestamp-first metric queries

Describes the behaviour as of `51f843a7` (2026-09-21).

A metric query reads samples from the ingesters and from the chunk store. This document states the
contract those sources must satisfy so the query does not count a log entry twice, and records where
the contract does not hold today.

It covers the timestamp-first sample order, which is the order every metric query uses. A
stream-first order exists and is wired into the chunk store and the ingester, but no query path
requests it yet. Where the two orders differ, this document says so.

Read sections 1 to 4 before adding a sample source. Section 6 lists defects that produce wrong
numbers today.

## 1. The sample identity

Deduplication treats two samples as the same sample when all three of these match:

1. **Stream hash** — a hash of the log stream's labels. It is not the fingerprint the ingester
   stores for that stream. Section 4 says which labels each order hashes.
2. **Timestamp** — the log entry's timestamp, compared exactly, to the nanosecond.
3. **Sample hash** — a hash of the query's **grouped output labels** joined with the **raw log
   line**.

The third component needs care, because two things about it surprise people.

It hashes the **grouped** labels, not the pipeline's output labels. The query's `by` or `without`
clause is applied first. So `count_over_time({app="x"})` hashes the full output labels, while
`sum(count_over_time({app="x"}))` hashes the constant empty label set, and
`sum by (app) (...)` hashes only `app`.

It hashes the **raw** line, not the line a `line_format` produced, and it never hashes the sample's
value.

### The identity does not uniquely identify a log entry

Two different entries share one identity when they share a stream, a timestamp to the nanosecond, a
raw line, and grouped output labels. No hash collision is needed. The query's grouping creates the
condition, by removing the label that told the entries apart.

This is the root cause of the defects in section 6.1. It also means the absent value is a real
omission rather than a safe one: two entries that share an identity can carry different values, and
deduplication keeps whichever arrives first.

## 2. Why deduplication is necessary

Three mechanisms make one log entry reach a query more than once.

**Replication.** A distributor writes each entry to several ingesters, and a query reaches every
healthy ingester. Each replica returns its copy.

**Flush skew.** Replicas flush at different moments. One replica can hold an entry in memory while
another has already written it to the store. Chunks are not retained in memory after flushing by
default, so skew between replicas drives this, not a retain period.

**Overlapping read ranges.** Whether the ingester range and the store range overlap depends on the
deployment:

- In the single-binary target, the ingester reads the store itself, and the querier's two ranges
  are complementary. Deduplication between memory and storage happens inside the ingester.
- In microservices and simple-scalable deployments, the ingester does not read the store, and the
  querier asks both sources for the same range. Deduplication happens in the querier.

The two arrangements are mutually exclusive. Either way `query_ingesters_within`, three hours by
default, stops the ingesters being asked about older ranges at all.

Before any of this, the write path tries to make the duplicates identical rather than merely
equivalent. Replicas cut chunks on a synchronised boundary, so their flushed chunks can be
byte-identical and collapse into one stored object. The store's own deduplication only matters for
chunks that failed to align.

## 3. Where deduplication happens

Four places combine samples and deduplicate. All four use the identity from section 1. Three are
reachable today.

- **Within one series in the chunk store.** A series' chunks are grouped by the fingerprint the
  index holds, then split into runs that do not overlap in time, and the runs are deduplicated
  against each other.
- **Across configured stores.** No query path reaches this one yet, but it is the seam a new
  store-backed source gets added to. Its ranges are disjoint and it still deduplicates, so it is
  also the one place the code does not follow the "disjoint, so nothing to deduplicate" reasoning.
- **Inside an ingester** that reads the store itself, which is the single-binary target only.
- **In the querier**, in one pass over every reached ingester plus the store.

Three places combine without deduplicating, and each omission matters:

- **Across series inside one store read.** Chunks under different index fingerprints are combined
  by a plain sort. Two copies of one stream that reached the index under different fingerprints are
  never compared. See section 6.3.
- **Across tenants.** Samples from different tenants are never the same sample.
- **Across query shards, and across the sub-queries of a split-by-interval.** These are not sample
  combines at all. They are combined later, as result matrices, which have their own overlap
  handling.

## 4. The contract

A source feeding any of the four deduplicating combines must satisfy all of the following.

**C1. Emit samples in the combine's full sort order.** For timestamp-first that is non-decreasing by
timestamp, and ascending by stream hash within one timestamp, so each timestamp-and-stream-hash
group arrives as one unbroken run. Timestamp order alone is not enough.
*Violation: duplicates are never compared, so the result inflates. A source that goes backwards past
the current step window instead loses those samples, which deflates the result.*

**C2. Report the same stream hash as every source you overlap, computed over the same labels.** The
hash must depend only on the stream's labels and its tenant. It must not depend on which replica
answered, on the query's grouping, or on anything derived per line. Timestamp-first sources hash the
stream's labels unchanged. Stream-first sources hash them with `__name__` removed. The two are not
interchangeable.
*Violation: the copies land in different groups and are never compared. The result inflates.*

**C3. Report the same sample hash as every source you overlap, for the same log entry.** Both
sources must agree on the grouped output labels and on the raw line bytes. This is a property of the
source and the query together: a pipeline that is not deterministic makes C3 unsatisfiable by any
source.
*Violation: inflates when two sources disagree. Deflates when a source omits a component that
distinguishes two entries.*

**C4. Report the same timestamp, to the nanosecond.** The comparison is exact. A source that
truncates or rounds timestamps matches nothing.
*Violation: the result inflates.*

**C5. Report the same number of samples per identity as every source you overlap.** Deduplication
keeps the count from whichever source is read first, not the largest or the smallest count. Sources
that disagree therefore give a result that depends on read order.
*Violation: the result inflates or deflates, and can change between runs of the same query.*

**C6. Do not overlap another source if you cannot compute a sample hash.** A zero sample hash means
"no identity available", and such samples are never treated as duplicates of anything.
*Violation: the result inflates, with no mechanism available to correct it.*

Two notes on scope. C1 binds every source, including one whose range cannot overlap: the
non-deduplicating combines are the same merge, and unordered input corrupts the output order. C2 to
C6 bind only sources that can overlap another.

Nothing checks any of these at runtime. Every violation returns a wrong number, not an error.

## 5. Adding a source

1. **Establish whether your range can overlap another source's.** If something enforces
   disjointness rather than assuming it, only C1 applies. Enforced disjointness has its own failure
   mode: an off-by-one at the boundary drops samples instead of duplicating them.
2. **Decide what you hash, and match an existing source exactly.** If you read flushed chunks, you
   must remove `__name__` before hashing, because the flush path overwrites that label. If you read
   in-memory streams, you must not.
3. **Compare multiplicity, not just hashes.** For one identity, check that your source reports the
   same number of samples as the source you overlap. This is the rule Loki's own sources break, and
   the one C2 and C3 testing will not catch.
4. **Build the test on a stream that defeats the identity.** Use several entries at one identical
   nanosecond, in one stream, with the same raw line, distinguished by a label that the query's
   grouping removes. A test on simple streams proves almost nothing, because the hashing paths
   coincide there.
5. **Make both sources end together in the test.** A combine left with one live source stops
   deduplicating, so a test where one source outlives the other passes over its tail without
   exercising anything.
6. **Do not use the duplicate counter as evidence.** See section 7.2.

## 6. Known defects

These produce wrong numbers today. They are not hypothetical.

### 6.1 Grouping can erase what distinguishes two entries

A query whose grouping removes the only label that told two entries apart makes them share one
identity, per section 1. Deduplication then drops one of them.

The condition is ordinary: entries at one nanosecond arise whenever timestamps have second or
millisecond resolution, and a per-line label such as a trace or span identifier distinguishes them.
Adding `by` or `without` removes that label from the identity.

Three shapes of wrong answer follow:

- **Under-count.** A counting query loses one entry per merged pair.
- **Wrong value.** For an `unwrap` query, the two entries can carry different values.
  Deduplication keeps the first one read, so the result depends on read order rather than on the
  data.
- **An answer that changes over time.** The store deduplicates within a series and the ingesters do
  not, which is a C5 violation between Loki's own sources. While the entries are in memory the query
  returns both. Once they are only in the store it returns one. The same query over the same fixed
  window silently drops a sample later. Where replicas disagree, the answer can also change between
  consecutive runs, because the order replicas are read in is not fixed.

No source can fix this alone. The duplicates are two genuinely different log entries, so a source
that dropped one would itself lose data. The identity is what needs to change.

### 6.2 Streams carrying `__name__` are counted once per source

The flush path writes `__name__="logs"` onto every chunk's labels, and every store read path removes
that label to recover the stream's own labels. A stream that itself carries `__name__` is
indistinguishable from the injected one, so the store removes the user's label and its value is
lost.

Loki accepts `__name__` on write and does not reject or rename it.

In timestamp-first order the ingester hashes the stream's labels unchanged, so it disagrees with the
store on both the stream hash and the sample hash. In stream-first order the ingester removes
`__name__` too, so only the sample hash disagrees.

How the error presents depends on the query:

- **Ungrouped**, the two sources produce two result series with visibly different labels. An
  operator can see something is wrong.
- **Grouped**, both land in one result series and the count silently doubles.

### 6.3 A remapped fingerprint defeats store deduplication

When two streams in one ingester collide on their label hash, that ingester gives one of them a
substitute fingerprint so the two stay separate in memory. The substitute comes from a per-ingester
counter, so two replicas need not choose the same one.

The flushed chunk carries that substitute, and the index stores it. The store groups chunks for
deduplication by the fingerprint it read from the index, and combines different fingerprints with a
plain sort. So two replicas that chose different substitutes each keep their copy.

Both copies then reach the querier inside a single source, where section 7.1 means they are not
compared there either.

Order does not help. Deriving the stream hash from labels, which both orders do, only stops the
copies landing in different dedup groups. It does not get them compared.

## 7. Quirks

### 7.1 Duplicates from one source survive

A source's samples are only ever compared against samples already contributed by other sources,
never against its own. When one source emits two samples that share an identity, both survive.

The write path narrows but does not remove the chance of this. An ingester drops an entry that
repeats the line it appended immediately before, and drops an exact repeat within the head block it
is currently filling. An entry that arrives interleaved with others, or after the head block was
cut, is stored twice. The ordered chunk format does not check at all.

### 7.2 The duplicate counter measures work done, not correctness

Every removed duplicate increments a per-query statistic, reported with the query's other
statistics. The figure is attributed to the store's chunk statistics whatever the duplicate's
origin.

It counts removals. It cannot count duplicates that were never compared. A query whose sources
disagree on a hash reports zero duplicates and looks healthy, because no comparison ever matched. So
a zero count does not show correctness, and a count that falls after a change to a source is worth
investigating.

To detect a failure, compare results instead:

- Run the same counting query over a window the ingesters still hold, then over the same window
  from the store alone. A ratio near the replication factor means no deduplication happened.
- Re-run a query over a fixed past window after the data has left the ingesters. A changed answer
  points at section 6.1.
- Record the duplicate count before changing a source, so the after figure has a baseline.

### 7.3 The last remaining source is not deduplicated

Once one source is left, the combine passes its samples straight through. That is safe, because
nothing remains to compare against. A combine given only one source from the start behaves the same
way.

### 7.4 A missed comparison is more likely than a false one

Any disagreement on a hash causes a missed comparison, so an inflated result. Do not read that as a
128-bit safety margin against the opposite error. Section 6.1 shows a false deduplication happening
with exact, intended hash equality and no collision at all.

### 7.5 No source reports a zero sample hash today

Every producer of samples on the chunk read path sets the hash. C6 therefore has no subject at
present, and the zero case is better understood as tolerance for a mixed-version peer than as a
facility in use. A source that wants to be the first to rely on it should know it would be the first.

## 8. Causes of divergence worth knowing

Named because each is a configuration or deployment change rather than a code defect, and each
breaks C2 or C3 while everything looks healthy:

- **Chunk format.** An older chunk format drops structured metadata that a newer one keeps, so the
  same entry hashes differently. A rollout where replicas differ violates C3.
- **Label-hashing build tags.** A non-default build hashes stream labels with `__name__` excluded,
  which changes whether section 6.2 breaks C2 as well as C3.
- **Deployment target.** It decides whether memory-versus-storage deduplication happens in the
  ingester or in the querier, per section 2.
