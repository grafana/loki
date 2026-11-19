# Index runs

An index run is a set of index objects that queries and compaction read as one logical index.
The dataobj-compactor writes the output of an index merge as a run of index objects cut by size,
instead of one large index object.
This document describes the design decisions behind runs: what a run is, the rules that keep runs
correct, and how queries and compaction read them.

## The problem

An index merge combines the postings and stats of several index objects of one tenant and one
12-hour ToC window into one new index object.
As compaction goes on, these index objects grow without limit.

A large index object costs a query more than it needs.
A query reads only the postings of the columns that its matchers and predicates name.
But it opens every postings section of the index object to find them.
A large index object also costs the compactor time, because every merge rewrites it.
And the compactor builds the whole output of a merge before it can flush and upload it.
So the memory of a merge grows with the size of its output.
An index object without a size limit eventually needs more memory than a compactor has.

## The idea

The index merge cuts its output into several index objects by size.
It never cuts inside one posting key.
A posting key is the triple `(kind, column_name, label_value)`.
So each index object holds a contiguous range of posting keys, and the index objects of a run cover
disjoint ranges.

The ToC lists every index object of the run, with the range of posting keys that it holds.
A query then opens only the index objects whose range can hold a column that it reads.
The compactor plans, merges and replaces the index objects of a run as one unit.

## What the ToC records

A ToC holds one entry, an index pointer row, for each index object.
Each entry can carry these optional columns:

| Column | Meaning | Missing value means |
| --- | --- | --- |
| `run_id` | The run that holds the index object. | The index object is a run of its own. |
| `run_length` | The number of index objects of the run. | Unknown: planning skips the length check. |
| `min_posting_key` | The smallest posting key of the index object. | Unbounded: the object can hold any key. |
| `max_posting_key` | The largest posting key of the index object. | Unbounded. |
| `shard_buckets` | The shard bucket count of the tenant's shard layout in the index object. | Unknown: read it from the stats. |

The shard layout is part of the stream layout of the tenant.
The stream layout also holds the sort schema and the stream order.

A run ID is unique within one ToC only.
The ToC writer assigns it inside the conditional write that adds the run.
So two writers that race never give one ID to two runs.
A run ID can come back after its run leaves the ToC, so a reader must not keep run IDs across ToC
reads.
A run of one index object gets no run ID.

## Rules for a run

These rules hold for every run of more than one index object.

1. **One time range.**
   Every entry of a run has the time range of the run.
   A query filters the entries of a ToC by time, so it then reads all index objects of a run or none
   of them.
   Per-object time ranges would gain nothing, because a run is cut by posting key.
   Posting keys are not related to time.
2. **Posting key bounds on every entry.**
   Queries choose the index objects of a run by their key bounds.
   An entry without bounds means that the writer did not record where it cut the run.
3. **Whole runs only.**
   A change must remove all index objects of a run or none of them.
   The kept index objects would miss the postings of the removed ones.
   A new run must not reuse a path that the ToC keeps, or a path that the change removes.
   The kept entry would keep its own run ID, and a retry after a lost response would remove part of
   the new run.
4. **One layout, recorded by every index object.**
   Every index object of a run records the stream layout of the run in a stats section, even if it
   holds no stats rows.
   A run where only some index objects record a layout, or where they record different layouts, is
   corrupt.
   A sharded query reads an index object without a stats section as the default shard layout, so a
   missing section can also give a query the wrong layout.
5. **Postings in every index object.**
   Index merge planning places an index object by its postings.
   An index object without postings would fall out of the task of its run.

The ToC writer checks rules 1 to 3 when a change adds or removes a run.
See `validateNewRuns`, `errPartialRun` and `errRunPathInToC` in `pkg/dataobj/metastore/toc_writer.go`.
Compaction planning checks rules 3 to 5 on the index objects, and skips a run that breaks one of them
as corrupt.
See `indexRun.checkLength` in `pkg/engine/compactor/index_run.go`, and `indexLayoutOfRun` and
`readIndexRun` in `pkg/engine/compactor/sections.go`.
Only index merge planning checks rule 5.
Nothing checks that the entries of a run have the same `shard_buckets`.
A query fails on a mismatch.

## How queries read a run

A query groups the entries of a ToC into runs by ToC path and run ID.
It reads all postings sections of the index objects of a run with one stream selector.
A stream matches only if each matcher matches one of its labels, and those labels can sit in
different index objects of the run.
See `indexRuns` in `pkg/dataobj/metastore/index_run.go`.

Before it reads, the query drops the index objects whose key range cannot hold a column that a
matcher or an equality predicate names.
This is the only case where a query reads part of a run.
It is safe, because the dropped index objects hold no posting that the query reads.
An index object with unknown key bounds can hold any posting, so the query keeps it.
See `indexRun.withPostingsFor` in the same file.

A query never reads stats rows.
A sharded query takes the shard layout of each index object from `shard_buckets`.
If the entry has none, the query reads the layout from the stats section metadata of the index
object.
The query fails with `errRunShardLayouts` if the index objects that it reads disagree.

## How an index merge cuts a run

The index merge and the index filter write their rows in posting key order.
They cut a new index object when the current one reaches the target object size of the index
builder and the posting key changes.
So no posting key spans two index objects, and every index object holds postings.

The stats rows come after the postings in each task.
All stats rows of a task go into its last index object, which is still open when the postings end.
The earlier index objects of the task get an empty stats section, which records only the layout.
The last index object can exceed the target size by the size of the stats, which is small next to
the postings.

Each index object is uploaded when it is cut, so the task holds only one index object open.
The task reports one artifact for each index object.

## How a large index merge splits into tasks

One index merge of a large window can take too long for one task.
If `max_task_size` is set, the planner splits a merge into tasks by posting key span.
The tasks run in parallel and together write one run.
This follows the split of a log merge by sort prefix.

Planning samples one posting key in every 4096 postings rows, while it reads the postings bounds of
each section.
Each sample stands for the rows up to the next sample, with their share of the uncompressed section
size.
The planner pools the samples of every input run and cuts them into spans of about `max_task_size`
each.
A span is half open and starts at a sampled key, so no posting key spans two tasks.
The first span has no start and the last has no end, so the spans together hold every key.

Each task reads only the input sections whose key bounds overlap its span.
A section that crosses a cut is read by every task that it overlaps.
The key span predicate of a task skips the pages outside its span, and keeps exactly the rows in it.
So each task writes the rows of its span, and every task writes postings.

A stats row has no posting key, so it cannot follow the spans.
Instead the planner gives the stats rows of each input index object to one task that reads that
object.
It takes the largest objects first, and gives each to the reading task with the fewest assigned
bytes.
So the stats work spreads over the tasks, and the run holds each stats row once.
A task removes duplicate stats rows only among its own inputs.

The tasks of a merge are in key order.
The coordinator appends their artifacts in that order, so the index objects of the run stay in key
order.
Every entry of the run gets the time range and the shard layout of the whole merge.

A split task sends its runs in a separate field of the plan.
A worker that does not know key spans finds no runs and fails the task.
It does not merge every key, which would repeat the rows of the other tasks.
The coordinator also rejects an artifact whose posting keys fall outside the span of its task.

## How compaction handles a run

Planning reads the entries of a ToC and groups them into runs.
See `groupIndexRuns` in `pkg/engine/compactor/index_run.go`.

- **Index merge** plans each run as one planning unit, so one merge takes the whole run.
  A large merge can split into tasks by posting key span, as the previous section describes.
- **Log merge** plans one source run at a time.
  It folds the stats rows of every index object of the run into one reference for each log section.
  Before it dispatches the tasks, it checks that the ToC still lists every index object of the run.
  The ToC swap checks this again, because it applies only if the ToC holds every old path.
- **Index filter** keeps the index rows of the log objects that a log merge did not rewrite.
  It reads every index object of the source run.

A task reports the index objects that it wrote: the path and the posting key bounds of each.
These are the only facts that the task decides while it runs.
Planning already knows the time range of the task, which is the time range of its inputs.
It also knows the shard layout, which is the layout of its inputs.
The coordinator gives every entry of the run of the merge those values.
It replaces the source runs with one new run for each merge in one conditional write.
See `plannedRun` in `pkg/engine/compactor/coordinator.go` and `DecodeResultArtifacts` in
`pkg/dataobj/compaction/v2/result_record.go`.

## Where stats rows go

The stats rows of a log section can sit in any index object of a run, in any order across the index
objects.
Only the compactor and the deletion sweep read stats rows.
Each of them combines the rows of the whole run:

- Log merge planning folds the rows by log section.
- The index filter merges the rows of all index objects with a K-way merge.
  An index merge task does the same for the index objects that planning gives it.
- The deletion sweep takes the union of the log object paths in all rows.

Within one index object, the stats sections still form one sorted stream.

So an index object of a run can hold no stats rows.
It still records the layout under rule 4, in an empty stats section.
An empty stats section has no columns and records only the layout in its metadata.
Readers read an empty section as no rows.
The deletion sweep skips an empty section.
It still fails on any other stats section without exactly one `object_path` column, because a missed
object path would make its log object look unreachable.
A section whose columns are all of types that a reader does not know is not empty.
So a newer format cannot make the deletion sweep skip rows.
See `stats.LayoutSectionBuilder`, `Section.Empty` and `Section.ObjectPathColumn` in
`pkg/dataobj/sections/stats`.

## Open questions

- How much the ToC of a window grows when every index merge writes a run.
- What target size to choose.
  A query pays for each index object that it opens, so many small index objects can cost more than
  one large one.
- What to do when a task of a split merge writes a copy of an input index object.
  This happens when its span holds rows of only that object.
  The copy has the same content-hash path, so the ToC swap rejects the new run under rule 3.
  The coordinator logs this case.
