---
headless: true
description: |
  A headless page sets the render and list build options to never, creating a bundle of page resources.
  These page resources are accessible to the `docs/shared` shortcode.
---

[//]: # 'This file documents query error messages and troubleshooting'
[//]: #
[//]: # 'This shared file is included in these locations:'
[//]: # '/loki/docs/loki/latest/query/troubleshoot-query.md'
[//]: # '/loki/docs/loki/latest/operations/troubleshooting/troubleshoot-query.md'
[//]: #
[//]: # 'If you make changes to this file, verify that the meaning and content are not changed in any place where the file is included.'
[//]: # 'Any links should be fully qualified and not relative: /docs/grafana/ instead of ../grafana/.'

This guide helps you troubleshoot errors that occur when querying logs from Loki. When Loki rejects or fails query requests, it's typically due to query syntax errors, exceeding limits, timeout issues, or storage access problems.

Before you begin, ensure you have the following:

- Access to Grafana Loki logs and metrics
- Understanding of [LogQL query language](https://grafana.com/docs/loki/<LOKI_VERSION>/query/) basics
- Permissions to configure limits and settings if needed

## Monitoring query errors

Query errors can be observed using these Prometheus metrics:

- `loki_request_duration_seconds` - Query latency by route and status code
- `loki_logql_querystats_bytes_processed_per_seconds` - Bytes processed during queries
- `loki_frontend_query_range_duration_seconds_bucket` - Frontend query latency

You can set up alerts on 4xx and 5xx status codes to detect query problems early. This can be helpful when tuning limits configurations.

## LogQL parse errors

Parse errors occur when the LogQL query syntax is invalid. Loki returns HTTP status code `400 Bad Request` for all parse errors.

### Error: Failed to parse the log query

**Error message:**

`failed to parse the log query`

Or with position details:

`parse error at line <line>, col <col>: <message>`

**Cause:**

The LogQL query contains syntax errors. This could be due to:

- Missing or mismatched brackets, quotes, or braces
- Invalid characters or operators
- Incorrect function syntax
- Invalid duration format

**Common examples:**

| Invalid Query | Error | Fix |
|--------------|-------|-----|
| `{app="foo"` | Missing closing brace | `{app="foo"}` |
| `{app="foo"} \|= test` | Unquoted filter string | `{app="foo"} \|= "test"` |
| `rate({app="foo"}[5minutes])` | Invalid duration unit | `rate({app="foo"}[5m])` |
| `{app="foo"} \| json` | Missing pipe symbol before parser | `{app="foo"} \| json` |

**Resolution:**

Start with a simple stream selector: `{job="app"}`, then add filters and operations incrementally to identify syntax issues.

* **Check bracket matching** - Ensure all `{`, `}`, `(`, `)`, `[`, `]` are properly closed.
* **Verify string quoting** - All label values and filter strings must be quoted.
* **Use valid duration units** - Use `ns`, `us`, `ms`, `s`, `m`, `h`, `d`, `w`, `y` , for example, `5m` not `5minutes`.
* **Review operator syntax** - Ensure label matchers use proper operators (`=`, `!=`, `=~`, `!~`). Check the [LogQL documentation](https://grafana.com/docs/loki/<LOKI_VERSION>/query/) for correct operator usage.
* **Use Grafana Assistant** - If you are a Cloud Logs user, you can use Grafana Assistant to write or revise your query using natural language, for example, “What errors occurred for application foo in the last hour?”

**Properties:**

- Enforced by: Query Frontend/Querier
- Retryable: No (query must be fixed)
- HTTP status: 400 Bad Request
- Configurable per tenant: No

### Error: At least one equality matcher required

**Error message:**

`parse error : queries require at least one regexp or equality matcher that does not have an empty-compatible value. For instance, app=~".*" does not meet this requirement, but app=~".+" will`

**Cause:**

The query uses only negative matchers (`!=`, `!~`) or matchers that match empty strings (`=~".*"`), which would select all streams. This is prevented to protect against accidentally querying the entire database.

**Invalid examples:**

```logql
{foo!="bar"}
{app=~".*"}
{foo!~"bar|baz"}
```

**Valid examples:**

```logql
{foo="bar"}
{app=~".+"}
{app="baz", foo!="bar"}
```

**Resolution:**

* **Add at least one positive matcher** that selects specific streams.
* **Use `.+` instead of `.*`** in regex matchers to require at least one character.
* **Add additional label selectors** to narrow down the query scope.
* **Use Grafana Assistant** - If you are a Cloud Logs user, you can use Grafana Assistant to write or revise your query using natural language, for example, “Find logs containing ’foo' but not 'bar' or 'baz'.”

**Properties:**

- Enforced by: Query Frontend
- Retryable: No (query must be fixed)
- HTTP status: 400 Bad Request
- Configurable per tenant: No

### Error: Only label matchers are supported

**Error message:**

`only label matchers are supported`

**Cause:**

The query was passed to an API that only accepts label matchers (like the series API), but included additional expressions like line filters or parsers.

**Resolution:**

* **Use only stream selectors** for APIs that don't support full LogQL:

   ```logql
   # Valid for series API
   {app="foo", env="prod"}
   
   # Invalid for series API
   {app="foo"} |= "error"
   ```

**Properties:**

- Enforced by: API handler
- Retryable: No (query must be fixed)
- HTTP status: 400 Bad Request
- Configurable per tenant: No

### Error: Log queries not supported as instant query type

**Error message:**

`log queries are not supported as an instant query type, please change your query to a range query type`

**Cause:**

A [log query](https://grafana.com/docs/loki/<LOKI_VERSION>/query/log_queries/) (one that returns log lines rather than metrics) was submitted to the instant query endpoint (`/loki/api/v1/query`). Log queries must use the range query endpoint.

**Resolution:**

* **Convert to a range query** Convert log queries to range queries with a time range. Range queries are the default in Grafana Explore.
* **Use the range query endpoint** `/loki/api/v1/query_range` for log queries.
* **Convert to a metric query** if you need to use instant queries:

   ```logql
   # This is a log query (returns logs)
   {app="foo"} |= "error"
   
   # This is a metric query (can be instant)
   count_over_time({app="foo"} |= "error"[5m])
   ```

* **Use Grafana Assistant** - If you are a Cloud Logs user, you can use Grafana Assistant to write or revise your query.

**Properties:**

- Enforced by: Query API
- Retryable: No (use correct endpoint or query type)
- HTTP status: 400 Bad Request
- Configurable per tenant: No

### Error: Invalid aggregation without unwrap

**Error message:**

`parse error : invalid aggregation sum_over_time without unwrap`

**Cause:**

Aggregation functions like `sum_over_time`, `avg_over_time`, `min_over_time`, `max_over_time` require an `unwrap` expression to extract a numeric value from log lines.

**Resolution:**

* **Add an unwrap expression** to extract the numeric label:

   ```logql
   # Invalid
   sum_over_time({app="foo"} | json [5m])
   
   # Valid - unwrap a numeric label
   sum_over_time({app="foo"} | json | unwrap duration [5m])
   ```

**Properties:**

- Enforced by: Query Parser
- Retryable: No (query must be fixed)
- HTTP status: 400 Bad Request
- Configurable per tenant: No

### Error: Invalid aggregation with unwrap

**Error message:**

`parse error : invalid aggregation count_over_time with unwrap`

**Cause:**

The `count_over_time` function doesn't use unwrapped values - it just counts log lines. Using it with `unwrap` is invalid.

**Resolution:**

* **Remove the unwrap expression** for count_over_time:

   ```logql
   # Invalid
   count_over_time({app="foo"} | json | unwrap duration [5m])
   
   # Valid
   count_over_time({app="foo"} | json [5m])
   ```

* **Use sum_over_time** if you want to sum unwrapped values.

**Properties:**

- Enforced by: Query Parser
- Retryable: No (query must be fixed)
- HTTP status: 400 Bad Request
- Configurable per tenant: No
