---
title: "0003: Coprocessor"
description: "introduce coprocessor from Google’s BigTable coprocessor and HBase coprocessor"
---

# 0003: Coprocessor

**Author:** liguozhong (fuling.lgz@alibaba-inc.com)

**Date:** 02/2023

**Sponsor(s):** @jeschkies

**Type:** Feature

**Status:** Draft

**Related issues/PRs:** https://github.com/grafana/loki/issues/8559 AND https://github.com/grafana/loki/issues/91

**Thread from [mailing list](https://groups.google.com/forum/#!forum/lokiproject):** N/A

---

## Background

{log_type="service_metrics"} |= "ee74f4ee-3059-4473-8ba6-94d8bfe03272"

We have counted the source distribution of our logql, and 85% of the grafana log explore queries are traceID queries.
Generally, the log time range of traceID is about 10 minutes（trace time= start～end）

## Problem Statement

but because users do not know traceID start time and end time , they usually search for 7 day log. 
In fact having a time range of "7d-10m" is an invalid search.

## Goals

So we hope to introduce some auxiliary abilities to solve this "7d-10m" invalid search.

We have checked that in the database field, such feature have been implemented very maturely.

And our team tried to implement the preQuery Coprocessor, and achieved great success. 

Through this feature, we solved the problem of "loki + traceID search is very slow".

## Non-Goals

The problem of slow traceID search In the past six months, we tried to introduce kv system / reverse index text system/ bloomfilter to speed up logql return, but the machine cost was too high and finally gave up.
So we don't want to introduce cost-intensive solutions to solve the problem of slow traceID search.

## Proposals

### Proposal 0: Do nothing

Users cannot migrate from log systems like ELK and other indexing schemes to LOKI,if the user heavily uses traceID log search.

### Proposal 1: Query Coprocessor

Thanks Google’s BigTable coprocessor and HBase coprocessor.
HBase coprocessor_introduction link： https://blogs.apache.org/hbase/entry/coprocessor_introduction
The idea of HBase Coprocessors was inspired by Google’s BigTable coprocessors. Jeff Dean gave a talk at LADIS ’09 (http://www.scribd.com/doc/21631448/Dean-Keynote-Ladis2009, page 66-67)
HBase Coprocessor
The RegionObserver interface provides callbacks for:

`**preOpen, postOpen**`: Called before and after the region is reported as online to the master.
`**preFlush, postFlush**`: Called before and after the memstore is flushed into a new store file.
`**preGet, postGet**`: Called before and after a client makes a Get request.
`**preExists, postExists**`: Called before and after the client tests for existence using a Get.
`**prePut and postPut**`: Called before and after the client stores a value.
`**preDelete and postDelete**`: Called before and after the client deletes a value.
etc.


Loki Coprocessor
The QuerierObserver interface provides callbacks for:

`**preQuery**`: Called before querier , Pass (logql, start, end) 3 parameters to the Coprocessor,
and the Coprocessor judges whether it is necessary for the querier to actually execute this query.

For example, for traceID search,   query range = 7d + `split_queries_by_interval: 2h`.
This logql query will actually be divided into 84 query sub-requests, and here 83 are invalid,
and only one 2h sub-request can find the log of traceID.
We try to implement two types of Coprocessors in this scenario.

traceID Coprocessor 1 simple text analysis :
if traceID is traceID from XRay or openTelemtry (《Change default trace-id format to be
similar to AWS X-Ray (use timestamp )#1947》), this type of traceID information has a timestamp,
and Coprocessor can specify a trace to execute the longest duration to cooperate
with logql start and end 2 information quickly judges.

traceID Coprocessor 2 base tracing system:
If the trace information exists in a certain tracing system, the Coprocessor can query the return result of the traceID
in the tracing system once, and judge whether the logql query is
necessary based on the time distribution in the returned result and the start and end time of logql.

`preGetChunk`,: ...do someThing .
`preGetIndex`,: ...do someThing .
etc.

The IngesterObserver interface provides callbacks for:

`preFlush`, postFlush: ...do someThing .
etc.

## Other Notes

This feature will allow loki to provide a log consumption feature similar to kafka in the future, because we can provide distributor.PostSendIngestSuccess() and ingester.flushChunk().

There is an opportunity for more loki operators to provide more opportunities to participate in development and form some ecological components of loki. Prometehus-like relationship with thanos

The imagination is unlimited, but it may also make loki less focused on low-cost logging systems. So this feature is also very dangerous. 
I noticed before that loki once wanted to be the log exporter of open telemetry, but it has been rejected later. loki expect to focus more on the development of the loki kernel. 
For example, the realization of TSDB Index is a very exciting and good result.
