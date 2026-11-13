# ToC watermarks

A ToC watermark tells the dataobj-builder which Kafka records a ToC already holds.
The builder uses it to drop records that it read twice.
Without it, a restart can put the same log lines in the index two times.

## The problem

A ToC (table of contents) is a small object for one tenant and one 12-hour window.
It lists the index pointers of that tenant and window.
Queries use these pointers to find the log objects.

A flush in the builder has three steps.

1. It uploads one log object for each tenant and ToC window.
2. It adds an index pointer for each log object to the ToC of that tenant and window.
3. It commits the Kafka offset of the last record.

The builder can stop between step 2 and step 3.
Then it reads the same records again after the restart.
The second run can cut other log objects, because the flush time and the size limits can differ.
Other log objects have other index pointers, so nothing finds the duplicate.
A query then counts some log lines two times.

## The idea

Each ToC keeps one number for each Kafka topic and partition.
This number is the watermark.
It is the highest offset whose records for this tenant and window the ToC already holds.

The builder follows two rules.

- **Drop rule.**
  The builder drops a record when its offset is at or below the watermark of its tenant and window.
  It still counts the record as read, so the commit moves past it.
- **Write rule.**
  The builder raises the watermark in the same write that adds the index pointer.
  The write works only if the ToC still holds the watermark that the builder expected.

The first rule removes the duplicates.
The second rule keeps the watermark true.
A ToC never has the pointer without the watermark, and never has the watermark without the pointer.

## How the builder works

1. **First record of a tenant and window.**
   The builder reads the watermark from the ToC in the bucket.
   It keeps the value in a cache.
   The cache is a plain LRU, so any entry can leave and the builder reads it again later.
   See `watermarkCache` in `pkg/dataobj/builder/watermarks.go`.
2. **Each record.**
   The builder compares the record offset with the watermark.
   It drops the record or appends it to the builder of its tenant and window.
   See `MultiObjectBuilder.Append` in `pkg/dataobj/builder/multi_builder.go`.
3. **Initial watermark.**
   The first record of a tenant and window decides the watermark of its builder.
   The builder keeps it as the *initial watermark* until its flush ends.
   See `WatermarkedBuilder` in `pkg/dataobj/builder/multi_builder.go`.
4. **Flush.**
   The flush adds the index pointer and sets the watermark to the offset of the last record of the flush.
   It expects the ToC to hold the initial watermark of the builder.
   If it does not, the write fails.
   See `writeToC` in `pkg/dataobj/builder/flush_committer.go` and `WriteEntryWithWatermark` in `pkg/dataobj/metastore/toc_writer.go`.
5. **After a flush.**
   The cache stores the flush offset, so the next builder of the same tenant and window starts from it.
   See `MultiObjectBuilder.Reset` in `pkg/dataobj/builder/multi_builder.go`.

When a write fails because the watermark is not the expected one, the builder stops and restarts.
It reads the real watermarks again and goes on from the last commit.

## Example scenarios

All scenarios use one partition, one 12-hour window, and two tenants, A and B.
The numbers are Kafka offsets.
The builder flushes after every four records.

| Offset | 1 | 2 | 3 | 4 | 5 | 6 | 7 | 8 |
| ------ | - | - | - | - | - | - | - | - |
| Tenant | A | B | A | B | A | B | A | B |

Flush 1 holds the offsets 1 to 4.
Flush 2 holds the offsets 5 to 8.
Both ToCs start with no watermark.

### Scenario 1: no crash

| Step | ToC of A | ToC of B | Commit |
| ---- | -------- | -------- | ------ |
| Flush 1 ends | pointer, watermark 4 | pointer, watermark 4 | 4 |
| Flush 2 ends | pointers, watermark 8 | pointers, watermark 8 | 8 |

The watermark always moves with the pointer.
Nothing is dropped.

### Scenario 2: the builder stops before the ToC writes of flush 2

Both ToCs hold the watermark 4.
The commit is 4.

After the restart, the builder reads from offset 5.
Its initial watermark is 4 for both tenants.
No record is at or below 4, so it keeps all four records.
It can cut other log objects than the first run.
This is safe: the ToCs never saw the first objects.

**Result:** every line appears once.

### Scenario 3: the builder stops during the ToC writes of flush 2

The ToC of A holds a pointer and the watermark 8.
The ToC of B still holds the watermark 4.
The commit is 4.

After the restart, the builder reads from offset 5.

| Tenant | Records | Watermark | Action |
| ------ | ------- | --------- | ------ |
| A | 5, 7 | 8 | Drops both. The ToC holds them in the first run's object. |
| B | 6, 8 | 4 | Keeps both. They go into a new log object. |

**Result:** every line appears once. Without watermarks, the lines of A would appear twice.

### Scenario 4: the builder stops after the ToC writes and before the commit

Both ToCs hold the watermark 8.
The commit is still 4.

After the restart, the builder reads from offset 5.
It drops all four records.
Its builders stay empty, so it has nothing to flush.
After the idle timeout, an empty flush commits offset 8.
A long replay of dropped records also commits after the maximum age of a builder.

**Result:** every line appears once, and the commit catches up.

### Scenario 5: the builder stops after the commit

The commit is 8.
The builder reads from offset 9, and no record is at or below a watermark.

**Result:** nothing to drop, every line appears once.

### Scenario 6: two builders read the same partition

A pod with a wrong partition ID, or an old pod that did not stop, can read the same partition.
Call the two builders X and Y.
Both read the offsets 5 to 8 and both expect the watermark 4 for tenant A.

1. X writes the ToC of A first. The watermark becomes 8.
2. Y writes the ToC of A. It expects 4 and finds 8, so the write fails.
3. Y stops and restarts. It reads the watermark 8 and drops the records 5 and 7.

**Result:** the ToC of A holds one copy of every line.
Y can leave log objects in the bucket that no ToC points to.
These objects use space but do not change query results.

If X and Y build the same log object, the pointer is already in the ToC.
The write of Y then changes nothing and succeeds.

### Scenario 7: a record with lines in two windows

A record at offset 9 has one line in window 1 and one line in window 2.
The ToC of A for window 1 holds the watermark 12, because another flush already wrote it.
The ToC of A for window 2 holds the watermark 8.

The builder checks each window on its own.
It drops the part for window 1, and it keeps the part for window 2.

**Result:** no line appears twice, and no line is lost.

## What the watermark does not cover

- **Old ToCs.**
  A ToC that has no watermark drops nothing.
  A replay of records from before this change can still add duplicates.
- **A recreated topic.**
  Offsets then start again below the watermarks, and the builder drops new records.
  The builder has no guard for this.
  If `loki_dataobj_builder_toc_watermark_dropped_records_total` grows without a restart or a replay, check the topic.
- **Old compactors.**
  A compactor that does not copy the watermark section removes the watermark.
  The next write of the builder then fails, and the builder restarts.
  Release the compactor change before the builder change.
- **Load.**
  The builder reads one ToC for each tenant and window after a start.
  Each read costs one request to the bucket.

## What to watch

| Metric | Meaning |
| ------ | ------- |
| `loki_dataobj_builder_toc_watermark_dropped_records_total` | Records that the builder dropped. It grows after a replay. |
| `loki_dataobj_builder_toc_watermark_cache_lookups_total` | Cache hits and misses. A miss reads a ToC. |
| `loki_dataobj_builder_toc_watermark_read_duration_seconds` | Time to read one watermark, by result. |
| `loki_dataobj_builder_toc_watermark_cache_entries` | Number of watermarks in the cache. |

A watermark mismatch has no metric, because the builder exits right after it.
Look for the error log of the flush and for the restart count of the pod.
