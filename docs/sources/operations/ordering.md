---
title: Out of order writes
weight: 60
---
# Out of order writes

Enabling out of order writes involves two configuration options in Loki:

- `ingester.unordered-writes` must be enabled. This is also configurable per tenants as part of [`limits_config`](../configuration#limits_config).
- `ingester.max-chunk-age` (default 1h) is used to parameterize _how far back out of order data is accepted_.

When unordered writes are enabled, Loki will accept older data for each stream as far back as `most_recent_line - (max_chunk_age/2)`. For instance, if `ingester.max-chunk-age=2h` and the stream `{foo="bar"}` has one entry at `8:00`, Loki will accept data for that stream as far back in time as `7:00`. If another log line is written at `10:00`,  Loki will accept data for that stream as far back in time as `9:00`. Anything farther back will return an out of order error.
