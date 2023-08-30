---
title: V?.?
description: Version ?.? release notes
weight: 100000
draft: true
---

# V?.?
Grafana Labs is excited to announce the release of Loki ?.?. Here's a summary of new enhancements and important fixes:

:warning: This a placeholder for the next release. Clean up all features listed below

## Features and enhancements

-  **Query Language Improvements**: Several improvements to the query language that speed up line parsing and regex matching. [PR #8646](https://github.com/grafana/loki/pull/8646), [PR #8659](https://github.com/grafana/loki/pull/8659), [PR #8724](https://github.com/grafana/loki/pull/8724), [PR #8734](https://github.com/grafana/loki/pull/8734), [PR #8739](https://github.com/grafana/loki/pull/8739), [PR #8763](https://github.com/grafana/loki/pull/8763), [PR #8890](https://github.com/grafana/loki/pull/8890),[PR #8914](https://github.com/grafana/loki/pull/8914)

-  **Multi-store Index support**: Loki now supports reading/writing indexes to multiple object stores which enables the use of different storage buckets across periods for storing index. [PR #7754](https://github.com/grafana/loki/pull/7754)], [PR #7447](https://github.com/grafana/loki/pull/7447)

-  **Track effectiveness of hedged requests** [PR #10281](https://github.com/grafana/loki/pull/10281)]

-  **New volume and volume_range endpoints**: Two new endoints, `index/volume` and `index/volume_range`, have been added to Loki. They return aggregate volume information from the TSDB index for all streams matching a provided stream selector. This feature was introduced via multiple PRs, including [PR #9988](https://github.com/grafana/loki/pull/9988), [PR #9966](https://github.com/grafana/loki/pull/9966), [PR #9833](https://github.com/grafana/loki/pull/9833), [PR #9832](https://github.com/grafana/loki/pull/9832), [PR #9776](https://github.com/grafana/loki/pull/9776), [PR #9762](https://github.com/grafana/loki/pull/9762), [PR #9704](https://github.com/grafana/loki/pull/9704), [PR #10248](https://github.com/grafana/loki/pull/10248), [PR #10099](https://github.com/grafana/loki/pull/10099), [PR #10076](https://github.com/grafana/loki/pull/10076), [PR #10047](https://github.com/grafana/loki/pull/10047) and [PR #10045](https://github.com/grafana/loki/pull/10045)
