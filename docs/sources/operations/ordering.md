---
title: Out of Order Writes
weight: 60
---
# Enabling Out of Order writes.

Enabling out of order writes involves two configuration options in Loki:

- `ingester.unordered-writes-enabled` must be enabled
- `ingester.max-chunk-age` (default 1h) is used to parameterize _how far back out of order data is accepted_.

When unordered writes are enabled, Loki will accept older data for each stream as far back as `most_recent_line - (max_chunk_age/2)`. For instance, if `ingester.max-chunk-age=2h` and the stream `{foo="bar"}` has one entry at `8:00`, Loki will accept data for that stream as far back in time as `7:00`. If another log line is wrritten at `10:00`, this boundary changes to `9:00`. Anything farther back than this range will still return an _out of order_ error.
