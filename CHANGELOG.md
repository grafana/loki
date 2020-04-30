## 1.4.1 (2020-04-06)

We realized after the release last week that piping data into promtail was not working on Linux or Windows, this should fix this issue for both platforms:

* [1893](https://github.com/grafana/loki/pull/1893) **cyriltovena**: Removes file size check for pipe, not provided by linux.

Also thanks to @dottedmag for providing this fix for Fluent Bit!

* [1890](https://github.com/grafana/loki/pull/1890) **dottedmag**: fluentbit: JSON encoding: avoid base64 encoding of []byte inside other slices

## 1.4.0 (2020-04-01)

Over 130 PR's merged for this release, from 40 different contributors!!  We continue to be humbled and thankful for the growing community of contributors and users of Loki.  Thank you all so much.

### Important Notes

**Really, this is important**

Before we get into new features, version 1.4.0 brings with it the first (that we are aware of) upgrade dependency.

We have created a dedicated page for upgrading Loki in the [operations section of the docs](https://github.com/grafana/loki/blob/master/docs/operations/upgrade.md#140)

The docker image tag naming was changed, the starting in 1.4.0 docker images no longer have the `v` prefix: `grafana/loki:1.4.0`

Also you should be aware we are now pruning old `master-xxxxx` docker images from docker hub, currently anything older than 90 days is removed.  **We will never remove released versions of Loki**

### Notable Features

* [1661](https://github.com/grafana/loki/pull/1661) **cyriltovena**: Frontend & Querier query statistics instrumentation.

The API now returns a plethora of stats into the work Loki performed to execute your query, eventually this will be displayed in some form in Grafana to help users better understand how "expensive" their queries are.  Our goal here initially was to better instrument the recent work done in v1.3.0 on query parallelization and to better understand the performance of each part of Loki.  In the future we are looking at additional ideas to provide feedback to users to tailor their queries for better performance.

* [1652](https://github.com/grafana/loki/pull/1652) **cyriltovena**: --dry-run Promtail.
* [1649](https://github.com/grafana/loki/pull/1649) **cyriltovena**: Pipe data to Promtail

This is a long overdue addition to Promtail which can help setup and debug pipelines, with these new features you can do this to feed a single log line into Promtail:

```bash
echo -n 'level=debug msg="test log (200)"' | cmd/promtail/promtail -config.file=cmd/promtail/promtail-local-config.yaml --dry-run -log.level=debug 2>&1 | sed 's/^.*stage/stage/g'
```

`-log.level=debug 2>&1 | sed 's/^.*stage/stage/g` are added to enable debug output, direct the output to stdout, and a sed filter to remove some noise from the log lines.

The `stdin` functionality also works without `--dry-run` allowing you to feed any logs into Promtail via `stdin` and send them to Loki

* [1677](https://github.com/grafana/loki/pull/1677) **owen-d**: Literal Expressions in LogQL
* [1662](https://github.com/grafana/loki/pull/1662) **owen-d**: Binary operators in LogQL

These two extensions to LogQL now let you execute queries like this:

    * `sum(rate({app="foo"}[5m])) * 2` 
    * `sum(rate({app="foo"}[5m]))/1e6` 

* [1678](https://github.com/grafana/loki/pull/1678) **slim-bean**: promtail: metrics pipeline count all log lines

Now you can get per-stream line counts as a metric from promtail, useful for seeing which applications log the most

```yaml
- metrics:
    line_count_total:
      config:
        action: inc
        match_all: true
      description: A running counter of all lines with their corresponding
        labels
      type: Counter
```

* [1558](https://github.com/grafana/loki/pull/1558) **owen-d**: ingester.max-chunk-age
* [1572](https://github.com/grafana/loki/pull/1572) **owen-d**: Feature/query ingesters within

These two configs let you set the max time a chunk can stay in memory in Loki, this is useful to keep memory usage down as well as limit potential loss of data if ingesters crash.  Combine this with the `query_ingesters_within` config and you can have your queriers skip asking the ingesters for data which you know won't still be in memory (older than max_chunk_age).

**NOTE** Do not set the `max_chunk_age` too small, the default of 1h is probably a good point for most people.  Loki does not perform well when you flush many small chunks (such as when your logs have too much cardinality), setting this lower than 1h risks flushing too many small chunks.

* [1581](https://github.com/grafana/loki/pull/1581) **slim-bean**: Add sleep to canary reconnect on error

This isn't a feature but it's an important fix, this is the second time our canaries have tried to DDOS our Loki clusters so you should update to prevent them from trying to attack you.  Aggressive little things these canaries...

* [1840](https://github.com/grafana/loki/pull/1840) **slim-bean**: promtail: Retry 429 rate limit errors from Loki, increase default retry limits
* [1845](https://github.com/grafana/loki/pull/1845) **wardbekker**: throw exceptions on HTTPTooManyRequests and HTTPServerError so FluentD will retry

These two PR's change how 429 HTTP Response codes are handled (Rate Limiting), previously these responses were dropped, now they will be retried for these clients

    * Promtail
    * Docker logging driver
    * Fluent Bit
    * Fluentd

This pushes the failure to send logs to two places. First is the retry limits. The defaults in promtail (and thus also the Docker logging driver and Fluent Bit, which share the same underlying code) will retry 429s (and 500s) on an exponential backoff for up to about 8.5 mins on the default configurations. (This can be changed; see the [config docs](https://github.com/grafana/loki/blob/v1.4.0/docs/clients/promtail/configuration.md#client_config) for more info.)

The second place would be the log file itself. At some point, most log files roll based on size or time. Promtail makes an attempt to read a rolled log file but will only try once. If you are very sensitive to lost logs, give yourself really big log files with size-based rolling rules and increase those retry timeouts. This should protect you from Loki server outages or network issues.

### All Changes

There are many other important fixes and improvements to Loki, way too many to call out in individual detail, so take a look!

#### Loki
* [1810](https://github.com/grafana/loki/pull/1810) **cyriltovena**: Optimize empty filter queries.
* [1809](https://github.com/grafana/loki/pull/1809) **cyriltovena**: Test stats memchunk
* [1807](https://github.com/grafana/loki/pull/1807) **pracucci**: Enable global limits by default in production mixin
* [1802](https://github.com/grafana/loki/pull/1802) **cyriltovena**: Add a test for duplicates count in the heap iterator and fixes it.
* [1799](https://github.com/grafana/loki/pull/1799) **pstibrany**: loki: update Cortex to master
* [1797](https://github.com/grafana/loki/pull/1797) **cyriltovena**: Use ingester client GRPC call options from config.
* [1794](https://github.com/grafana/loki/pull/1794) **pstibrany**: loki: Convert module names to string
* [1793](https://github.com/grafana/loki/pull/1793) **johncming**: pkg/chunkenc: fix leak of pool.
* [1789](https://github.com/grafana/loki/pull/1789) **adityacs**: Fix loki exit on jaeger agent not being present
* [1787](https://github.com/grafana/loki/pull/1787) **cyriltovena**: Regexp simplification
* [1785](https://github.com/grafana/loki/pull/1785) **pstibrany**: Update Cortex to master
* [1758](https://github.com/grafana/loki/pull/1758) **cyriltovena**: Query range should not support date where start == end.
* [1750](https://github.com/grafana/loki/pull/1750) **talham7391**: Clearer error response from push endpoint when labels are malformed
* [1746](https://github.com/grafana/loki/pull/1746) **cyriltovena**: Update cortex vendoring to include frontend status code improvement.
* [1745](https://github.com/grafana/loki/pull/1745) **cyriltovena**: Refactor querier http error handling.
* [1736](https://github.com/grafana/loki/pull/1736) **adityacs**: Add /ready endpoint to table-manager
* [1733](https://github.com/grafana/loki/pull/1733) **cyriltovena**: This logs queries with latency tag when  recording stats.
* [1730](https://github.com/grafana/loki/pull/1730) **adityacs**: Fix nil pointer dereference in ingester client
* [1719](https://github.com/grafana/loki/pull/1719) **cyriltovena**: Expose QueryType function.
* [1718](https://github.com/grafana/loki/pull/1718) **cyriltovena**: Better logql metric status code.
* [1708](https://github.com/grafana/loki/pull/1708) **cyriltovena**: Increase discarded samples when line is too long.
* [1704](https://github.com/grafana/loki/pull/1704) **owen-d**: api support for scalars
* [1686](https://github.com/grafana/loki/pull/1686) **owen-d**: max line lengths (component + tenant overrides)
* [1684](https://github.com/grafana/loki/pull/1684) **cyriltovena**: Ensure status codes are set correctly in the frontend.
* [1677](https://github.com/grafana/loki/pull/1677) **owen-d**: Literal Expressions in LogQL
* [1662](https://github.com/grafana/loki/pull/1662) **owen-d**: Binary operators in LogQL
* [1661](https://github.com/grafana/loki/pull/1661) **cyriltovena**: Frontend & Querier query statistics instrumentation.
* [1651](https://github.com/grafana/loki/pull/1651) **owen-d**: removes duplicate logRangeExprExt grammar
* [1636](https://github.com/grafana/loki/pull/1636) **cyriltovena**: Fixes stats summary computation.
* [1630](https://github.com/grafana/loki/pull/1630) **owen-d**: adds stringer methods for all ast expr types
* [1626](https://github.com/grafana/loki/pull/1626) **owen-d**: compiler guarantees for logql exprs
* [1616](https://github.com/grafana/loki/pull/1616) **owen-d**: cache key cant be reused when an interval changes
* [1615](https://github.com/grafana/loki/pull/1615) **cyriltovena**: Add statistics to query_range and instant_query API.
* [1612](https://github.com/grafana/loki/pull/1612) **owen-d**: bumps cortex to 0.6.1 master
* [1605](https://github.com/grafana/loki/pull/1605) **owen-d**: Decouple logql engine/AST from execution context
* [1582](https://github.com/grafana/loki/pull/1582) **slim-bean**: Change new stats names
* [1579](https://github.com/grafana/loki/pull/1579) **rfratto**: Disable transfers in loki-local-config.yaml
* [1572](https://github.com/grafana/loki/pull/1572) **owen-d**: Feature/query ingesters within
* [1677](https://github.com/grafana/loki/pull/1677) **owen-d**: Introduces numeric literals in LogQL
* [1569](https://github.com/grafana/loki/pull/1569) **owen-d**: refactors splitby to not require buffered channels
* [1567](https://github.com/grafana/loki/pull/1567) **owen-d**: adds span metadata for split queries
* [1565](https://github.com/grafana/loki/pull/1565) **owen-d**: Feature/per tenant splitby
* [1562](https://github.com/grafana/loki/pull/1562) **sandeepsukhani**: limit for concurrent tail requests
* [1558](https://github.com/grafana/loki/pull/1558) **owen-d**: ingester.max-chunk-age
* [1484](https://github.com/grafana/loki/pull/1484) **pstibrany**: loki: use new runtimeconfig package from Cortex

#### Promtail
* [1840](https://github.com/grafana/loki/pull/1840) **slim-bean**: promtail: Retry 429 rate limit errors from Loki, increase default retry limits
* [1775](https://github.com/grafana/loki/pull/1775) **slim-bean**: promtail: remove the read lines counter when the log file stops being tailed
* [1770](https://github.com/grafana/loki/pull/1770) **adityacs**: Fix single job with multiple service discovery elements
* [1765](https://github.com/grafana/loki/pull/1765) **adityacs**: Fix error in templating when extracted key has nil value
* [1743](https://github.com/grafana/loki/pull/1743) **dtennander**: Promtail: Ignore dropped entries in subsequent metric-stages in pipelines.
* [1687](https://github.com/grafana/loki/pull/1687) **adityacs**: Fix panic in labels debug message
* [1683](https://github.com/grafana/loki/pull/1683) **slim-bean**: promtail: auto-prune stale metrics
* [1678](https://github.com/grafana/loki/pull/1678) **slim-bean**: promtail: metrics pipeline count all log lines
* [1666](https://github.com/grafana/loki/pull/1666) **adityacs**: Support entire extracted value map in template pipeline stage
* [1664](https://github.com/grafana/loki/pull/1664) **adityacs**: Support custom prefix name in metrics stage
* [1660](https://github.com/grafana/loki/pull/1660) **rfratto**: pkg/promtail/positions: handle empty positions file
* [1652](https://github.com/grafana/loki/pull/1652) **cyriltovena**: --dry-run Promtail.
* [1649](https://github.com/grafana/loki/pull/1649) **cyriltovena**: Pipe data to Promtail
* [1602](https://github.com/grafana/loki/pull/1602) **slim-bean**: Improve promtail configuration docs

#### Helm
* [1731](https://github.com/grafana/loki/pull/1731) **billimek**: [promtail helm chart] - Expand promtail syslog svc to support values
* [1688](https://github.com/grafana/loki/pull/1688) **fredgate**: Loki stack helm chart can deploy datasources without Grafana
* [1632](https://github.com/grafana/loki/pull/1632) **lukipro**: Added support for imagePullSecrets in Loki Helm chart
* [1620](https://github.com/grafana/loki/pull/1620) **rsteneteg**: [promtail helm chart] option to set fs.inotify.max_user_instances with init container
* [1617](https://github.com/grafana/loki/pull/1617) **billimek**: [promtail helm chart] Enable support for syslog service
* [1590](https://github.com/grafana/loki/pull/1590) **polar3130**: Helm/loki-stack: refresh default grafana.image.tag to 6.6.0
* [1587](https://github.com/grafana/loki/pull/1587) **polar3130**: Helm/loki-stack: add template for the service name to connect to loki
* [1585](https://github.com/grafana/loki/pull/1585) **monotek**: [loki helm chart] added ingress
* [1553](https://github.com/grafana/loki/pull/1553) **got-root**: helm: Allow setting 'loadBalancerSourceRanges' for the loki service
* [1529](https://github.com/grafana/loki/pull/1529) **tourea**: Promtail Helm Chart: Add support for passing environment variables

#### Jsonnet
* [1776](https://github.com/grafana/loki/pull/1776) **Eraac**: fix typo: Not a binary operator: =
* [1767](https://github.com/grafana/loki/pull/1767) **joe-elliott**: Dashboard Cleanup
* [1766](https://github.com/grafana/loki/pull/1766) **joe-elliott**: Move dashboards out into their own json files
* [1757](https://github.com/grafana/loki/pull/1757) **slim-bean**: promtail-mixin: Allow choosing promtail name
* [1756](https://github.com/grafana/loki/pull/1756) **sh0rez**: fix(ksonnet): named parameters for containerPort
* [1749](https://github.com/grafana/loki/pull/1749) **slim-bean**: Increasing the threshold for a file lag and reducing the severity to warning
* [1748](https://github.com/grafana/loki/pull/1748) **slim-bean**: jsonnet: Breakout promtail mixin.
* [1739](https://github.com/grafana/loki/pull/1739) **cyriltovena**: Fixes frontend args in libsonnet.
* [1735](https://github.com/grafana/loki/pull/1735) **cyriltovena**: Allow to configure global limits via the jsonnet deployment.
* [1705](https://github.com/grafana/loki/pull/1705) **cyriltovena**: Add overrides file for our jsonnet library.
* [1699](https://github.com/grafana/loki/pull/1699) **pracucci**: Increased production distributors memory request and limit
* [1689](https://github.com/grafana/loki/pull/1689) **shokada**: Add headers for WebSocket
* [1665](https://github.com/grafana/loki/pull/1665) **cyriltovena**: Query frontend service should be headless.
* [1613](https://github.com/grafana/loki/pull/1613) **cyriltovena**: Fixes config change in the result cache

#### Fluent Bit
* [1791](https://github.com/grafana/loki/pull/1791) **cyriltovena**: Improve fluentbit logfmt.
* [1717](https://github.com/grafana/loki/pull/1717) **adityacs**: Fluent-bit: Fix panic error when AutoKubernetesLabels is true

#### FluentD
* [1811](https://github.com/grafana/loki/pull/1811) **JamesJJ**: Error handling: Show data stream at "debug" level, not "warn"
* [1728](https://github.com/grafana/loki/pull/1728) **irake99**: docs: fix outdated link to fluentd
* [1703](https://github.com/grafana/loki/pull/1703) **Skeen**:  fluent-plugin-grafana-loki: Update fluentd base image to current images (edge)
* [1656](https://github.com/grafana/loki/pull/1656) **takanabe**: Convert second(Integer class) to nanosecond precision
* [1646](https://github.com/grafana/loki/pull/1646) **takanabe**: Fix rubocop violation for fluentd/fluent-plugin-loki
* [1603](https://github.com/grafana/loki/pull/1603) **tarokkk**: fluentd-plugin: add URI validation

#### Docs
* [1781](https://github.com/grafana/loki/pull/1781) **candlerb**: Docs: Recommended schema is now v11
* [1771](https://github.com/grafana/loki/pull/1771) **rfratto**: change slack url to slack.grafana.com and use https
* [1738](https://github.com/grafana/loki/pull/1738) **jgehrcke**: docs: observability.md: clarify lines vs. entries
* [1707](https://github.com/grafana/loki/pull/1707) **dangoodman**: Fix regex in pipeline-example.yml
* [1697](https://github.com/grafana/loki/pull/1697) **oke-py**: fix promtail/templates/NOTES.txt to show correctly port-forward command
* [1675](https://github.com/grafana/loki/pull/1675) **owen-d**: maintainer links & usernames
* [1673](https://github.com/grafana/loki/pull/1673) **cyriltovena**: Add Owen to the maintainer team.
* [1671](https://github.com/grafana/loki/pull/1671) **shokada**: Update tanka.md so that promtail.yml is the correct format
* [1648](https://github.com/grafana/loki/pull/1648) **ShotaKitazawa**: loki-canary: fix indent of DaemonSet manifest written in .md file
* [1642](https://github.com/grafana/loki/pull/1642) **slim-bean**: Improve systemd field docs
* [1641](https://github.com/grafana/loki/pull/1641) **pastatopf**: Correct syntax of rate example
* [1634](https://github.com/grafana/loki/pull/1634) **takanabe**: Unite docs for fluentd plugin
* [1619](https://github.com/grafana/loki/pull/1619) **shaikatz**: PeriodConfig documentation fix dynamodb -> aws-dynamo
* [1611](https://github.com/grafana/loki/pull/1611) **owen-d**: loki frontend docs additions
* [1609](https://github.com/grafana/loki/pull/1609) **Lusitaniae**: Fix wget syntax in documentation
* [1608](https://github.com/grafana/loki/pull/1608) **PabloCastellano**: Documentation: Recommend using the latest schema version (v11)
* [1601](https://github.com/grafana/loki/pull/1601) **rfratto**: Clarify regex escaping rules
* [1598](https://github.com/grafana/loki/pull/1598) **cyriltovena**: Update tanka.md doc.
* [1586](https://github.com/grafana/loki/pull/1586) **MrSaints**: Fix typo in changelog for 1.3.0
* [1504](https://github.com/grafana/loki/pull/1504) **hsraju**: Updated configuration.md

#### Logcli
* [1808](https://github.com/grafana/loki/pull/1808) **slim-bean**: logcli: log the full stats and send to stderr instead of stdout
* [1682](https://github.com/grafana/loki/pull/1682) **adityacs**: BugFix: Fix logcli --quiet parameter parsing issue
* [1644](https://github.com/grafana/loki/pull/1644) **cyriltovena**: This improves the log output for statistics in the logcli.
* [1638](https://github.com/grafana/loki/pull/1638) **owen-d**: adds query stats and org id options in logcli
* [1573](https://github.com/grafana/loki/pull/1573) **cyriltovena**: Improve logql query statistics collection.

#### Loki Canary
* [1653](https://github.com/grafana/loki/pull/1653) **slim-bean**: Canary needs its logo
* [1581](https://github.com/grafana/loki/pull/1581) **slim-bean**: Add sleep to canary reconnect on error

#### Build
* [1780](https://github.com/grafana/loki/pull/1780) **slim-bean**: build: Update the CD deploy task name
* [1762](https://github.com/grafana/loki/pull/1762) **dgzlopes**: Bump testify to 1.5.1
* [1742](https://github.com/grafana/loki/pull/1742) **slim-bean**: build: fix deploy on tagged build
* [1741](https://github.com/grafana/loki/pull/1741) **slim-bean**: add darwin and freebsd binaries to release output
* [1740](https://github.com/grafana/loki/pull/1740) **rfratto**: Fix 32-bit Promtail ARM docker builds from Drone
* [1710](https://github.com/grafana/loki/pull/1710) **adityacs**: Add goimport local-prefixes configuration to .golangci.yml
* [1647](https://github.com/grafana/loki/pull/1647) **mattmendick**: Attempting to add `informational` only feedback for codecov
* [1640](https://github.com/grafana/loki/pull/1640) **rfratto**: ci: print error messages when an API request fails
* [1639](https://github.com/grafana/loki/pull/1639) **rfratto**: ci: prune docker tags prefixed with "master-" older than 90 days
* [1637](https://github.com/grafana/loki/pull/1637) **rfratto**: ci: pin plugins/manifest image tag
* [1633](https://github.com/grafana/loki/pull/1633) **rfratto**: ci: make manifest publishing run in serial
* [1629](https://github.com/grafana/loki/pull/1629) **slim-bean**: Ignore markdown files in codecoverage
* [1628](https://github.com/grafana/loki/pull/1628) **rfratto**: Exempt proposals from stale bot
* [1614](https://github.com/grafana/loki/pull/1614) **mattmendick**: Codecov: Update config to add informational flag
* [1600](https://github.com/grafana/loki/pull/1600) **mattmendick**: Codecov circleci test [WIP]

#### Tooling
* [1577](https://github.com/grafana/loki/pull/1577) **pstibrany**: Move chunks-inspect tool to Loki repo

## 1.3.0 (2020-01-16)

### What's New?? ###

With 1.3.0 we are excited to announce several improvements focusing on performance! 

First and most significant is the Query Frontend:

* [1442](https://github.com/grafana/loki/pull/1442) **cyriltovena**: Loki Query Frontend

The query frontend allows for sharding queries by time and dispatching them in parallel to multiple queriers, giving true horizontal scaling ability for queries.  Take a look at the [jsonnet changes](https://github.com/grafana/loki/pull/1442/files?file-filters%5B%5D=.libsonnet) to see how we are deploying this in our production setup.  Keep an eye out for a blog post with more information on how the frontend works and more information on this exciting new feature.

In our quest to improve query performance, we discovered that gzip, while good for compression ratio, is not the best for speed.  So we introduced the ability to select from several different compression algorithms:

* [1411](https://github.com/grafana/loki/pull/1411) **cyriltovena**: Adds configurable compression algorithms for chunks

We are currently testing out LZ4 and snappy, LZ4 seemed like a good fit however we found that it didn't always compress the same data to the same output which was causing some troubles for another important improvement:

* [1438](https://github.com/grafana/loki/pull/1438) **pstibrany**: pkg/ingester: added sync period flags

Extending on the work done by @bboreham on Cortex, @pstibrany added a few new flags and code to synchronize chunks between ingesters, which reduces the number of chunks persisted to object stores and therefore also reduces the number of chunks loaded on queries and the amount of de-duplication work which needs to be done.  

As mentioned above, LZ4 was in some cases compressing the same data with a different result which was interfering with this change, we are still investigating the cause of this issue (It may be in how we implemented something, or may be in the compression code itself).  For now we have switched to snappy which has seen a reduction in data written to the object store from almost 3x the source data (with a replication factor of 3) to about 1.5x, saving a lot of duplicated log storage!

Another valuable change related to chunks:

* [1406](https://github.com/grafana/loki/pull/1406) **slim-bean**: allow configuring a target chunk size in compressed bytes

With this change you can set a `chunk_target_size` and Loki will attempt to fill a chunk to approx that size before flushing (previously a chunk size was a hard coded 10 blocks where the default block size is 262144 bytes).  Larger chunks are beneficial for a few reasons, mainly on reducing API calls to your object store when performing queries, but also in reducing overhead in a few places, especially when processing very high volume log streams.

Another big improvement is the introduction of accurate rate limiting when running microservices:

* [1486](https://github.com/grafana/loki/pull/1486) **pracucci**: Add ingestion rate global limit support

Previously the rate limit was applied at each distributor, however with traffic split over many distributors the limit would need to be adjusted accordingly.  This meant that scaling up distributors required changing the limit.  Now this information is communicated between distributors such that the limit should be applied accurately regardless of the number of distributors.

And last but not least on the notable changes list is a new feature for Promtail:

* [1275](https://github.com/grafana/loki/pull/1275) **bastjan**: pkg/promtail: IETF Syslog (RFC5424) Support

With this change Promtail can receive syslogs via TCP!  Thanks to @bastjan for all the hard work on this submission!

### Important things to note:

* [1519](https://github.com/grafana/loki/pull/1519) Changes a core behavior in Loki regarding logs with duplicate content AND duplicate timestamps, previously Loki would store logs with duplicate timestamps and content, moving forward logs with duplicate content AND timestamps will be silently ignored.  Mainly this change is to prevent duplicates that appear when a batch is retried (the first entry in the list would be inserted again, now it will be ignored).  Logs with the same timestamp and different content will still be accepted.
* [1486](https://github.com/grafana/loki/pull/1486) Deprecated `-distributor.limiter-reload-period` flag / distributor's `limiter_reload_period` config option.

### All Changes

Once again we can't thank our community and contributors enough for the significant work that everyone is adding to Loki, the entire list of changes is long!!

#### Loki
* [1526](https://github.com/grafana/loki/pull/1526) **codesome**: Support <selector> <range> <filters> for aggregation
* [1522](https://github.com/grafana/loki/pull/1522) **cyriltovena**: Adds support for the old query string regexp in the frontend.
* [1519](https://github.com/grafana/loki/pull/1519) **rfratto**: pkg/chunkenc: ignore duplicate lines pushed to a stream
* [1511](https://github.com/grafana/loki/pull/1511) **sandlis**: querier: fix panic in tailer when max tail duration exceeds
* [1499](https://github.com/grafana/loki/pull/1499) **slim-bean**: Fix a panic in chunk prefetch
* [1495](https://github.com/grafana/loki/pull/1495) **slim-bean**: Prefetch chunks while processing
* [1496](https://github.com/grafana/loki/pull/1496) **cyriltovena**: Add duplicates info and remove timing informations.
* [1490](https://github.com/grafana/loki/pull/1490) **owen-d**: Fix/deadlock frontend queue
* [1489](https://github.com/grafana/loki/pull/1489) **owen-d**: unifies reverse iterators
* [1488](https://github.com/grafana/loki/pull/1488) **cyriltovena**: Fixes response json encoding and add regression tests.
* [1486](https://github.com/grafana/loki/pull/1486) **pracucci**: Add ingestion rate global limit support* [1493](https://github.com/grafana/loki/pull/1493) **pracucci**: Added max streams per user global limit
* [1480](https://github.com/grafana/loki/pull/1480) **cyriltovena**: Close iterator properly and check nil before releasing buffers.
* [1473](https://github.com/grafana/loki/pull/1473) **rfratto**: pkg/querier: don't query all ingesters
* [1470](https://github.com/grafana/loki/pull/1470) **cyriltovena**: Validates limit parameter.
* [1448](https://github.com/grafana/loki/pull/1448) **cyriltovena**: Improving storage benchmark
* [1445](https://github.com/grafana/loki/pull/1445) **cyriltovena**: Add decompression tracing instrumentation.
* [1442](https://github.com/grafana/loki/pull/1442) **cyriltovena**: Loki Query Frontend
* [1438](https://github.com/grafana/loki/pull/1438) **pstibrany**: pkg/ingester: added sync period flags
* [1433](https://github.com/grafana/loki/pull/1433) **zendern**: Using strict parsing for yaml configs
* [1425](https://github.com/grafana/loki/pull/1425) **pstibrany**: pkg/ingester: Added possibility to disable transfers.
* [1423](https://github.com/grafana/loki/pull/1423) **pstibrany**: pkg/chunkenc: Fix BenchmarkRead to focus on reading chunks, not converting bytes to string
* [1421](https://github.com/grafana/loki/pull/1421) **pstibrany**: pkg/chunkenc: change default LZ4 buffer size to 64k.
* [1420](https://github.com/grafana/loki/pull/1420) **cyriltovena**: Sets the chunk encoding correctly when creating chunk from bytes.
* [1419](https://github.com/grafana/loki/pull/1419) **owen-d**: Enables Series API in loki
* [1413](https://github.com/grafana/loki/pull/1413) **pstibrany**: RangeQuery benchmark optimizations
* [1411](https://github.com/grafana/loki/pull/1411) **cyriltovena**: Adds configurable compression algorithms for chunks
* [1409](https://github.com/grafana/loki/pull/1409) **slim-bean**: change the chunk size histogram to allow for bigger buckets
* [1408](https://github.com/grafana/loki/pull/1408) **slim-bean**: forgot to register the new metric for counting blocks per chunk
* [1406](https://github.com/grafana/loki/pull/1406) **slim-bean**: allow configuring a target chunk size in compressed bytes
* [1405](https://github.com/grafana/loki/pull/1405) **pstibrany**: Convert string to bytes once only when doing string filtering.
* [1396](https://github.com/grafana/loki/pull/1396) **pstibrany**: pkg/cfg: print help only when requested, and print it on stdout
* [1383](https://github.com/grafana/loki/pull/1383) **beornf**: Read websocket close in tail handler
* [1071](https://github.com/grafana/loki/pull/1071) **rfratto**: pkg/ingester: limit total number of errors a stream can return on push
* [1545](https://github.com/grafana/loki/pull/1545) **joe-elliott**: Critical n => m conversions
* [1541](https://github.com/grafana/loki/pull/1541) **owen-d**: legacy endpoint 400s metric queries

#### Promtail
* [1515](https://github.com/grafana/loki/pull/1515) **slim-bean**: Promtail: Improve position and size metrics
* [1485](https://github.com/grafana/loki/pull/1485) **p37ruh4**: Fileglob parsing fixes
* [1472](https://github.com/grafana/loki/pull/1472) **owen-d**: positions.ignore-corruptions
* [1453](https://github.com/grafana/loki/pull/1453) **chancez**: pkg/promtail: Initialize counters to 0 when creating client
* [1436](https://github.com/grafana/loki/pull/1436) **rfratto**: promtail: add support for passing through journal entries as JSON
* [1426](https://github.com/grafana/loki/pull/1426) **wphan**: Support microsecond timestamp format
* [1416](https://github.com/grafana/loki/pull/1416) **pstibrany**: pkg/promtail/client: missing URL in client returns error
* [1275](https://github.com/grafana/loki/pull/1275) **bastjan**: pkg/promtail: IETF Syslog (RFC5424) Support

#### Fluent Bit
* [1455](https://github.com/grafana/loki/pull/1455) **JensErat**: fluent-bit-plugin: re-enable failing JSON marshaller tests; pass error instead of logging and ignoring
* [1294](https://github.com/grafana/loki/pull/1294) **JensErat**: fluent-bit: multi-instance support
* [1514](https://github.com/grafana/loki/pull/1514) **shane-axiom**: fluent-plugin-grafana-loki: Add `fluentd_thread` label when `flush_thread_count` > 1

#### FluentD
* [1500](https://github.com/grafana/loki/pull/1500) **cyriltovena**: Bump fluentd plugin to 1.2.6.
* [1475](https://github.com/grafana/loki/pull/1475) **Horkyze**: fluentd-plugin: call gsub for strings only

#### Docker Driver
* [1414](https://github.com/grafana/loki/pull/1414) **cyriltovena**: Adds tenant-id for docker driver.

#### Logcli
* [1492](https://github.com/grafana/loki/pull/1492) **sandlis**: logcli: replaced GRAFANA_* with LOKI_* in logcli env vars, set default server url for logcli to localhost

#### Helm
* [1534](https://github.com/grafana/loki/pull/1534) **olivierboudet**: helm : fix fluent-bit parser configuration syntax
* [1506](https://github.com/grafana/loki/pull/1506) **terjesannum**: helm: add podsecuritypolicy for fluent-bit
* [1431](https://github.com/grafana/loki/pull/1431) **eugene100**: Helm: fix issue with config.clients
* [1430](https://github.com/grafana/loki/pull/1430) **olivierboudet**: helm : allow to define custom parsers to use with fluentbit.io/parser annotation
* [1418](https://github.com/grafana/loki/pull/1418) **evalsocket**: Helm chart url added in helm.md
* [1336](https://github.com/grafana/loki/pull/1336) **terjesannum**: helm: support adding init containers to the loki pod
* [1530](https://github.com/grafana/loki/pull/1530) **WeiBanjo**: Allow extra command line args for external labels like hostname

#### Jsonnet
* [1518](https://github.com/grafana/loki/pull/1518) **benjaminhuo**: Fix error 'Field does not exist: jaeger_mixin' in tk show
* [1501](https://github.com/grafana/loki/pull/1501) **anarcher**: jsonnet: fix common/defaultPorts parameters
* [1497](https://github.com/grafana/loki/pull/1497) **cyriltovena**: Update Loki mixin to include frontend QPS and latency.
* [1478](https://github.com/grafana/loki/pull/1478) **cyriltovena**: Fixes the typo in the result cache config of the Loki ksonnet lib.
* [1543](https://github.com/grafana/loki/pull/1543) **sh0rez**: fix(ksonnet): use apps/v1

#### Docs
* [1531](https://github.com/grafana/loki/pull/1531) **fitzoh**: Documentation: Add note on using Loki with Amazon ECS
* [1521](https://github.com/grafana/loki/pull/1521) **rfratto**: docs: Document timestamp ordering rules
* [1516](https://github.com/grafana/loki/pull/1516) **rfratto**: Link to release docs in README.md, not master docs
* [1508](https://github.com/grafana/loki/pull/1508) **cyriltovena**: Fixes bad json in Loki API documentation.
* [1505](https://github.com/grafana/loki/pull/1505) **sandlis**: doc: fix sample yaml in docs for installing promtail to k8s
* [1481](https://github.com/grafana/loki/pull/1481) **terjesannum**: docs: fix broken promtail link
* [1474](https://github.com/grafana/loki/pull/1474) **Eraac**: <doc>: information about max_look_back_period
* [1471](https://github.com/grafana/loki/pull/1471) **cyriltovena**: Update README.md
* [1466](https://github.com/grafana/loki/pull/1466) **Eraac**: <documentation>: Update IAM requirement
* [1441](https://github.com/grafana/loki/pull/1441) **vtereso**: <Docs>: README spelling fix
* [1437](https://github.com/grafana/loki/pull/1437) **daixiang0**: fix all misspell
* [1432](https://github.com/grafana/loki/pull/1432) **joe-elliott**: Removed unsupported encodings from docs
* [1399](https://github.com/grafana/loki/pull/1399) **vishesh92**: Docs: Add configuration docs for redis
* [1394](https://github.com/grafana/loki/pull/1394) **chancez**: Documentation: Fix example AWS storage configuration
* [1227](https://github.com/grafana/loki/pull/1227) **daixiang0**: Add docker install doc
* [1560](https://github.com/grafana/loki/pull/1560) **robshep**: Promtail Docs: Update output.md
* [1546](https://github.com/grafana/loki/pull/1546) **mattmendick**: Removing third-party link
* [1539](https://github.com/grafana/loki/pull/1539) **j18e**: docs: fix syntax error in pipeline example

#### Build
* [1494](https://github.com/grafana/loki/pull/1494) **pracucci**: Fixed TOUCH_PROTOS in all DroneCI pipelines
* [1479](https://github.com/grafana/loki/pull/1479) **owen-d**: TOUCH_PROTOS build arg for dockerfile
* [1476](https://github.com/grafana/loki/pull/1476) **owen-d**: initiates docker daemon for circle windows builds
* [1469](https://github.com/grafana/loki/pull/1469) **rfratto**: Makefile: re-enable journal scraping on ARM

#### New Members!
* [1415](https://github.com/grafana/loki/pull/1415) **cyriltovena**: Add Joe as member of the team.

# 1.2.0 (2019-12-09)

One week has passed since the last Loki release, and it's time for a new one!

## Notable Changes

We have continued our work making our API Prometheus-compatible. The key
changes centered around API compatibility are:

* [1370](https://github.com/grafana/loki/pull/1370) **slim-bean**: Change `/loki/api/v1/label` to `loki/api/v1/labels`
* [1381](https://github.com/grafana/loki/pull/1381) **owen-d**: application/x-www-form-urlencoded support

Meanwhile, @pstibrany has done great work ensuring that Loki handles hash
collisions properly:

* [1247](https://github.com/grafana/loki/pull/1247) **pstibrany**: pkg/ingester: handle labels mapping to the same fast fingerprint.

## Other Changes

:heart: All PR's are important to us, thanks everyone for continuing to help support and improve Loki! :heart:

### Features

* [1372](https://github.com/grafana/loki/pull/1372) **cyriltovena**: Let Loki start when using the debug image.
* [1300](https://github.com/grafana/loki/pull/1300) **pstibrany**: pkg/ingester: check that ingester is in LEAVING state when transferring chunks and claiming tokens. Required when using memberlist client.

### Bug Fixes/Improvements

* [1376](https://github.com/grafana/loki/pull/1376) **jstaffans**: Fluentd: guard against nil values when sanitizing labels
* [1371](https://github.com/grafana/loki/pull/1371) **cyriltovena**: Logql benchmark and performance improvement.
* [1363](https://github.com/grafana/loki/pull/1363) **cyriltovena**: Fixes fluentd new push path API.
* [1353](https://github.com/grafana/loki/pull/1353) **pstibrany**: docs: Fix grpc_listen_host and http_listen_host.
* [1350](https://github.com/grafana/loki/pull/1350) **Eraac**: documentation: iam requirement for autoscaling

# 1.1.0 (2019-12-04)

It's been a busy 2 weeks since the 1.0.0 release and quite a few important PR's have been merged to Loki.

The most significant:

* [1322](https://github.com/grafana/loki/pull/1322) **rfratto**: Fix v1 label API to be Prometheus-compatible

Some might call this a **breaking change**, we are instead calling it a bug fix as our goal was to be prometheus compatible and we were not :smiley:

**But please be aware if you are using the `/loki/api/v1/label` or `/loki/api/v1/label/<name>/values` the JSON result will be different in 1.1.0**

Old result:
```json
{
  "values": [
    "label1",
    "label2",
    "labeln"
  ]
}
```
New result:

```json
{
  "status": "success",
  "data": [
    "label1",
    "label2",
    "labeln"
  ]
}
```

**ALSO IMPORTANT**

* [1160](https://github.com/grafana/loki/pull/1160) **daixiang0**: replace gzip with zip

Binaries will now be zipped instead of gzipped as many people voiced their opinion that zip is likely to be installed on more systems by default.

**If you had existing automation to download and install binaries this will have to be updated to use zip instead of gzip**

## Notable Fixes and Improvements

* Broken version info in startup log message:

    [1095](https://github.com/grafana/loki/pull/1095) **pstibrany**: Makefile changes to allow easy builds with or without vendoring. Also fixes version bug for both cases.

* The hashing algorithm used to calculate the hash for a stream was creating hash collisions in some instances.
**Please Note** this is just one part of the fix and is only in Promtail, the second part for Loki can be tracked [in PR1247](https://github.com/grafana/loki/pull/1247) which didn't quite make the cut for 1.1.0 and will be in 1.2.0:

    [1254](https://github.com/grafana/loki/pull/1254) **pstibrany**: pkg/promtail/client: Handle fingerprint hash collisions

* Thank you @putrasattvika for finding and fixing an important bug where logs were some logs were missed in a query shortly after a flush!

    [1299](https://github.com/grafana/loki/pull/1299) **putrasattvika**: storage: fix missing logs with batched chunk iterator

* Thank you @danieldabate for helping to again improve our API to be more Prometheus compatible:

    [1355](https://github.com/grafana/loki/pull/1355) **danieldabate**: HTTP API: Support duration and float formats for step parameter

* LogQL will support duration formats that are not typically handled by Go like [1d] or [1w]

    [1357](https://github.com/grafana/loki/pull/1357) **cyriltovena**: Supports same duration format in LogQL as Prometheus


## Everything Else

:heart: All PR's are important to us, thanks everyone for continuing to help support and improve Loki! :heart:

* [1349](https://github.com/grafana/loki/pull/1349) **Eraac**: documentation: using parsable value in example
* [1343](https://github.com/grafana/loki/pull/1343) **dgzlopes**: doc(configuration): Fix duration format.
* [1342](https://github.com/grafana/loki/pull/1342) **whothey**: Makefile: add debug symbols to loki and promtail debug builds
* [1341](https://github.com/grafana/loki/pull/1341) **adamjohnson01**: Update loki helm chart to support service account annotations
* [1340](https://github.com/grafana/loki/pull/1340) **adamjohnson01**: Pull in cortex changes to support IAM roles for EKS
* [1339](https://github.com/grafana/loki/pull/1339) **cyriltovena**: Update gem version.
* [1333](https://github.com/grafana/loki/pull/1333) **daixiang0**: fix broken link
* [1328](https://github.com/grafana/loki/pull/1328) **cyriltovena**: Fixes linter warning from the yacc file.
* [1326](https://github.com/grafana/loki/pull/1326) **dawidmalina**: Wrong api endpoint in fluent-plugin-grafana-loki
* [1320](https://github.com/grafana/loki/pull/1320) **roidelapluie**: Metrics: use Namespace everywhere when declaring metrics
* [1318](https://github.com/grafana/loki/pull/1318) **roidelapluie**: Use tenant as label name for discarded_samples metrics
* [1317](https://github.com/grafana/loki/pull/1317) **roidelapluie**: Expose discarded bytes metric
* [1316](https://github.com/grafana/loki/pull/1316) **slim-bean**: Removing old file needed for dep (no longer needed)
* [1312](https://github.com/grafana/loki/pull/1312) **ekeih**: Docs: Add missing ) in LogQL example
* [1311](https://github.com/grafana/loki/pull/1311) **pstibrany**: Include positions filename in the error when YAML unmarshal fails.
* [1310](https://github.com/grafana/loki/pull/1310) **JensErat**: fluent-bit: sorted JSON and properly convert []byte to string
* [1304](https://github.com/grafana/loki/pull/1304) **pstibrany**: promtail: write positions to new file first, move to target location afterwards
* [1303](https://github.com/grafana/loki/pull/1303) **zhangjianweibj**: https://github.com/grafana/loki/issues/1302
* [1298](https://github.com/grafana/loki/pull/1298) **rfratto**: pkg/promtail: remove journal target forced path
* [1279](https://github.com/grafana/loki/pull/1279) **rfratto**: Fix loki_discarded_samples_total metric
* [1278](https://github.com/grafana/loki/pull/1278) **rfratto**: docs: update limits_config to new structure from #948
* [1276](https://github.com/grafana/loki/pull/1276) **roidelapluie**: Update fluentbit README.md based on my experience
* [1274](https://github.com/grafana/loki/pull/1274) **sh0rez**: chore(ci): drone-cli
* [1273](https://github.com/grafana/loki/pull/1273) **JensErat**: fluent-bit: tenant ID configuration
* [1266](https://github.com/grafana/loki/pull/1266) **polar3130**: add description about tenant stage
* [1262](https://github.com/grafana/loki/pull/1262) **Eraac**: documentation: iam requirement for autoscaling
* [1261](https://github.com/grafana/loki/pull/1261) **rfratto**: Document systemd journal scraping
* [1249](https://github.com/grafana/loki/pull/1249) **cyriltovena**: Move to jsoniter instead of default json package
* [1223](https://github.com/grafana/loki/pull/1223) **jgehrcke**: authentication.md: replace "user" with "tenant"
* [1204](https://github.com/grafana/loki/pull/1204) **allanhung**: fluent-bit-plugin: Auto add Kubernetes labels to Loki labels



# 1.0.0 (2019-11-19)

:tada: Nearly a year since Loki was announced at KubeCon in Seattle 2018 we are very excited to announce the 1.0.0 release of Loki! :tada:

A lot has happened since the announcement, the project just recently passed 1000 commits by 138 contributors over 700+ PR's accumulating over 7700 GitHub stars!

Internally at Grafana Labs we have been using Loki to monitor all of our infrastructure and ingest around 1.5TB/10 billion log lines a day. Since the v0.2.0 release we have found Loki to be reliable and stable in our environments.

We are comfortable with the state of the project in our production environments and think it's time to promote Loki to a non-beta release to communicate to everyone that they should feel comfortable using Loki in their production environments too.

## API Stability

With the 1.0.0 release our intent is to try to follow Semver rules regarding stability with some aspects of Loki, focusing mainly on the operating experience of Loki as an application.  That is to say we are not planning any major changes to the HTTP API, and anything breaking would likely be accompanied by a major release with backwards compatibility support.

We are currently NOT planning on maintaining Go API stability with this release, if you are importing Loki as a library you should be prepared for any kind of change, including breaking, even in minor or bugfix releases.

Loki is still a young and active project and there might be some breaking config changes in non-major releases, rest assured this will be clearly communicated and backwards or overlapping compatibility will be provided if possible.

## Changes

There were not as many changes in this release as the last, mainly we wanted to make sure Loki was mostly stable before 1.0.0.  The most notable change is the inclusion of the V11 schema in PR's [1201](https://github.com/grafana/loki/pull/1201) and [1280](https://github.com/grafana/loki/pull/1280).  The V11 schema adds some more data to the index to improve label queries over large amounts of time and series.  Currently we have not updated the Helm or Ksonnet to use the new schema, this will come soon with more details on how it works.

The full list of changes:

* [1280](https://github.com/grafana/loki/pull/1280) **owen-d**: Fix duplicate labels (update cortex)
* [1260](https://github.com/grafana/loki/pull/1260) **rfratto**: pkg/loki: unmarshal module name from YAML
* [1257](https://github.com/grafana/loki/pull/1257) **rfratto**: helm: update default terminationGracePeriodSeconds to 4800
* [1251](https://github.com/grafana/loki/pull/1251) **obitech**: docs: Fix promtail releases download link
* [1248](https://github.com/grafana/loki/pull/1248) **rfratto**: docs: slightly modify language in community Loki packages section
* [1242](https://github.com/grafana/loki/pull/1242) **tarokkk**: fluentd: Suppress unread configuration warning
* [1239](https://github.com/grafana/loki/pull/1239) **pracucci**: Move ReservedLabelTenantID out from a dedicated file
* [1238](https://github.com/grafana/loki/pull/1238) **oke-py**: helm: loki-stack supports k8s 1.16
* [1237](https://github.com/grafana/loki/pull/1237) **joe-elliott**: Rollback google.golang.org/api to 0.8.0
* [1235](https://github.com/grafana/loki/pull/1235) **woodsaj**: ci: update triggers to use new deployment_tools location
* [1234](https://github.com/grafana/loki/pull/1234) **rfratto**: Standardize schema used in `match` stage
* [1233](https://github.com/grafana/loki/pull/1233) **wapmorgan**: Update docker-driver Dockerfile: add tzdb
* [1232](https://github.com/grafana/loki/pull/1232) **rfratto**: Fix drone deploy job
* [1231](https://github.com/grafana/loki/pull/1231) **joe-elliott**: Removed references to Loki free tier
* [1226](https://github.com/grafana/loki/pull/1226) **clickyotomy**: Update dependencies to use weaveworks/common upstream
* [1221](https://github.com/grafana/loki/pull/1221) **slim-bean**: use regex label matcher to not alert on any tail route latencies
* [1219](https://github.com/grafana/loki/pull/1219) **MightySCollins**: docs: Updated Kubernetes docs links in Helm charts
* [1218](https://github.com/grafana/loki/pull/1218) **slim-bean**: update dashboards to include the new /loki/api/v1/* endpoints
* [1217](https://github.com/grafana/loki/pull/1217) **slim-bean**: sum the bad words by name and level
* [1216](https://github.com/grafana/loki/pull/1216) **joe-elliott**: Remove rules that reference no longer existing metrics
* [1215](https://github.com/grafana/loki/pull/1215) **Eraac**: typo url
* [1214](https://github.com/grafana/loki/pull/1214) **takanabe**: Correct wrong document paths about querying
* [1213](https://github.com/grafana/loki/pull/1213) **slim-bean**: Fix docker latest and master tags
* [1212](https://github.com/grafana/loki/pull/1212) **joe-elliott**: Update loki operational
* [1206](https://github.com/grafana/loki/pull/1206) **sandlis**: ksonnet: fix replication always set to 3 in ksonnet
* [1203](https://github.com/grafana/loki/pull/1203) **joe-elliott**: Chunk iterator performance improvement
* [1202](https://github.com/grafana/loki/pull/1202) **beorn7**: Simplify regexp's
* [1201](https://github.com/grafana/loki/pull/1201) **cyriltovena**: Update cortex to bring v11 schema
* [1189](https://github.com/grafana/loki/pull/1189) **putrasattvika**: fluent-plugin: Add client certificate verification
* [1186](https://github.com/grafana/loki/pull/1186) **tarokkk**: fluentd: Refactor label_keys and and add extract_kubernetes_labels configuration

# 0.4.0 (2019-10-24)

A **huge** thanks to the **36 contributors** who submitted **148 PR's** since 0.3.0!

## Notable Changes

* With PR [654](https://github.com/grafana/loki/pull/654) @cyriltovena added a really exciting new capability to Loki, a Prometheus compatible API with support for running metric style queries against your logs! [Take a look at how to write metric queries for logs](https://github.com/grafana/loki/blob/master/docs/logql.md#counting-logs)
    > PLEASE NOTE: To use metric style queries in the current Grafana release 6.4.x you will need to add Loki as a Prometheus datasource in addition to having it as a Log datasource and you will have to select the correct source for querying logs vs metrics, coming soon Grafana will support both logs and metric queries directly to the Loki datasource!
* PR [1022](https://github.com/grafana/loki/pull/1022) (and a few others) @joe-elliott added a new set of HTTP endpoints in conjunction with the work @cyriltovena to create a Prometheus compatible API as well as improve how labels/timestamps are handled
    > IMPORTANT: The new `/api/v1/*` endpoints contain breaking changes on the query paths (push path is unchanged) Eventually the `/api/prom/*` endpoints will be removed
* PR [847](https://github.com/grafana/loki/pull/847) owes a big thanks to @cosmo0920 for contributing his Fluent Bit go plugin, now loki has Fluent Bit plugin support!!

* PR [982](https://github.com/grafana/loki/pull/982) was a couple weeks of painstaking work by @rfratto for a much needed improvement to Loki's docs! [Check them out!](https://github.com/grafana/loki/tree/master/docs)

* PR [980](https://github.com/grafana/loki/pull/980) by @sh0rez improved how flags and config file's are loaded to honor a more traditional order of precedence:
    1. Defaults
    2. Config file
    3. User-supplied flag values (command line arguments)
    > PLEASE NOTE: This is potentially a breaking change if you were passing command line arguments that also existed in a config file in which case the order they are given priority now has changed!

* PR [1062](https://github.com/grafana/loki/pull/1062) and [1089](https://github.com/grafana/loki/pull/1089) have moved Loki from Dep to Go Modules and to Go 1.13


## Loki

### Features/Improvements/Changes

* **Loki** [1171](https://github.com/grafana/loki/pull/1171) **cyriltovena**: Moves request parsing into the loghttp package
* **Loki** [1145](https://github.com/grafana/loki/pull/1145) **joe-elliott**: Update `/loki/api/v1/push` to use the v1 json format
* **Loki** [1128](https://github.com/grafana/loki/pull/1128) **sandlis**: bigtable-backup: list backups just before starting deletion of wanted backups
* **Loki** [1100](https://github.com/grafana/loki/pull/1100) **sandlis**: logging: removed some noise in logs from live-tailing
* **Loki/build** [1089](https://github.com/grafana/loki/pull/1089) **joe-elliott**: Go 1.13
* **Loki** [1088](https://github.com/grafana/loki/pull/1088) **pstibrany**: Updated cortex to latest master.
* **Loki** [1085](https://github.com/grafana/loki/pull/1085) **pracucci**: Do not retry chunks transferring on shutdown in the local dev env
* **Loki** [1084](https://github.com/grafana/loki/pull/1084) **pracucci**: Skip ingester tailer filtering if no filter is set
* **Loki/build**[1062](https://github.com/grafana/loki/pull/1062) **joe-elliott**: dep => go mod
* **Loki** [1049](https://github.com/grafana/loki/pull/1049) **joe-elliott**: Update loki push path
* **Loki** [1044](https://github.com/grafana/loki/pull/1044) **joe-elliott**: Fixed broken logql request filtering
* **Loki/tools** [1043](https://github.com/grafana/loki/pull/1043) **sandlis**: bigtable-backup: use latest bigtable backup docker image with fix for list backups
* **Loki** [1030](https://github.com/grafana/loki/pull/1030) **polar3130**: fix typo in error messages
* **Loki/tools** [1028](https://github.com/grafana/loki/pull/1028) **sandlis**: bigtable-backup: verify backups to work on latest list of backups
* **Loki** [1022](https://github.com/grafana/loki/pull/1022) **joe-elliott**: Loki HTTP/JSON Model Layer
* **Loki** [1016](https://github.com/grafana/loki/pull/1016) **slim-bean**: Revert "Updated stream json objects to be more parse friendly (#1010)"
* **Loki** [1010](https://github.com/grafana/loki/pull/1010) **joe-elliott**: Updated stream json objects to be more parse friendly
* **Loki** [1009](https://github.com/grafana/loki/pull/1009) **cyriltovena**: Make Loki HTTP API more compatible with Prometheus
* **Loki** [1008](https://github.com/grafana/loki/pull/1008) **wardbekker**: Improved Ingester out-of-order error for faster troubleshooting
* **Loki** [1001](https://github.com/grafana/loki/pull/1001) **slim-bean**: Update new API paths
* **Loki** [998](https://github.com/grafana/loki/pull/998) **sandlis**: Change unit of duration params to hours to align it with duration config at other places in Loki
* **Loki** [980](https://github.com/grafana/loki/pull/980) **sh0rez**: feat: configuration source precedence
* **Loki** [948](https://github.com/grafana/loki/pull/948) **sandlis**: limits: limits implementation for loki
* **Loki** [947](https://github.com/grafana/loki/pull/947) **sandlis**: added a variable for storing periodic table duration as an int to be …
* **Loki** [938](https://github.com/grafana/loki/pull/938) **sandlis**: vendoring: update cortex to latest master
* **Loki/tools** [930](https://github.com/grafana/loki/pull/930) **sandlis**: fix incrementing of bigtable_backup_job_backups_created metric
* **Loki/tools** [920](https://github.com/grafana/loki/pull/920) **sandlis**: bigtable-backup tool fix
* **Loki/tools** [895](https://github.com/grafana/loki/pull/895) **sandlis**: bigtable-backup-tool: Improvements
* **Loki** [755](https://github.com/grafana/loki/pull/755) **sandlis**: Use grpc client config from cortex for Ingester to get more control
* **Loki** [654](https://github.com/grafana/loki/pull/654) **cyriltovena**: LogQL: Vector and Range Vector Aggregation.

### Bug Fixes
* **Loki** [1114](https://github.com/grafana/loki/pull/1114) **rfratto**: pkg/ingester: prevent shutdowns from processing during joining handoff
* **Loki** [1097](https://github.com/grafana/loki/pull/1097) **joe-elliott**: Reverted cloud.google.com/go to 0.44.1
* **Loki** [986](https://github.com/grafana/loki/pull/986) **pracucci**: Fix panic in tailer due to race condition between send() and close()
* **Loki** [975](https://github.com/grafana/loki/pull/975) **sh0rez**: fix(distributor): parseError BadRequest
* **Loki** [944](https://github.com/grafana/loki/pull/944) **rfratto**: pkg/querier: fix concurrent access to querier tail clients

## Promtail

### Features/Improvements/Changes

* **Promtail/pipeline** [1179](https://github.com/grafana/loki/pull/1179) **pracucci**: promtail: fix handling of JMESPath expression returning nil while parsing JSON
* **Promtail/pipeline** [1123](https://github.com/grafana/loki/pull/1123) **pracucci**: promtail: added action_on_failure support to timestamp stage
* **Promtail/pipeline** [1122](https://github.com/grafana/loki/pull/1122) **pracucci**: promtail: initialize extracted map with initial labels
* **Promtail/pipeline** [1112](https://github.com/grafana/loki/pull/1112) **cyriltovena**: Add logql filter to match stages and drop capability
* **Promtail/journal** [1109](https://github.com/grafana/loki/pull/1109) **rfratto**: Clarify journal warning
* **Promtail** [1083](https://github.com/grafana/loki/pull/1083) **pracucci**: Increased promtail's backoff settings in prod and improved doc
* **Promtail** [1026](https://github.com/grafana/loki/pull/1026) **erwinvaneyk**: promtail: fix externalURL and path prefix issues
* **Promtail** [976](https://github.com/grafana/loki/pull/976) **slim-bean**: Wrap debug log statements in conditionals to save allocations
* **Promtail** [973](https://github.com/grafana/loki/pull/973) **ctrox**: tests: Set default value for BatchWait as ticker does not accept 0
* **Promtail** [969](https://github.com/grafana/loki/pull/969) **ctrox**: promtail: Use ticker instead of timer for batch wait
* **Promtail** [952](https://github.com/grafana/loki/pull/952) **pracucci**: promtail: add metrics on sent and dropped log entries
* **Promtail** [934](https://github.com/grafana/loki/pull/934) **pracucci**: promtail: do not send the last batch - to ingester - if empty
* **Promtail** [921](https://github.com/grafana/loki/pull/921) **rfratto**: promtail: add "max_age" field to configure cutoff for journal reading
* **Promtail** [883](https://github.com/grafana/loki/pull/883) **adityacs**: Add pipeline unit testing to promtail

### Bugfixes

* **Promtail** [1194](https://github.com/grafana/loki/pull/1194) **slim-bean**: Improve how we record file size metric to avoid a race in our file lagging alert
* **Promtail/journal** [1072](https://github.com/grafana/loki/pull/1072) **rfratto**: build: enable journal in promtail linux release build

## Docs

* **Docs** [1176](https://github.com/grafana/loki/pull/1176) **rfratto**: docs: add example and documentation about using JMESPath literals
* **Docs** [1139](https://github.com/grafana/loki/pull/1139) **joe-elliott**: Moved client docs and add serilog example
* **Docs** [1132](https://github.com/grafana/loki/pull/1132) **kailwallin**: FixedTypo.Update README.md
* **Docs** [1130](https://github.com/grafana/loki/pull/1130) **pracucci**: docs: fix Promtail / Loki capitalization
* **Docs** [1129](https://github.com/grafana/loki/pull/1129) **pracucci**: docs: clarified the relation between retention period and table period
* **Docs** [1124](https://github.com/grafana/loki/pull/1124) **geowa4**: Client recommendations documentation tweaks
* **Docs** [1106](https://github.com/grafana/loki/pull/1106) **cyriltovena**: Add fluent-bit missing link in the main documentation page.
* **Docs** [1099](https://github.com/grafana/loki/pull/1099) **pracucci**: docs: improve table manager documentation
* **Docs** [1094](https://github.com/grafana/loki/pull/1094) **rfratto**: docs: update stages README with the docker and cri stages
* **Docs** [1091](https://github.com/grafana/loki/pull/1091) **daixiang0**: docs(stage): add docker and cri
* **Docs** [1077](https://github.com/grafana/loki/pull/1077) **daixiang0**: doc(fluent-bit): add missing namespace
* **Docs** [1073](https://github.com/grafana/loki/pull/1073) **flouthoc**: Re Fix Docs: PR https://github.com/grafana/loki/pull/1053 got erased due to force push.
* **Docs** [1069](https://github.com/grafana/loki/pull/1069) **daixiang0**: doc: unify GOPATH
* **Docs** [1068](https://github.com/grafana/loki/pull/1068) **daixiang0**: doc: skip jb init when using Tanka
* **Docs** [1067](https://github.com/grafana/loki/pull/1067) **rfratto**: Fix broken links to docs in README.md
* **Docs** [1064](https://github.com/grafana/loki/pull/1064) **jonaskello**: Fix spelling of HTTP header
* **Docs** [1063](https://github.com/grafana/loki/pull/1063) **rfratto**: docs: fix deprecated warning in api.md
* **Docs** [1060](https://github.com/grafana/loki/pull/1060) **rfratto**: Add Drone CI badge to README.md
* **Docs** [1053](https://github.com/grafana/loki/pull/1053) **flouthoc**: Fix Docs: Change Imagepull policy to IfNotpresent / Add loki-canary b…
* **Docs** [1048](https://github.com/grafana/loki/pull/1048) **wassan128**: Loki: Fix README link
* **Docs** [1042](https://github.com/grafana/loki/pull/1042) **daixiang0**: doc(ksonnet): include ksonnet-lib
* **Docs** [1039](https://github.com/grafana/loki/pull/1039) **sh0rez**: doc(production): replace ksonnet with Tanka
* **Docs** [1036](https://github.com/grafana/loki/pull/1036) **sh0rez**: feat: -version flag
* **Docs** [1025](https://github.com/grafana/loki/pull/1025) **oddlittlebird**: Update CONTRIBUTING.md
* **Docs** [1024](https://github.com/grafana/loki/pull/1024) **oddlittlebird**: Update README.md
* **Docs** [1014](https://github.com/grafana/loki/pull/1014) **polar3130**: Fix a link to correct doc and fix a typo
* **Docs** [1006](https://github.com/grafana/loki/pull/1006) **slim-bean**: fixing lots of broken links and a few typos
* **Docs** [1005](https://github.com/grafana/loki/pull/1005) **SmilingNavern**: Fix links to correct doc
* **Docs** [1004](https://github.com/grafana/loki/pull/1004) **rfratto**: docs: fix example with pulling systemd logs
* **Docs** [1003](https://github.com/grafana/loki/pull/1003) **oddlittlebird**: Loki: Update README.md
* **Docs** [984](https://github.com/grafana/loki/pull/984) **tomgs**: Changing "Usage" link in main readme after docs change
* **Docs** [983](https://github.com/grafana/loki/pull/983) **daixiang0**: update positions.yaml location reference
* **Docs** [982](https://github.com/grafana/loki/pull/982) **rfratto**: Documentation Rewrite
* **Docs** [961](https://github.com/grafana/loki/pull/961) **worr**: doc: Add permissions that IAM roles for Loki need
* **Docs** [933](https://github.com/grafana/loki/pull/933) **pracucci**: doc: move promtail doc into dedicated subfolder
* **Docs** [924](https://github.com/grafana/loki/pull/924) **pracucci**: doc: promtail known failure modes
* **Docs** [910](https://github.com/grafana/loki/pull/910) **slim-bean**: docs(build): Update docs around releasing and fix bug in version updating script
* **Docs** [850](https://github.com/grafana/loki/pull/850) **sh0rez**: docs: general documentation rework

## Build

* **Build** [1157](https://github.com/grafana/loki/pull/1157) **daixiang0**: Update golint
* **Build** [1133](https://github.com/grafana/loki/pull/1133) **daixiang0**: bump up golangci to 1.20
* **Build** [1121](https://github.com/grafana/loki/pull/1121) **pracucci**: Publish loki-canary binaries on release
* **Build** [1054](https://github.com/grafana/loki/pull/1054) **pstibrany**: Fix dep check warnings by running dep ensure
* **Build/release** [1018](https://github.com/grafana/loki/pull/1018) **slim-bean**: updating the image version for loki-canary and adding the version increment to the release_prepare script
* **Build/CI** [997](https://github.com/grafana/loki/pull/997) **slim-bean**: full circle
* **Build/CI** [996](https://github.com/grafana/loki/pull/996) **rfratto**: ci/drone: fix deploy command by escaping double quotes in JSON body
* **Build/CI** [995](https://github.com/grafana/loki/pull/995) **slim-bean**: use the loki-build-image for calling circle
* **Build/CI** [994](https://github.com/grafana/loki/pull/994) **slim-bean**: Also need bash for the deploy step from drone
* **Build/CI** [993](https://github.com/grafana/loki/pull/993) **slim-bean**: Add make to the alpine image used for calling the circle deploy task from drone.
* **Build/CI** [992](https://github.com/grafana/loki/pull/992) **sh0rez**: chore(packaging): fix GOPATH being overwritten
* **Build/CI** [991](https://github.com/grafana/loki/pull/991) **sh0rez**: chore(packaging): deploy from drone
* **Build/CI** [990](https://github.com/grafana/loki/pull/990) **sh0rez**: chore(ci/cd): breaking the circle
* **Build** [989](https://github.com/grafana/loki/pull/989) **sh0rez**: chore(packaging): simplify tagging
* **Build** [981](https://github.com/grafana/loki/pull/981) **sh0rez**: chore(packaging): loki windows/amd64
* **Build** [958](https://github.com/grafana/loki/pull/958) **daixiang0**: sync release pkgs name with release note
* **Build/CI** [914](https://github.com/grafana/loki/pull/914) **rfratto**: ci: update apt-get before installing deps for rootless step
* **Build** [911](https://github.com/grafana/loki/pull/911) **daixiang0**: optimize image tag script

## Deployment

* **Ksonnet** [1023](https://github.com/grafana/loki/pull/1023) **slim-bean**: make promtail daemonset name configurable
* **Ksonnet** [1021](https://github.com/grafana/loki/pull/1021) **rfratto**: ksonnet: update memcached and memcached-exporter images
* **Ksonnet** [1020](https://github.com/grafana/loki/pull/1020) **rfratto**: ksonnet: use consistent hashing in memcached client configs
* **Ksonnet** [1017](https://github.com/grafana/loki/pull/1017) **slim-bean**: make promtail configmap name configurable
* **Ksonnet** [946](https://github.com/grafana/loki/pull/946) **rfratto**: ksonnet: remove prefix from kvstore.consul settings in loki config
* **Ksonnet** [926](https://github.com/grafana/loki/pull/926) **slim-bean**: feat(promtail): Make cluster role configurable
<!-- -->
* **Helm** [1174](https://github.com/grafana/loki/pull/1174) **rally25rs**: loki-stack: Add release name to prometheus service name.
* **Helm** [1152](https://github.com/grafana/loki/pull/1152) **nicr9**: docs(helm): fix broken link to grafana datasource
* **Helm** [1134](https://github.com/grafana/loki/pull/1134) **minhdanh**: Helm chart: Allow additional scrape_configs to be added
* **Helm** [1111](https://github.com/grafana/loki/pull/1111) **ekarlso**: helm: Add support for passing arbitrary secrets
* **Helm** [1110](https://github.com/grafana/loki/pull/1110) **marcosnils**: Bump grafana image in loki helm chart
* **Helm** [1104](https://github.com/grafana/loki/pull/1104) **marcosnils**: <Examples>: Deploy prometheus from helm chart
* **Helm** [1058](https://github.com/grafana/loki/pull/1058) **polar3130**: Helm: Remove default value of storageClassName in loki/loki helm chart
* **Helm** [1056](https://github.com/grafana/loki/pull/1056) **polar3130**: Helm: Fix the reference error of loki/loki helm chart
* **Helm** [967](https://github.com/grafana/loki/pull/967) **makocchi-git**: helm chart: Add missing operator to promtail
* **Helm** [937](https://github.com/grafana/loki/pull/937) **minhdanh**: helm chart: Add support for additional labels and scrapeTimeout for serviceMonitors
* **Helm** [909](https://github.com/grafana/loki/pull/909) **angelbarrera92**: Feature: Add extra containers to loki helm chart
* **Helm** [855](https://github.com/grafana/loki/pull/855) **ikeeip**: set helm chart appVersion while release
* **Helm** [675](https://github.com/grafana/loki/pull/675) **cyriltovena**: Helm default ingester config

## Loki Canary

* **Loki-canary** [1137](https://github.com/grafana/loki/pull/1137) **slim-bean**: Add some additional logging to the canary on queries
* **Loki-canary** [1131](https://github.com/grafana/loki/pull/1131) **rfratto**: pkg/canary: use default HTTP client when reading from Loki

## Logcli

* **Logcli** [1168](https://github.com/grafana/loki/pull/1168) **sh0rez**: feat(cli): order flags by categories
* **Logcli** [1115](https://github.com/grafana/loki/pull/1115) **pracucci**: logcli: introduced QueryStringBuilder utility to clean up query string encoding
* **Logcli** [1103](https://github.com/grafana/loki/pull/1103) **pracucci**: logcli: added --step support to query command
* **Logcli** [987](https://github.com/grafana/loki/pull/987) **joe-elliott**: Logcli: Add Support for New Query Path

## Tooling

* **Dashboards** [1188](https://github.com/grafana/loki/pull/1188) **joe-elliott**: Adding Operational dashboards
* **Dashboards** [1143](https://github.com/grafana/loki/pull/1143) **joe-elliott**: Improved compression ratio histogram
* **Dashboards** [1126](https://github.com/grafana/loki/pull/1126) **joe-elliott**: Fix Loki Chunks Dashboard
* **Tools** [1108](https://github.com/grafana/loki/pull/1108) **joe-elliott**: Updated push path to current prod

## Plugins

* **DockerDriver** [972](https://github.com/grafana/loki/pull/972) **cyriltovena**: Add stream label to docker driver
* **DockerDriver** [971](https://github.com/grafana/loki/pull/971) **cyriltovena**: Allow to pass max-size and max-file to the docker driver
* **DockerDriver** [970](https://github.com/grafana/loki/pull/970) **mindfl**: docker-driver compose labels support
<!-- -->
* **Fluentd** [928](https://github.com/grafana/loki/pull/928) **candlerb**: fluent-plugin-grafana-loki: Escape double-quotes in labels, and suppress labels with value nil
<!-- -->
* **Fluent Bit** [1155](https://github.com/grafana/loki/pull/1155) **cyriltovena**: rollback fluent-bit push path until we release 0.4
* **Fluent Bit** [1096](https://github.com/grafana/loki/pull/1096) **JensErat**: fluent-bit: edge case tests
* **Fluent Bit** [847](https://github.com/grafana/loki/pull/847) **cosmo0920**: fluent-bit shared object go plugin

## Misc

Loki is now using a Bot to help keep issues and PR's pruned based on age/relevancy.  Please don't hesitate to comment on an issue or PR that you think was closed by the stale-bot which you think should remain open!!

* **Github** [965](https://github.com/grafana/loki/pull/965) **rfratto**: Change label used to keep issues from being marked as stale to keepalive
* **Github** [964](https://github.com/grafana/loki/pull/964) **rfratto**: Add probot-stale configuration to close stale issues.











# 0.3.0 (2019-08-16)

### Features/Enhancements


* **Loki** [877](https://github.com/grafana/loki/pull/877) **pracucci**: loki: Improve Tailer loop
* **Loki** [870](https://github.com/grafana/loki/pull/870) **sandlis**: bigtable-backup: update docker image for bigtable-backup tool
* **Loki** [862](https://github.com/grafana/loki/pull/862) **sandlis**: live-tailing: preload all the historic entries before query context is cancelled
* **Loki** [858](https://github.com/grafana/loki/pull/858) **pracucci**: loki: removed unused TestGZIPCompression
* **Loki** [854](https://github.com/grafana/loki/pull/854) **adityacs**: Readiness probe for querier
* **Loki** [851](https://github.com/grafana/loki/pull/851) **cyriltovena**: Add readiness probe to distributor deployment.
* **Loki** [894](https://github.com/grafana/loki/pull/894) **rfratto**: ksonnet: update ingester config to transfer chunks on rollout
<!-- -->
* **Build** [901](https://github.com/grafana/loki/pull/901) **sh0rez**: chore(packaging): set tag length to 7
* **Build** [900](https://github.com/grafana/loki/pull/900) **sh0rez**: chore(ci/cd): fix grafanasaur credentials and CircleCI image build
* **Build** [891](https://github.com/grafana/loki/pull/891) **sh0rez**: chore(ci/cd): build containers using drone.io
* **Build** [888](https://github.com/grafana/loki/pull/888) **rfratto**: Makefile: disable building promtail with systemd support on non-amd64 platforms
* **Build** [887](https://github.com/grafana/loki/pull/887) **slim-bean**: chore(packaging): Dockerfile make avoid containers
* **Build** [886](https://github.com/grafana/loki/pull/886) **sh0rez**: chore(packaging): wrong executable format
* **Build** [855](https://github.com/grafana/loki/pull/855) **ikeeip**: set helm chart appVersion while release
<!-- -->
* **Promtail** [856](https://github.com/grafana/loki/pull/856) **martinbaillie**: promtail: Add ServiceMonitor and headless Service
* **Promtail** [809](https://github.com/grafana/loki/pull/809) **rfratto**: Makefile: build promtail with CGO_ENABLED if GOHOSTOS=GOOS=linux
* **Promtail** [730](https://github.com/grafana/loki/pull/730) **rfratto**: promtail: Add systemd journal support

> 809, 730 NOTE: Systemd journal support is currently limited to amd64 images, arm support should come in the future when the transition to building the arm image and binaries is done natively via an arm container
<!-- -->
* **Docs** [896](https://github.com/grafana/loki/pull/896) **dalance**: docs: fix link format
* **Docs** [876](https://github.com/grafana/loki/pull/876) **BouchaaraAdil**: update Docs: update Retention section on Operations doc file
* **Docs** [864](https://github.com/grafana/loki/pull/864) **temal-**: docs: Replace old values in operations.md
* **Docs** [853](https://github.com/grafana/loki/pull/853) **cyriltovena**: Add governance documentation
<!-- -->
* **Deployment** [874](https://github.com/grafana/loki/pull/874) **slim-bean**: make our ksonnet a little more modular by parameterizing the chunk and index stores
* **Deployment** [857](https://github.com/grafana/loki/pull/857) **slim-bean**: Reorder relabeling rules to prevent pod label from overwriting config define labels

> 857 POSSIBLY BREAKING: If you relied on a custom pod label to overwrite one of the labels configured by the other sections of the scrape config: `job`, `namespace`, `instance`, `container_name` and/or `__path__`, this will no longer happen, the custom pod labels are now loaded first and will be overwritten by any of these listed labels.


### Fixes

* **Loki** [897](https://github.com/grafana/loki/pull/897) **pracucci**: Fix panic in tailer when an ingester is removed from the ring while tailing
* **Loki** [880](https://github.com/grafana/loki/pull/880) **cyriltovena**: fix a bug where nil line buffer would be put back
* **Loki** [859](https://github.com/grafana/loki/pull/859) **pracucci**: loki: Fixed out of order entries allowed in a chunk on edge case
<!-- -->
* **Promtail** [893](https://github.com/grafana/loki/pull/893) **rfratto**: pkg/promtail/positions: remove executable bit from positions file
<!-- -->
* **Deployment** [867](https://github.com/grafana/loki/pull/867) **slim-bean**: Update read dashboard to include only query and label query routes
* **Deployment** [865](https://github.com/grafana/loki/pull/865) **sandlis**: fix broken jsonnet for querier
<!-- -->
* **Canary** [889](https://github.com/grafana/loki/pull/889) **slim-bean**: fix(canary): Fix Flaky Tests
<!-- -->
* **Pipeline** [869](https://github.com/grafana/loki/pull/869) **jojohappy**: Pipeline: Fixed labels process test with same objects
<!-- -->
* **Logcli** [863](https://github.com/grafana/loki/pull/863) **adityacs**: Fix Nolabels parse metrics


# 0.2.0 (2019-08-02)

There were over 100 PR's merged since 0.1.0 was released, here's a highlight:

### Features / Enhancements

* **Loki**:  [521](https://github.com/grafana/loki/pull/521) Query label values and names are now fetched from the store.
* **Loki**:  [541](https://github.com/grafana/loki/pull/541) Improvements in live tailing of logs.
* **Loki**: [713](https://github.com/grafana/loki/pull/713) Storage memory improvement.
* **Loki**: [764](https://github.com/grafana/loki/pull/764) Tailing can fetch previous logs for context.
* **Loki**: [782](https://github.com/grafana/loki/pull/782) Performance improvement: Query storage by iterating through chunks in batches.
* **Loki**: [788](https://github.com/grafana/loki/pull/788) Querier timeouts.
* **Loki**: [794](https://github.com/grafana/loki/pull/794) Support ingester chunk transfer on shutdown.
* **Loki**: [729](https://github.com/grafana/loki/pull/729) Bigtable backup tool support.
<!-- -->
* **Pipeline**: [738](https://github.com/grafana/loki/pull/738) Added a template stage for manipulating label values.
* **Pipeline**: [732](https://github.com/grafana/loki/pull/732) Support for Unix timestamps.
* **Pipeline**: [760](https://github.com/grafana/loki/pull/760) Support timestamps without year.
<!-- -->
* **Helm**:  [641](https://github.com/grafana/loki/pull/641) Helm integration testing.
* **Helm**: [824](https://github.com/grafana/loki/pull/824) Add service monitor.
* **Helm**: [830](https://github.com/grafana/loki/pull/830) Customize namespace.
<!-- -->
* **Docker-Plugin**: [663](https://github.com/grafana/loki/pull/663) Created a Docker logging driver plugin.
<!-- -->
* **Fluent-Plugin**: [669](https://github.com/grafana/loki/pull/669) Ability to specify keys to remove.
* **Fluent-Plugin**: [709](https://github.com/grafana/loki/pull/709) Multi-worker support.
* **Fluent-Plugin**: [792](https://github.com/grafana/loki/pull/792) Add prometheus for metrics and update gems.
<!-- -->
* **Build**: [668](https://github.com/grafana/loki/pull/668),[762](https://github.com/grafana/loki/pull/762) Build multiple architecture containers.
<!-- -->
* **Loki-Canary**: [772](https://github.com/grafana/loki/pull/772) Moved into Loki project.

### Bugfixes

There were many fixes, here are a few of the most important:

* **Promtail**: [650](https://github.com/grafana/loki/pull/650) Build on windows.
* **Fluent-Plugin**: [667](https://github.com/grafana/loki/pull/667) Rename fluent plugin.
* **Docker-Plugin**: [813](https://github.com/grafana/loki/pull/813) Fix panic for newer docker version (18.09.7+).


# 0.1.0 (2019-06-03)

First (beta) Release!
