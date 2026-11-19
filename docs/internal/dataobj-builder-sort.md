# Sort on flush

The dataobj-builder sorts the logs of a tenant when it flushes an object.
The sort puts the streams in a fixed order and gives them new IDs.
Then it writes the logs in that order, so that queries can skip whole sections.
This page explains the steps and how the sort runs on several goroutines.

## Why the builder sorts

The builder appends log lines in the order in which they arrive.
One section of the object then holds lines of many unrelated streams.
A query for one stream must read most of the sections.

After the sort, the streams that are close in the sort order sit in the same sections.
Each section covers a small range of streams.
A query reads only the sections whose range can match.

## The steps

A sorted object holds one tenant.
The sort of one tenant has four steps.

1. **Order the streams.**
   Read the streams section of the original object.
   Compute the sort key of each stream from its labels and the sort schema of the tenant.
   Sort the streams by their sort keys.
   Give them new IDs, 1 to N, in this order.
2. **Sort the logs.**
   Each logs section of the original object is already sorted inside itself.
   Merge them into one sequence ordered by new stream ID, then by timestamp, newest first.
   Drop the duplicate lines.
3. **Write the logs sections.**
   Append the merged lines to a new logs section.
   When the section reaches the target section size, encode it and start the next one.
4. **Write the streams section.**
   Write the streams with their new IDs.
   Use the row counts and sizes from step 2, because the sort can drop duplicates.

Step 2 and step 3 cost almost all of the CPU time.
The merge compares lines, and the encoder compresses the pages.

## The problem with one goroutine

One tenant often holds most of the bytes of a flush.
Its sort runs on one goroutine, so more CPU does not make it faster.
If a flush takes as long as the builder needs to fill the next object, the builder cannot keep up.
It stops reading from Kafka while it flushes, and the lag grows.

## Sort on several goroutines

The sort can run in parallel, because the logs of different streams do not depend on each other.
The setting `-dataobj.builder.sort-parallelism` sets the maximum number of goroutines for one tenant.
The default is 1.

The idea:

1. After step 1, we have the streams in their final order.
   We know the uncompressed size of the logs of each stream.
2. Cut this list into ranges of consecutive streams.
   Each range holds about the size of one logs section.
3. Sort and encode each range on its own goroutine.
   A goroutine reads the original logs sections, and skips the rows of the streams outside its range.
   It writes one or more finished logs sections.
4. Append the finished sections to the object in the order of the ranges.
   This step is cheap, because the sections are already encoded.

The result has the same lines in the same order as the sort on one goroutine.

### Rules that keep the result correct

- **A stream stays in one range.**
  The sort drops duplicate lines, and it can only find them when all lines of a stream are in one merge.
  So the sort never splits a stream, and it keeps streams with the same sort key together.
- **A range can make more than one section.**
  A stream that is larger than a section gives a range with several sections.
- **Every range ends a section.**
  An object can have up to about twice as many sections as the sort on one goroutine makes, when streams are large compared to the section size.
  The lines and the stream stats are the same.

### Memory

The sort holds encoded sections in memory until the object builder takes them.
Two rules limit this memory.

- At most `sort-parallelism` ranges hold lines or encoded sections at a time.
  A range keeps its slot until the object builder has all of its sections.
  A slow early range therefore stops the later ranges from starting.
- The first range that is not yet in the object does not wait.
  It appends each section as soon as the encoder finishes it.

The goroutines reuse their section builders from one range to the next.
The helper `parallel.ForEachOrdered` in `pkg/util/parallel` implements the slots and the order.

### Limits

- One stream goes to one goroutine.
  A tenant with most of its bytes in one stream does not get faster with more goroutines.
- A flush can sort several objects at the same time, up to `flush-concurrency`.
  Each of them uses up to `sort-parallelism` goroutines.

## Where the code is

- `pkg/dataobj/logsobj/sort.go`: the steps, the plan of the ranges and the writers.
- `pkg/dataobj/sortmerge`: the merge of sorted sections, with a filter for the stream IDs of a range.
- `pkg/util/parallel/ordered.go`: the ordered parallel helper.
