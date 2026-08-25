---
title: Block unwanted queries
description: Describes how to configure Grafana Loki to block unwanted or expensive queries using per-tenant overrides.
weight: 
---
# Block unwanted queries

In certain situations, you may not be able to control the queries being sent to your Loki installation. These queries
may be intentionally or unintentionally expensive to run, and they may affect the overall stability or cost of running
your service.

You can block queries using [per-tenant overrides](https://grafana.com/docs/loki/<LOKI_VERSION>/configure/#runtime-configuration-file), like so:

```yaml
overrides:
  "tenant-id":
    blocked_queries:
      # block this query exactly
      - pattern: 'sum(rate({env="prod"}[1m]))'

      # block any query matching this regex pattern 
      - pattern: '.*prod.*'
        regex: true

      # block all metric queries
      - types: metric

      # block any filter or limited queries matching this regex pattern 
      - pattern: '.*prod.*'
        regex: true
        types: filter,limited

      # block any query that matches this query hash
      - hash: 2943214005          # hash of {stream="stdout",pod="loki-canary-9w49x"}
        types: filter,limited

      # block queries originating from specific sources via X-Query-Tags
      # Keys and values are matched case-insensitively.
      - pattern: '.*'             # optional; if pattern and regex are omitted they will default to '.*' and true
        regex: true
        query_tags:
          source: grafana
          feature: beta
```

{{< admonition type="note" >}}
Changes to these configurations **do not require a restart**; they are defined in the [runtime configuration file](https://grafana.com/docs/loki/<LOKI_VERSION>/configure/#runtime-configuration-file).
{{< /admonition >}}

The available query types are:

- `metric`: a query with an aggregation, e.g. `sum(rate({env="prod"}[1m]))`
- `filter`: a query with a log filter, e.g. `{env="prod"} |= "error"`
- `limited`: a query without a filter or a metric aggregation

The `hash` option uses a [32-bit FNV-1](https://en.wikipedia.org/wiki/Fowler%E2%80%93Noll%E2%80%93Vo_hash_function) hash of the query string, represented as a 32-bit unsigned integer.
This can often be easier to use than query strings that are long or require lots of string escaping. A `query_hash` field
is logged with every query request in the `query-frontend` and `querier` logs, for easy reference. Here's an example log line:

```logfmt
level=info ts=2023-03-30T09:08:15.2614555Z caller=metrics.go:152 component=frontend org_id=29 latency=fast 
query="{stream=\"stdout\",pod=\"loki-canary-9w49x\"}" query_hash=2943214005 query_type=limited range_type=range ...
```

{{< admonition type="note" >}}
The order of patterns is preserved, and Loki stops at the first rule whose `pattern` or `hash` matches the query text.
If that rule's `types` or `query_tags` constraints then don't match, the query is **not** blocked, and Loki does not continue checking any later rules, even if a later rule would have matched and blocked the query.

For example, with this configuration:

```yaml
overrides:
  "tenant-id":
    blocked_queries:
      - pattern: '{env="prod"}'
        types: metric
      - pattern: '{env="prod"}'
```

A query for `{env="prod"}` (a `limited` query) matches the first rule's pattern, but not its `types: metric` constraint, so it is not blocked. Loki does not fall through to the second rule, which has no `types` restriction and would otherwise have blocked it. To block a query with more than one rule that share a pattern, combine the constraints into a single rule instead of relying on a later rule as a fallback.
{{< /admonition >}}

## Observing blocked queries

Blocked queries are logged, as well as counted in the `loki_blocked_queries` metric on a per-tenant basis.

When a policy matches by pattern/hash/regex, Loki logs whether the query type and request tags matched that policy:

```logfmt
level=warn msg="query blocker matched with regex policy" user=29 type=metric pattern=".*rate\\(.*\\).*" query="sum(rate({app=\"foo\"}[5m]))" typesMatched=true tagsMatched=false blocked=false
```

If tag constraints fail to match, Loki emits a debug log showing the missing key and the raw header value that was received:

```logfmt
level=debug msg="query blocker tags mismatch: missing or mismatched key" key=feature tagsRaw="Source=grafana,Feature=alpha"
```

## Scope

Query blocking is enforced by the LogQL query engine when it evaluates a query. This covers:

- Range and instant queries sent to the `/loki/api/v1/query` and `/loki/api/v1/query_range` API endpoints.
- [Alerting and recording rules](https://grafana.com/docs/loki/<LOKI_VERSION>/alert/), in both local and remote rule evaluation modes. Remote evaluation sends the rule's query back through the query frontend and querier, so it is blocked the same way as an API query.

Query blocking does **not** apply to:

- Log tailing (the `/loki/api/v1/tail` endpoint). Tailing reads from the ingesters and the store without using the query engine, so no block policy is applied.
- Metadata endpoints, such as `labels`, `series`, `index/stats`, `index/volume`, and `detected_fields`. These endpoints don't run a LogQL expression through the query engine, so there is nothing for the query blocker to match against.

{{< admonition type="warning" >}}
Loki has an experimental next-generation query engine for querying data objects (dataobj/columnar storage), enabled with `query_engine.enable: true`. Queries that are routed to that engine are **not** checked against `blocked_queries` policies at all. This is a known limitation of the experimental engine, not a deferred check; block policies are silently ignored for any query it serves.
{{< /admonition >}}

## Tag-based blocking

You can scope a blocked query rule to requests that include specific key=value pairs in the `X-Query-Tags` header.

- Header format: `key=value` pairs separated by commas, for example: `Source=grafana,Feature=beta`.
- Allowed characters are alphanumeric plus space, comma, equals, '@', '.', and '-'. Any other characters are replaced with `_`.
- Parsing keeps only canonical `key=value` tokens; malformed tokens are ignored.
- Matching rules:
  - Keys are matched case-insensitively (the server lowercases keys).
  - Values are matched case-insensitively.
  - All specified `tags:` pairs in the rule must be present in the request to apply the block.

Examples:

```yaml
overrides:
  tenant-a:
    blocked_queries:
      # Block only metric queries from a beta feature flag
      - types: metric
        query_tags:
          feature: beta

      # Combine with regex to narrow scope further
      - pattern: '.*rate\\(.*\\).*'
        regex: true
        query_tags:
          source: grafana
```

### Ruler query tags

When the ruler evaluates alerting and recording rules in remote evaluation mode (`-ruler.evaluation.mode=remote`), it automatically sets `X-Query-Tags` on the query it sends to `source=ruler,rule_name=<rule name>,rule_type=<rule type>`.
You can match on these tags to block or scope a policy to rule evaluations specifically, for example:

```yaml
overrides:
  "tenant-id":
    blocked_queries:
      # block all queries that come from rule evaluation
      - query_tags:
          source: ruler
```

{{< admonition type="note" >}}
The ruler doesn't set `X-Query-Tags` in local evaluation mode, so you can't match on these tags there.

Be careful when you match on `rule_name`, because a rule name that contains a comma or an equals sign doesn't survive tag parsing. A comma ends the value early, so `rule_name=my,rule` is read as `rule_name=my`. An equals sign makes the pair invalid, so `rule_name=a=b` is dropped completely. Refer to [Tag-based blocking](#tag-based-blocking) for the full parsing rules.
{{< /admonition >}}
