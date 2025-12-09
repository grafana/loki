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
- `cortex_frontend_query_stats_latency_seconds` - Frontend query latency

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

1. **Check bracket matching** - Ensure all `{`, `}`, `(`, `)`, `[`, `]` are properly closed.
1. **Verify string quoting** - All label values and filter strings must be quoted.
1. **Use valid duration units** - Use `ns`, `us`, `ms`, `s`, `m`, `h`, `d`, `w`, `y` , for example, `5m` not `5minutes`.
1. **Review operator syntax** - Ensure label matchers use proper operators (`=`, `!=`, `=~`, `!~`). Check the [LogQL documentation](https://grafana.com/docs/loki/<LOKI_VERSION>/query/) for correct operator usage.
1. **Use Grafana Assistant** - If you are a Cloud Logs user, you can use Grafana Assistant to write or revise your query using natural language, for example, “What errors occurred for application foo in the last hour?”

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

1. **Add at least one positive matcher** that selects specific streams.
1. **Use `.+` instead of `.*`** in regex matchers to require at least one character.
1. **Add additional label selectors** to narrow down the query scope.
1. **Use Grafana Assistant** - If you are a Cloud Logs user, you can use Grafana Assistant to write or revise your query using natural language, for example, “Find logs containing ’foo' but not 'bar' or 'baz'.”

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

1. **Use only stream selectors** for APIs that don't support full LogQL:

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

1. **Convert to a range query** Convert log queries to range queries with a time range. Range queries are the default in Grafana Explore.
1. **Use the range query endpoint** `/loki/api/v1/query_range` for log queries.
1. **Convert to a metric query** if you need to use instant queries:

   ```logql
   # This is a log query (returns logs)
   {app="foo"} |= "error"
   
   # This is a metric query (can be instant)
   count_over_time({app="foo"} |= "error"[5m])
   ```

1. **Use Grafana Assistant** - If you are a Cloud Logs user, you can use Grafana Assistant to write or revise your query.

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

1. **Add an unwrap expression** to extract the numeric label:

   ```logql
   # Invalid
   sum_over_time({app="foo"} | json [5m])
   
   # Valid - unwrap a numeric label
   sum_over_time({app="foo"} | json | unwrap duration [5m])
   ```

1. **Use count_over_time** if you just want to count log lines (no unwrap needed):

   ```logql
   count_over_time({app="foo"} | json [5m])
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

1. **Remove the unwrap expression** for count_over_time:

   ```logql
   # Invalid
   count_over_time({app="foo"} | json | unwrap duration [5m])
   
   # Valid
   count_over_time({app="foo"} | json [5m])
   ```

1. **Use sum_over_time** if you want to sum unwrapped values.

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

1. **Add more specific stream selectors** to reduce cardinality:

   ```logql
   # Too broad
   {job="ingress-nginx"}
   
   # More specific
   {job="ingress-nginx", namespace="production", pod=~"ingress-nginx-.*"}
   ```

1. **Reduce the time range** of the query.

1. **Use label filters** to narrow down results: `{job="app"} |= "error"`

1. **Use aggregation functions** to reduce cardinality:

   ```logql
   sum by (status) (rate({job="nginx"} | json [5m]))
   ```

1. **Increase the limit** if resources allow:

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

1. **Use more specific label selectors** to reduce the number of unique streams.
1. **Apply aggregation functions** to reduce cardinality:

   ```logql
   sum by (status) (rate({job="nginx"}[5m]))
   ```

1. **Use `by()` or `without()` clauses** to group results and reduce dimensions:

   ```logql
   sum by (status, method) (rate({job="nginx"} | json [5m]))
   ```

1. **Increase the limit** if needed:

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

1. **Reduce the limit parameter** in your query request.

1. **Use pagination** with the `limit` parameter: `{job="app"} | limit 1000`

1. **Add more specific filters** to return fewer results:

   ```logql
   {app="foo"} |= "error" | json | level="error"
   ```

1. **Reduce the time range** of the query.

1. **Increase the limit** if needed:

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

1. **Add more specific stream selectors** to reduce data volume.
1. **Reduce the time range** of the query.
1. **Use line filters** to reduce processing:

   ```logql
   {app="foo"} |= "error"
   ```

1. **Use sampling**

  ```logql
   {job="app"} | line_format "{{__timestamp__}} {{.msg}}" | sample 0.1
   ```

1. **Increase the limit** if resources allow:

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

1. **Narrow stream selectors** to reduce the number of matching chunks:

   ```logql
   # Too broad
   {job="app"}
   
   # More specific
   {job="app", environment="production", namespace="api"}
   ```

1. **Reduce the query time range** to scan fewer chunks.
1. **Add line filters** to reduce processing:

   ```logql
   {app="foo"} |= "error"
   ```

1. **Increase the limit** if resources allow:

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

1. **Simplify your query** by using fewer label matchers.
1. **Combine multiple queries** instead of using many OR conditions.
1. **Use regex matchers** to consolidate multiple values:

   ```logql
   # Instead of many matchers
   {job="app1"} or {job="app2"} or {job="app3"}
   
   # Use regex
   {job=~"app1|app2|app3"}
   ```

1. **Increase the limit** if needed:

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

1. **Add more specific stream selectors**.
1. **Reduce the time range** or Break large queries into smaller time ranges.
1. **Simplify the query** if possible - some queries cannot be sharded.
1. **Increase the limit** (requires more querier resources):

   ```yaml
   limits_config:
     max_querier_bytes_read: 200GB  # default is 150GB
   ```

1. **Scale querier resources**

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

1. **Simplify the query** - reduce complexity.
1. **Reduce the time range**.
1. **Add more specific stream selectors**.

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

1. **Reduce the range interval** in your query:

   ```logql
   # If [1d] is too large, try smaller intervals
   rate({app="foo"}[1h])
   ```

1. **Check your configuration** for `max_query_length` limits. The default is `30d1h`.

**Properties:**

- Enforced by: Query Engine
- Retryable: No (query must be modified)
- HTTP status: 400 Bad Request
- Configurable per tenant: Yes

## Time range errors

These errors relate to the time range specified in queries.

### Error: Query time range exceeds limit

**Error message:**

`the query time range exceeds the limit (query length: <duration>, limit: <limit>)`

**Cause:**

The difference between the query's start and end time exceeds the maximum allowed query length.

**Default configuration:**

- `max_query_length`: 721h (30 days + 1 hour)

**Resolution:**

1. **Reduce the query time range**:

   ```logcli
   # Instead of querying 60 days
   logcli query '{app="foo"}' --from="60d" --to="now"
   
   # Query 30 days or less
   logcli query '{app="foo"}' --from="30d" --to="now"
   ```

1. **Increase the limit** if storage retention supports it:

   ```yaml
   limits_config:
     max_query_length: 2160h  # 90 days
   ```

**Properties:**

- Enforced by: Query Frontend/Querier
- Retryable: No (query must be modified)
- HTTP status: 400 Bad Request
- Configurable per tenant: Yes

### Error: Data is no longer available

**Error message:**

`this data is no longer available, it is past now - max_query_lookback (<duration>)`

**Cause:**

The entire query time range falls before the `max_query_lookback` limit. This happens when trying to query data older than the configured lookback period.

**Default configuration:**

- `max_query_lookback`: 0 (The default value of 0 does not set a limit.)

**Resolution:**

1. **Query more recent data** within the lookback window.
1. **Adjust the lookback limit** if the data should be queryable:

   ```yaml
   limits_config:
     max_query_lookback: 8760h  # 1 year
   ```

   {{< admonition type="caution" >}}
   The lookback limit should not exceed your retention period.
   {{< /admonition >}}

**Properties:**

- Enforced by: Query Frontend/Querier
- Retryable: No
- HTTP status: 400 Bad Request
- Configurable per tenant: Yes

### Error: Invalid query time range

**Error message:**

`invalid query, through < from (<end> < <start>)`

**Cause:**

The query end time is before the start time, which is invalid.

**Resolution:**

1. **Swap start and end times** if they were reversed.
1. **Check timestamp formats** to ensure times are correctly specified.

**Properties:**

- Enforced by: Query Frontend/Querier
- Retryable: No (query must be fixed)
- HTTP status: 400 Bad Request
- Configurable per tenant: No

## Required labels errors

These errors occur when queries don't meet configured label requirements.

### Error: Missing required matchers

**Error message:**

`stream selector is missing required matchers [<required_labels>], labels present in the query were [<present_labels>]`

**Cause:**

The tenant is configured to require certain label matchers in all queries, but the query doesn't include them.

**Default configuration:**

- `required_labels`: [] (none required by default)

**Resolution:**

1. **Check with your administrator** about which labels are required.

1. **Add the required labels** to your query:

   ```logql
   # If 'namespace' is required
   {app="foo", namespace="production"}
   ```

**Properties:**

- Enforced by: Query Frontend
- Retryable: No (query must include required labels)
- HTTP status: 400 Bad Request
- Configurable per tenant: Yes

### Error: Not enough label matchers

**Error message:**

`stream selector has less label matchers than required: (present: [<labels>], number_present: <count>, required_number_label_matchers: <required>)`

**Cause:**

The tenant is configured to require a minimum number of label matchers, but the query has fewer.

**Default configuration:**

- `minimum_labels_number`: 0 (no minimum by default)

**Resolution:**

1. **Add more label matchers** to meet the minimum requirement:

   ```logql
   # If minimum is 2, add another selector
   {app="foo", namespace="production"}
   ```

**Properties:**

- Enforced by: Query Frontend
- Retryable: No (query must meet requirements)
- HTTP status: 400 Bad Request
- Configurable per tenant: Yes

## Timeout errors

Timeout errors occur when queries take too long to execute.

### Error: Request timed out

**Error message:**

`request timed out, decrease the duration of the request or add more label matchers (prefer exact match over regex match) to reduce the amount of data processed`

Or:

`context deadline exceeded`

**Cause:**

The query exceeded the configured timeout. This can happen due to:

- Large time ranges
- High cardinality queries
- Complex query expressions
- Insufficient cluster resources
- Network issues

**Default configuration:**

- `query_timeout`: 1m
- `server.http_server_read_timeout`: 30s
- `server.http_server_write_timeout`: 30s

**Resolution:**

1. **Reduce the time range** of the query.
1. **Add more specific filters** to reduce data processing:

   ```logql
   # Less specific (slower)
   {namespace=~"prod.*"}
   
   # More specific (faster)
   {namespace="production"}
   ```

1. **Prefer exact matchers over regex** when possible.
1. **Add line filters early** in the pipeline:

   ```logql
   {app="foo"} |= "error" | json | level="error"
   ```

1. **Increase timeout limits** (if resources allow):

   ```yaml
   limits_config:
     query_timeout: 5m
   
   server:
     http_server_read_timeout: 5m
     http_server_write_timeout: 5m
   ```

1. **Use sampling** for exploratory queries

  ```logql
   {job="app"} | line_format "{{__timestamp__}} {{.msg}}" | sample 0.1
   ```

1. **Check for network issues** between components.

**Properties:**

- Enforced by: Query Frontend/Querier
- Retryable: Yes (with modifications)
- HTTP status: 504 Gateway Timeout
- Configurable per tenant: Yes (query_timeout)

### Error: Request cancelled by client

**Error message:**

`the request was cancelled by the client`

**Cause:**

The client closed the connection before receiving a response. This is typically caused by:

- Client-side timeout
- User navigating away in Grafana
- Network interruption

**Resolution:**

1. **Increase client timeout** in Grafana or LogCLI.
1. **Optimize the query** to return faster.
1. **Check network connectivity** between client and Loki.

**Properties:**

- Enforced by: Client
- Retryable: Yes
- HTTP status: 499 Client Closed Request
- Configurable per tenant: No

## Query blocked errors

These errors occur when queries are administratively blocked.

### Error: Query blocked by policy

**Error message:**

`query blocked by policy`

**Cause:**

The query matches a configured block rule. Administrators create tenant policies and rate limiting rules to block specific queries or query patterns to protect the cluster from expensive or problematic queries.

**Resolution:**

1. **Check with your Loki administrator** about blocked queries and to review policy settings.
1. **Modify the query** to avoid the block pattern:
   - Change the stream selectors
   - Adjust the time range
   - Use different aggregations
1. **Request the block to be removed** if the query is legitimate.

**Configuration reference:**

```yaml
limits_config:
  blocked_queries:
    - pattern: ".*"          # Regex pattern to match
      regex: true
      types:                 # Query types to block
        - metric
        - filter
      hash: 0               # Or block specific query hash
```

**Properties:**

- Enforced by: Query Engine
- Retryable: No (unless block is removed)
- HTTP status: 400 Bad Request
- Configurable per tenant: Yes

### Error: Querying is disabled

**Error message:**

`querying is disabled, please contact your Loki operator`

**Cause:**

Query parallelism is set to 0, effectively disabling queries for the tenant.

**Resolution:**

1. **Contact your Loki administrator** to enable querying.
1. **Check configuration** for `max_query_parallelism`:

   ```yaml
   limits_config:
     max_query_parallelism: 32     #(the default)
   ```

**Properties:**

- Enforced by: Query Frontend
- Retryable: No (until configuration is fixed)
- HTTP status: 400 Bad Request
- Configurable per tenant: Yes

### Error: Multi variant queries disabled
Multi variant queries are an experimental feature that enables support for running multiple query variants over the same underlying data. For example, running both a `rate()` and `count_over_time()` query over the same range selector.

**Error message:**

`multi variant queries are disabled for this instance`

**Cause:**

The query uses the variants feature, but it's disabled for the tenant or instance.

**Resolution:**

1. **Remove variant expressions** from the query.
1. **Enable the feature** if needed:

   ```yaml
   limits_config:
     enable_multi_variant_queries: true  #default is false
   ```

**Properties:**

- Enforced by: Query Engine
- Retryable: No (until feature is enabled)
- HTTP status: 400 Bad Request
- Configurable per tenant: Yes

## Pipeline processing errors

Pipeline errors occur during log line processing but don't cause query failures. Instead, affected log lines are annotated with error labels.

### Understanding pipeline errors

When a pipeline stage fails (for example, parsing JSON that isn't valid JSON), Loki:

1. Does NOT filter out the log line
2. Adds an `__error__` label with the error type
3. Optionally adds `__error_details__` with more information
4. Passes the log line to the next pipeline stage

### Error types

| Error Label Value | Cause |
|------------------|-------|
| `JSONParserErr` | Log line is not valid JSON |
| `LogfmtParserErr` | Log line is not valid logfmt |
| `SampleExtractionErr` | Failed to extract numeric value for metrics |
| `LabelFilterErr` | Label filter operation failed |
| `TemplateFormatErr` | Template formatting failed |

### Viewing pipeline errors

To see logs with errors:

```logql
{app="foo"} | json | __error__!=""
```

To see error details:

```logql
{app="foo"} | json | __error__!="" | line_format "Error: {{.__error__}} - {{.__error_details__}}"
```

### Filtering out errors

To exclude logs with parsing errors:

```logql
{app="foo"} | json | __error__=""
```

To exclude specific error types:

```logql
{app="foo"} | json | __error__!="JSONParserErr"
```

### Dropping error labels

To remove error labels from results:

```logql
{app="foo"} | json | drop __error__, __error_details__
```

## Authentication and connection errors

These errors occur when connecting to Loki, often when using LogCLI.

### Error: No org ID

**Error message:**

`no org id`

**Cause:**

Multi-tenancy is enabled but no tenant ID was provided in the request.

**Resolution:**

1. **Add the X-Scope-OrgID header** in your request.
1. **For LogCLI**, use the `--org-id` flag:

   ```bash
   logcli query '{app="foo"}' --org-id="my-tenant"
   ```

1. **In Grafana**, configure the tenant ID in the data source settings.

**Properties:**

- Enforced by: Loki API
- Retryable: Yes (with tenant ID)
- HTTP status: 400 Bad Request
- Configurable per tenant: No

### Error: Authentication configuration conflict

**Error message:**

`at most one of HTTP basic auth (username/password), bearer-token & bearer-token-file is allowed to be configured`

Or:

`at most one of the options bearer-token & bearer-token-file is allowed to be configured`

**Cause:**

Multiple authentication methods are configured simultaneously in LogCLI.

**Resolution:**

1. **Use only one authentication method**:

   ```bash
   # Basic auth
   logcli query '{app="foo"}' --username="user" --password="pass"
   
   # OR bearer token
   logcli query '{app="foo"}' --bearer-token="token"
   
   # OR bearer token file
   logcli query '{app="foo"}' --bearer-token-file="/path/to/token"
   ```

**Properties:**

- Enforced by: LogCLI
- Retryable: Yes (with correct configuration)
- HTTP status: N/A (client-side error)
- Configurable per tenant: No

### Error: Run out of attempts while querying

**Error message:**

`run out of attempts while querying the server`

**Cause:**

LogCLI exhausted all retry attempts when trying to reach Loki. This usually indicates:

- Network connectivity issues
- Server unavailability
- Authentication failures

**Resolution:**

1. **Check Loki server availability**.
1. **Verify network connectivity**.
1. **Check authentication credentials**.
1. **Increase retries** if transient issues are expected:

   ```bash
   logcli query '{app="foo"}' --retries=5
   ```

**Properties:**

- Enforced by: LogCLI
- Retryable: Yes (automatic retries exhausted)
- HTTP status: Varies
- Configurable per tenant: No

### Error: WebSocket connection closed unexpectedly

**Error message:**

`websocket: close 1006 (abnormal closure): unexpected EOF`

**Cause:**

When tailing logs, the WebSocket connection was closed unexpectedly. This can happen if:

- The querier handling the tail request stopped
- Network interruption occurred
- Server-side timeout

**Resolution:**

1. LogCLI will automatically attempt to reconnect, up to 5 times.
1. **Check Loki querier health** if reconnections fail.
1. **Review network stability** between client and server.

**Properties:**

- Enforced by: Network/Server
- Retryable: Yes (automatic reconnection)
- HTTP status: N/A (WebSocket error)
- Configurable per tenant: No

## Data availability errors

These errors occur when requested data is not available.

### Error: No data found

**Error message:**

`no data found`

Or an empty result set with no error message.

**Cause:**

The query time range contains no matching log data. This can happen if:

- No logs match the stream selectors
- The time range is outside the data retention period
- Log ingestion is not working
- Stream labels don't match any existing streams

**Resolution:**

1. **Verify the time range** contains data for your streams.
1. **Check if log ingestion is working** correctly:

   ```bash
   # Check if any data is being ingested
   logcli query '{job=~".+"}'
   ```

1. **Verify stream selectors** match existing log streams:

   ```bash
   # List available streams
   curl http://loki:3100/loki/api/v1/series
   ```

1. **Check data retention** settings to ensure logs are still available.
1. **Use broader selectors** to test if any data exists:

   ```logql
   {job=~".+"}
   ```

**Properties:**

- Enforced by: Query Engine
- Retryable: Yes (with different parameters)
- HTTP status: 200 OK (with empty result)
- Configurable per tenant: No

### Error: Index not ready

**Error message:**

`index not ready`

Or:

`index gateway not ready for time range`

**Cause:**

The index for the requested time range is not yet available for querying. This can happen when:

- Index files are still being synced from storage
- The index gateway is still starting up
- Querying data older than the configured ready index period

**Default configuration:**

- `query_ready_index_num_days`: 0 (all indexes are considered ready)

**Resolution:**

1. **Wait for the index to become available** - this is often a temporary issue during startup.
1. **Query more recent data** that's available in ingesters:

   ```logql
   {app="foo"} # Query last few hours instead of older data
   ```

1. **Check the configuration** for index readiness:

   ```yaml
   query_range:
     query_ready_index_num_days: 7  #default is 0
   ```

1. **Verify index synchronization** is working correctly by checking ingester and index gateway logs.

**Properties:**

- Enforced by: Index Gateway/Querier
- Retryable: Yes (wait and retry)
- HTTP status: 503 Service Unavailable
- Configurable per tenant: No

### Error: Tenant limits

**Error message:**

`max concurrent tail requests limit exceeded, count > limit (10 > 5)`

**Cause:**

The tenant has exceeded the maximum number of concurrent streaming (tail) requests. This limit protects the cluster from excessive resource consumption by real-time log streaming.

**Default configuration:**

- `max_concurrent_tail_requests`: 10

**Resolution:**

1. **Reduce the number of concurrent tail/streaming queries**.
1. **Use batch queries** instead of real-time streaming where possible:

   ```logql
   # Instead of tailing in real-time
   # Use periodic range queries
   {app="foo"} |= "error"
   ```

1. **Increase the limit** if more concurrent tails are needed:

   ```yaml
   limits_config:
     max_concurrent_tail_requests: 20  #default is 10
   ```

**Properties:**

- Enforced by: Querier
- Retryable: Yes (when connections are available)
- HTTP status: 429 Too Many Requests
- Configurable per tenant: Yes

## Storage errors

These errors occur when Loki cannot read data from storage.

### Error: Failed to load chunk

**Error message:**

`failed to load chunk '<chunk_key>'`

**Cause:**

Loki couldn't retrieve a chunk from object storage. Possible causes:

- Chunk was deleted or moved
- Storage permissions issue
- Network connectivity to storage
- Storage service unavailable

**Resolution:**

1. **Check storage connectivity** from Loki components.
1. **Verify storage credentials and permissions**.
1. **Check for chunk corruption** or deletion.
1. **Review storage service status**.

**Properties:**

- Enforced by: Storage Client
- Retryable: Yes (automatically)
- HTTP status: 500 Internal Server Error
- Configurable per tenant: No

### Error: Object not found in storage

**Error message:**

`object not found in storage`

**Cause:**

The requested chunk or object doesn't exist in storage. This might happen if:

- Data was deleted due to retention
- Compaction removed the chunk
- Chunk was never written successfully

**Resolution:**

1. **Check if data is within retention period**.
1. **Verify data was ingested successfully**.
1. **Review compaction jobs** for issues.

**Properties:**

- Enforced by: Storage Client
- Retryable: No (data doesn't exist)
- HTTP status: 404 or 500 depending on context
- Configurable per tenant: No

### Error: Failed to decode chunk

**Error message:**

`failed to decode chunk '<chunk_key>' for tenant '<tenant>': <error>`

**Cause:**

A chunk was retrieved from storage but couldn't be decoded. This indicates chunk corruption.

**Resolution:**

1. **Report to Loki administrators** for investigation.
1. **Check for storage data integrity issues**.
1. Note that the corrupted chunk data may be unrecoverable.

**Properties:**

- Enforced by: Storage Client
- Retryable: No (chunk is corrupted)
- HTTP status: 500 Internal Server Error
- Configurable per tenant: No

## Troubleshooting workflow

Follow this workflow when investigating query issues:

1. **Check the error message** - Identify which category of error you're encountering.

1. **Review query syntax** - Use the LogQL documentation to validate your query.

1. **Check query statistics** - In Grafana, enable "Query Inspector" to see:
   - Bytes processed
   - Number of chunks scanned
   - Execution time breakdown

1. **Simplify the query** - Start with a basic selector and add complexity:

   ```logql
   # Start simple
   {app="foo"}
   
   # Add filters
   {app="foo"} |= "error"
   
   # Add parsing
   {app="foo"} |= "error" | json
   
   # Add label filters
   {app="foo"} |= "error" | json | level="error"
   ```

1. **Check metrics** for query performance:

   ```promql
   # Query latency
   histogram_quantile(0.99, sum(rate(loki_request_duration_seconds_bucket[5m])) by (le, route))
   
   # Query errors
   sum by (status_code) (rate(loki_request_duration_seconds_count[5m]))
   ```

1. **Review Loki logs** for detailed error information:

   ```bash
   kubectl logs -l app=loki-read --tail=100 | grep -i error
   ```

1. **Test with LogCLI** for more detailed output:

   ```bash
   logcli query '{app="foo"}' --stats --limit=10
   ```

## Related resources

- Learn more about [LogQL Query Language](https://grafana.com/docs/loki/<LOKI_VERSION>/query/)
- Configure appropriate [query limits](https://grafana.com/docs/loki/<LOKI_VERSION>/configure/#limits_config)
- Learn more about [Query performance tuning](https://grafana.com/docs/loki/<LOKI_VERSION>/operations/query-acceleration/)
- Review the [LogCLI documentation](https://grafana.com/docs/loki/<LOKI_VERSION>/query/logcli/)
- Learn more about [LogQL query optimization](https://grafana.com/docs/loki/<LOKI_VERSION>/query/log_queries/)
- Review [query performance best practices](https://grafana.com/docs/loki/<LOKI_VERSION>/best-practices/)
- Use [query debugging features](https://grafana.com/docs/loki/<LOKI_VERSION>/query/query_stats/) to analyze slow queries
- Explore the [Grafana Loki GitHub repository](https://github.com/grafana/loki) for community support
