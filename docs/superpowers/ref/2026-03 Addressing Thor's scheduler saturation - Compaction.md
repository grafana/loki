# Compaction for Thor

[Robert Fratto](mailto:robert.fratto@grafana.com), 2026-03-24

This document describes how Thor can achieve storage locality at scale.

This is the final part of a series discussing Thor's scheduler saturation, where we finally explore implementation details. The other parts are prerequisites to fully understand both the problem and solution:

* [Analyzing Thor's task load](?tab=t.0)  
* [Storage locality in Thor for logs](?tab=t.g5upi78cpdva)  
* [Storage locality in Thor for indexes](?tab=t.c67fijfvtlwj)

# Background

The previous parts of this series discussed [why we have high query times in ops](?tab=t.0), how [log ordering can improve this](?tab=t.g5upi78cpdva), and how we can [opportunistically improve index ordering](?tab=t.c67fijfvtlwj).

This document finishes the series by talking about how to update Thor to have sorted logs and indexes over time.

# Proposal

We will use several **k-way merge** passes to gradually form large sequences of sorted data object sections called *runs*, where each run is one or more sections in known sorted order, spread across one or more data objects. 

K-way merges are already used today as part of the dataobj-consumer write path, so this is not a monumental shift. The big changes are:

1. Log records are sorted by the tenant's configured sort schema, rather than by streamID and timestamp.   
2. An external process (a compactor) runs cross-object K-way merging in the background to form larger contiguous non-overlapping runs over time.   
3. We change log and index objects to have 12-hour UTC-aligned time partitioning across objects, matching the behaviour of TOC objects; this ensures that no data object can be referenced across two TOCs and makes deletions easier. 

# Phase 1: Single-object sorting

Given that compaction will use K-way merging, we need individual objects to be sorted with the target schema before we can achieve cross-object sorting. 

This phase is a prerequisite before compaction can be built, but it's intentionally small in scope to allow for speedy delivery. It will also provide some benefits on its own as individual log objects will have better storage locality over what they have today.  

## Time bucketing

When a dataobj-consumer receives incoming data, that data will be partitioned across several in-memory builders, each aligned on 12-hour UTC windows. Before committing offsets to Kafka, all time partitioned builders are flushed. 

Time partition applies across objects, rather than sections, to guarantee that an object can only be referenced by exactly one TOC file; this makes deletions much easier to implement. Partitioning by tenant will continue to happen by section. 

This change will cause out-of-order writes to be placed in very tiny sections. The cost of this will be short-lived, as cross-object sorting will help compact these tiny sections together when needed. 

## Sort schema 

We will add a per-tenant configuration to store sort schemas, along the lines of 

```
sort_schema:
  - name: service_name 
    kind: label 
  - kind: timestamp 
```

A kind field is specified to allow future flexibility, such as permitting to sort based on commonly queried structured metadata fields like detected\_level.

The sort schema used will be embedded as file-level metadata in each data object (keyed by tenant). This will allow cross-object sorting to detect sort incompatibilities across objects. While the sort schema for index objects is currently not intended to be configurable, its sort schema will also be stored to permit future flexibility.

An object-wide sort will be performed prior to flushing for both logs and index objects, following the pattern established by [logsobj.Builder.CopyAndSort](https://github.com/grafana/loki/blob/b25e70dbf044629f7b934cba42cf9c87a71bd836/pkg/dataobj/consumer/logsobj/builder.go#L440). 

## Index a run ID 

A "run ID" will be used as a grouping key for the compactor to know whether two sections are contiguously sorted with no overlap. 

We will add this column to index objects in the [stats section](?tab=t.c67fijfvtlwj). When indexing a new logs object section, dataobj-index-builder will set the "run ID" of that section to match the hash of the indexed data object, read from its file name. 

# Phase 2: Cross-object sorting

Phase 2 will introduce a compactor to sort data across multiple objects.

The compactor will use a scheduler-worker architecture, similar to the one used for the query engine, with the "compaction task" being a fraction of the overall compaction pass to perform in a window. Like with the query engine schedulers, we will reuse the hierarchical fair queue construct to achieve fairness across tenants and windows. 

## Detecting new data 

dataobj-index-builder pods will write a sentinel file called dirty/WINDOW\_NAME any time the corresponding TOC is updated.

This is used as a very lightweight mechanism to detect whether a 12h window needs to be evaluated by the compactor: the compactor will do a LIST operation on the dirty/ prefix every compaction interval.

## Compaction triggers

For a given TOC window, the compactor will load required information from all index objects referenced by the TOC, and determine whether compaction is necessary:

* Trigger 1: Are there "too many" uncompacted runs?  
* Trigger 2: Does the data volume of uncompacted runs exceed a specific volume of bytes?

The exact configuration for these triggers will need to be discovered through experimentation. The general concept is to use knowledge of query performance to know when it's "worth" doing a compaction pass within a window. 

If a compaction pass is deemed unnecessary, the dirty file for the window is deleted using conditional deletes. Conditional deletes are supported on all major CSPs, with S3 most recently adding support for it in September 2025\. The E-Tag of the dirty file will need to be retrieved prior to starting compaction for this to work properly. 

## Compaction process

For a given TOC window to compact:

1. **Cache the dirty file E-Tag** for the eventual conditional delete to work properly.  
2. **Load runs** from the list of log object sections, grouped by tenant and run ID and sorted by sort key boundaries.   
3. **Form compaction tasks**, where each task is one set of K runs. For the initial pass, each run is one data object, but this will grow over time with each compaction pass.   
4. **Assign tasks to workers**, where they perform the K-way merge across their input runs.   
   1. Each task outputs one log object run and one index object run (holding index information for the compacted output).  
   2. The output run\_id of a task is formed deterministically by hashing the combination of its inputs (which include min/max sort key, dataobj path, section index, and run ID).   
5. **Wait for all tasks to complete.**  
6. **Compact the resulting index objects** following the same steps as 3-5.   
7. **Update the TOC** and replace references to compacted indexes with the new indexes.

If steps 1-7 produced a single run per tenant per partition (see below), no more compaction passes are needed, and the dirty file for the window can be deleted using a conditional delete with the E-tag from step 1\. Otherwise, the dirty file is kept and another pass will run at the next compaction interval.

## Compaction task partitioning 

Each compaction task operates on K runs, and outputs a single run. Subsequent passes, then, will have fractionally K fewer runs but with more sections per task, until there is only one task that produces one single run. 

The amount of data to process increasing per task can cause scalability issues, where L3/L4 compaction for large tenants will have no chance of finishing within a reasonable time.

To address this, we will use the sort schema to define partitions within a 12h window.

We will determine the maximum task size to be as follows:

```
max_task_size   = target_task_time * task_throughput
task_throughput = task_output_uncompressed_bytes / time_to_execute_task 
```

For example, if we want *any* task to complete within 10 minutes, and the average task throughput is 500MB/s, then the maximum task size is (600s \* 500MB/s) \= 300,00MB \= 300GB.

While forming the set of tasks, if any task exceeds the maximum task size, we will partition tasks, using the information stored in index objects to compute the total data volume per sort schema key. 

Partitions will have a starting and ending sort key from the schema. Partitions will be ordered matching the sort schema, and a partition will be cut whenever we cannot fit the next sort key into a partition. In the rare case where a full sort key is too big for a single partition, we will roughly divide that partition key into time ranges (partitioning within the final timestamp key in the schema).

Partition computation is deterministic, based on knowledge of log volume of sort keys and the min/max sort key per partition, and does not need to be explicitly stored anywhere. 

This partitioning logic fixes the unbounded growth of task size time, as each partition can be processed in parallel. This also means that the end goal of compaction is one single run of log objects *per partition, per window, per tenant.* 

**NOTE:** Forming partitions will require multiple tasks to read the same sections (when data for two partitions is held in the same section), but filtering out to different subsets. This impact is limited, as future compaction passes will already be partitioned and won't suffer from this read duplication.

## Index object compaction

After a compaction pass, we use the resulting index objects and compact them together using the same logic as log compaction.   
As the amount of data to index is orders of magnitude smaller than the amount of log data to store, it should be possible to set K way higher for the K-way merge so that we always output a single run of index objects.

An index object compaction ensures that we have a minimal number of index objects, fully decoupled from any partitioning we may have formed for log compaction. 

## Stale object deletion 

After a compaction pass completes, there will be a set of dangling references to the old index and log objects.

These must not be deleted right away, as there may be active queries reading from them. Instead, dangling references should be stored somewhere (perhaps in a "dangling references" prefix in object storage) and cleaned up in the background after a certain amount of time has passed. 

# Other details 

* This design permits, but doesn't explore, having multiple compactors. For example, we can use a hash ring to determine which TOC window belongs to which active compactor.

* For the initial implementation, if a compaction task fails, or the compactor crashes, it must be restarted fresh. Limiting total compaction time helps reduce the impact of this, but we may want to extend the design in the future to permit for smoother recovery. 

* While not discussed, deletes and retention can be applied by dropping log records when performing the K-way merge in a compaction task.

* While not discussed, workers will need to assert that sort schemas match across their input sections when processing a task. In the short term, workers can flag incompatible schemas and leave those sections uncompacted.

# Out of scope 

* I didn't consider how this would work with the [in-progress TSDB indexes](https://docs.google.com/document/d/1VVij7XQ9chHcIwCSo2BSgh7oK1hHjIH7XYM-znaGURo/edit?tab=t.mrbdhoktcksm#heading=h.s93vj29x4wud).

* New data may cause cascading new partitions to form across a window. We'll have to evaluate the potential read amplification impact of this. 

* Re-compacting an entire window for a delete request feels quite expensive. It may be possible to trigger a partial compaction job, based on where we know data exists, but this is left unexplored to keep this already massive document smaller in scope. 
