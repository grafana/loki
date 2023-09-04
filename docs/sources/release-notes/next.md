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

- **Query Language Improvements**: Several improvements to the query language that speed up line parsing and regex matching. [PR #8646](https://github.com/grafana/loki/pull/8646), [PR #8659](https://github.com/grafana/loki/pull/8659), [PR #8724](https://github.com/grafana/loki/pull/8724), [PR #8734](https://github.com/grafana/loki/pull/8734), [PR #8739](https://github.com/grafana/loki/pull/8739), [PR #8763](https://github.com/grafana/loki/pull/8763), [PR #8890](https://github.com/grafana/loki/pull/8890), [PR #8914](https://github.com/grafana/loki/pull/8914)

- **Remote rule evaluation**: Rule evaluation can now be handled by queriers to improve speed. [PR #8744](https://github.com/grafana/loki/pull/8744) [PR #8848](https://github.com/grafana/loki/pull/8848). ([LID #0002]({{< relref "../community/lids/0002-remoteruleevaluation" >}}))

- **Multi-store Index support**: Loki now supports reading/writing indexes to multiple object stores which enables the use of different storage buckets across periods for storing index. [PR #7754](https://github.com/grafana/loki/pull/7754)], [PR #7447](https://github.com/grafana/loki/pull/7447)

- **New volume and volume_range endpoints**: Two new endoints, `index/volume` and `index/volume_range`, have been added to Loki. They return aggregate volume information from the TSDB index for all streams matching a provided stream selector. This feature was introduced via multiple PRs, including [PR #9988](https://github.com/grafana/loki/pull/9988), [PR #9966](https://github.com/grafana/loki/pull/9966), [PR #9833](https://github.com/grafana/loki/pull/9833), [PR #9832](https://github.com/grafana/loki/pull/9832), [PR #9776](https://github.com/grafana/loki/pull/9776), [PR #9762](https://github.com/grafana/loki/pull/9762), [PR #9704](https://github.com/grafana/loki/pull/9704), [PR #10248](https://github.com/grafana/loki/pull/10248), [PR #10099](https://github.com/grafana/loki/pull/10099), [PR #10076](https://github.com/grafana/loki/pull/10076), [PR #10047](https://github.com/grafana/loki/pull/10047) and [PR #10045](https://github.com/grafana/loki/pull/10045)

- **New Storage Client**: Add support for IBM cloud object storage as storage client. [PR #8826](https://github.com/grafana/loki/pull/8826)

- **New Promtail Stage**: Add a Promtail stage for probabilistic sampling. [PR #7127](https://github.com/grafana/loki/pull/7127)

- **Block queries by hash**: Queries can now be blocked by a query hash. [PR #8953](https://github.com/grafana/loki/pull/8953)

- **Deprecations**
  - Legacy index and chunk stores that are not "single store" (such as `tsdb`, `boltdb-shipper`) are deprecated. These storage backends are Cassandra (`cassandra`), DynamoDB (`aws`, `aws-dynamo`), BigTable (`bigtable`, `bigtable-hashed`), GCP (`gcp`, `gcp-columnkey`), and gRPC (`grpc`). See https://grafana.com/docs/loki/latest/storage/ for more information.
  - The `table-manager` target is deprecated, because it is not used by "single store" implementations.
  - The `-boltdb.shipper.compactor.*` CLI flags are deprecated in favor of `-compactor.*`.
  - The `-ingester.unordered-writes` CLI flag is deprecated and will always default to `true` in the next major release.
  - For the full list of deprecations see CHANGELOG.md
