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

## Query limit errors

These errors occur when queries exceed configured resource limits. They return HTTP status code `400 Bad Request`.

### Error: Maximum series reached

**Error message:**

`maximum number of series (<limit>) reached for a single query; consider reducing query cardinality by adding more specific stream selectors, reducing the time range, or aggregating results with functions like sum(), count() or topk()`

**Cause:**

The query matches more unique label combinations (series) than the configured limit allows. This protects against queries that would consume excessive memory.

**Default configuration:**

- `max_query_series`: 500 (default)

**Resolution:**

* **Add more specific stream selectors** to reduce cardinality:

   ```logql
   # Too broad
   {job="ingress-nginx"}
   
   # More specific
   {job="ingress-nginx", namespace="production", pod=~"ingress-nginx-.*"}
   ```

* **Reduce the time range** of the query.

* **Use label filters** to narrow down results: `{job="app"} |= "error"`

* **Use aggregation functions** to reduce cardinality:

   ```logql
   sum by (status) (rate({job="nginx"} | json [5m]))
   ```

* **Increase the limit** if resources allow:

   ```yaml
   limits_config:
     max_query_series: 1000  #default is 500
   ```

**Properties:**

- Enforced by: Query Frontend
- Retryable: No (query must be modified)
- HTTP status: 400 Bad Request
- Configurable per tenant: Yes

### Error: Cardinality issues

**Error message:**

`cardinality limit exceeded for {}; 100001 entries, more than limit of 100000`

**Cause:**

The query produces results with too many unique label combinations. This protects against queries that would generate excessive memory usage and slow performance.

**Default configuration:**

- `cardinality_limit`: 100000

**Resolution:**

* **Use more specific label selectors** to reduce the number of unique streams.
* **Apply aggregation functions** to reduce cardinality:

   ```logql
   sum by (status) (rate({job="nginx"}[5m]))
   ```

* **Use `by()` or `without()` clauses** to group results and reduce dimensions:

   ```logql
   sum by (status, method) (rate({job="nginx"} | json [5m]))
   ```

* **Increase the limit** if needed:

   ```yaml
   limits_config:
     cardinality_limit: 200000  #default is 100000
   ```

**Properties:**

- Enforced by: Query Engine
- Retryable: No (query must be modified)
- HTTP status: 400 Bad Request
- Configurable per tenant: Yes

### Error: Max entries limit per query exceeded

**Error message:**

`max entries limit per query exceeded, limit > max_entries_limit_per_query (<requested> > <limit>)`

**Cause:**

The query requests more log entries than the configured maximum. This applies to log queries (not metric queries).

**Default configuration:**

- `max_entries_limit_per_query`: 5000

**Resolution:**

* **Reduce the limit parameter** in your query request.

* **Use pagination** with the `limit` parameter: `{job="app"} | limit 1000`

* **Add more specific filters** to return fewer results:

   ```logql
   {app="foo"} |= "error" | json | level="error"
   ```

* **Reduce the time range** of the query.

* **Increase the limit** if needed:

   ```yaml
   limits_config:
     max_entries_limit_per_query: 10000  #default is 5000
   ```

**Properties:**

- Enforced by: Querier/Query Frontend
- Retryable: No (query must be modified)
- HTTP status: 400 Bad Request
- Configurable per tenant: Yes

### Error: Query would read too many bytes

**Error message:**

`the query would read too many bytes (query: <size>, limit: <limit>); consider adding more specific stream selectors or reduce the time range of the query`

**Cause:**

The estimated data volume for the query exceeds the configured limit. This is determined before query execution using index statistics.

**Default configuration:**

- `max_query_bytes_read`: 0B (disabled by default)

**Resolution:**

* **Add more specific stream selectors** to reduce data volume.
* **Reduce the time range** of the query.
* **Use line filters** to reduce processing:

   ```logql
   {app="foo"} |= "error"
   ```

* **Use sampling**

  ```logql
   {job="app"} | line_format "{{__timestamp__}} {{.msg}}" | sample 0.1
   ```

* **Increase the limit** if resources allow:

   ```yaml
   limits_config:
     max_query_bytes_read: 10GB
   ```

**Properties:**

- Enforced by: Query Frontend
- Retryable: No (query must be modified)
- HTTP status: 400 Bad Request
- Configurable per tenant: Yes

### Error: Too many chunks (count)

**Error message:**

`the query hit the max number of chunks limit (limit: 2000000 chunks)`

**Cause:**

The number of chunks that the query would read exceeds the configured limit. This protects against queries that would scan excessive amounts of data and consume too much memory.

**Default configuration:**

- `max_chunks_per_query`: 2000000

**Resolution:**

* **Narrow stream selectors** to reduce the number of matching chunks:

   ```logql
   # Too broad
   {job="app"}
   
   # More specific
   {job="app", environment="production", namespace="api"}
   ```

* **Reduce the query time range** to scan fewer chunks.
* **Add line filters** to reduce processing:

   ```logql
   {app="foo"} |= "error"
   ```

* **Increase the limit** if resources allow:

   ```yaml
   limits_config:
     max_chunks_per_query: 5000000  #default is 2000000
   ```

**Properties:**

- Enforced by: Query Frontend
- Retryable: No (query must be modified)
- HTTP status: 400 Bad Request
- Configurable per tenant: Yes

### Error: Stream matcher limits

**Error message:**

`max streams matchers per query exceeded, matchers-count > limit (1000 > 500)`

**Cause:**

The query contains too many stream matchers. This limit prevents queries with excessive complexity that could impact query performance.

**Default configuration:**

- `max_streams_matchers_per_query`: 1000

**Resolution:**

* **Simplify your query** by using fewer label matchers.
* **Combine multiple queries** instead of using many OR conditions.
* **Use regex matchers** to consolidate multiple values:

   ```logql
   # Instead of many matchers
   {job="app1"} or {job="app2"} or {job="app3"}
   
   # Use regex
   {job=~"app1|app2|app3"}
   ```

* **Increase the limit** if needed:

   ```yaml
   limits_config:
     max_streams_matchers_per_query: 2000  #default is 1000
   ```

**Properties:**

- Enforced by: Query Frontend
- Retryable: No (query must be modified)
- HTTP status: 400 Bad Request
- Configurable per tenant: Yes

### Error: Query too large for single querier

**Error message:**

`query too large to execute on a single querier: (query: <size>, limit: <limit>); consider adding more specific stream selectors, reduce the time range of the query, or adjust parallelization settings`

Or for un-shardable queries:

`un-shardable query too large to execute on a single querier: (query: <size>, limit: <limit>); consider adding more specific stream selectors or reduce the time range of the query`

**Cause:**

Even after query splitting and sharding, individual query shards exceed the per-querier byte limit.

**Default configuration:**

- `max_querier_bytes_read`: 150GB (per querier)

**Resolution:**

* **Add more specific stream selectors**.
* **Reduce the time range** or Break large queries into smaller time ranges.
* **Simplify the query** if possible - some queries cannot be sharded.
* **Increase the limit** (requires more querier resources):

   ```yaml
   limits_config:
     max_querier_bytes_read: 200GB  # default is 150GB
   ```

* **Scale querier resources**

**Properties:**

- Enforced by: Query Frontend
- Retryable: No (query must be modified)
- HTTP status: 400 Bad Request
- Configurable per tenant: Yes

### Error: Limit reached while evaluating query

**Error message:**

`limit reached while evaluating the query`

**Cause:**

An internal limit was reached during query evaluation. This is a catch-all for various internal limits.

**Resolution:**

* **Simplify the query** - reduce complexity.
* **Reduce the time range**.
* **Add more specific stream selectors**.

**Properties:**

- Enforced by: Query Engine
- Retryable: No
- HTTP status: 400 Bad Request
- Configurable per tenant: Varies

### Error: Interval value exceeds limit

**Error message:**

`[interval] value exceeds limit`

**Cause:**

The range vector interval (in brackets like `[5m]`) exceeds configured limits.

**Resolution:**

* **Reduce the range interval** in your query:

   ```logql
   # If [1d] is too large, try smaller intervals
   rate({app="foo"}[1h])
   ```

* **Check your configuration** for `max_query_length` limits. The default is `30d1h`.

**Properties:**

- Enforced by: Query Engine
- Retryable: No (query must be modified)
- HTTP status: 400 Bad Request
- Configurable per tenant: Yes
