# Wildcard Tenant Queries

{{< docs/experimental product="Loki" >}}

Wildcard tenant queries allow you to query logs across all tenants in a multi-tenant Loki deployment without explicitly listing each tenant ID. This is particularly useful for administrators and monitoring tools that need visibility across the entire system.

## Overview

By default, Loki requires you to specify tenant IDs explicitly in the `X-Scope-OrgID` header. For multi-tenant queries, you can use pipe-separated values like `tenant1|tenant2|tenant3`. However, this becomes unwieldy when you have many tenants or when tenants are frequently added or removed.

Wildcard tenant queries introduce support for the special `*` character, which automatically expands to include all known tenants discovered from Loki's storage.

## Requirements

- Multi-tenant mode must be enabled (`auth_enabled: true`)
- Multi-tenant queries must be enabled (`querier.multi_tenant_queries_enabled: true`)
- Wildcard tenant queries must be enabled (`querier.wildcard_tenant_queries_enabled: true`)

## Configuration

Add the following to your Loki configuration:

```yaml
querier:
  # Required: Enable multi-tenant queries
  multi_tenant_queries_enabled: true
  
  # Enable wildcard tenant queries (experimental)
  wildcard_tenant_queries_enabled: true
  
  # How long to cache the list of discovered tenants (default: 5m)
  wildcard_tenant_cache_ttl: 5m
```

Or via command-line flags:

```bash
-querier.multi-tenant-queries-enabled=true
-querier.wildcard-tenant-queries-enabled=true
-querier.wildcard-tenant-cache-ttl=5m
```

## Usage

### Query All Tenants

To query all tenants, set the `X-Scope-OrgID` header to `*`:

```bash
curl -H "X-Scope-OrgID: *" \
  "http://loki:3100/loki/api/v1/query_range" \
  --data-urlencode 'query={job=~".+"}'
```

### Query All Tenants Except Specific Ones

You can exclude specific tenants using the `!` prefix:

```bash
# Query all tenants except "internal" and "test"
curl -H "X-Scope-OrgID: *|!internal|!test" \
  "http://loki:3100/loki/api/v1/query_range" \
  --data-urlencode 'query={job=~".+"}'
```

### Grafana Data Source Configuration

To configure a Grafana data source for wildcard tenant queries:

1. Add a new Loki data source in Grafana
2. In the HTTP Headers section, add:
   - Header: `X-Scope-OrgID`
   - Value: `*` (or `*|!tenant1|!tenant2` for exclusions)

Example using Grafana provisioning:

```yaml
apiVersion: 1
datasources:
  - name: Loki-Admin
    type: loki
    access: proxy
    url: http://loki-gateway:3100
    jsonData:
      httpHeaderName1: 'X-Scope-OrgID'
    secureJsonData:
      httpHeaderValue1: '*'
```

## How It Works

When a query is received with `*` in the `X-Scope-OrgID` header:

1. Loki's querier detects the wildcard character
2. It queries the index storage to discover all tenant IDs that have written data
3. Results are cached for the duration specified by `wildcard_tenant_cache_ttl`
4. Any exclusions (prefixed with `!`) are removed from the list
5. The query is executed across all remaining tenants
6. Results are merged with a `__tenant_id__` label added to each log entry

## Performance Considerations

### Tenant Discovery Caching

Discovering tenants requires listing files from object storage, which can be expensive. The results are cached for `wildcard_tenant_cache_ttl` (default: 5 minutes). 

**Trade-offs:**
- Shorter TTL: New tenants appear faster, but more storage operations
- Longer TTL: Fewer storage operations, but new tenants take longer to appear

### Query Performance

Wildcard queries fan out to all tenants in parallel. Keep in mind:

- Queries touching many tenants will use more resources
- Consider using label selectors to reduce the amount of data scanned
- Monitor query latency and adjust limits as needed

### Resource Limits

Wildcard queries are subject to the same limits as regular multi-tenant queries:

- `limits_config.max_query_parallelism`
- `limits_config.max_query_series`
- `limits_config.query_timeout`

Consider adjusting these limits if you have many tenants.

## API Endpoints Supported

Wildcard tenant queries work with all query endpoints:

- `/loki/api/v1/query` - Instant queries
- `/loki/api/v1/query_range` - Range queries
- `/loki/api/v1/labels` - Label names
- `/loki/api/v1/label/{name}/values` - Label values
- `/loki/api/v1/series` - Series metadata
- `/loki/api/v1/index/stats` - Index statistics
- `/loki/api/v1/index/volume` - Log volume

## Tenant Label

When querying multiple tenants (including wildcard queries), Loki automatically adds a `__tenant_id__` label to each log entry. This allows you to:

- Filter results by tenant: `{job="app"} | __tenant_id__="tenant1"`
- Aggregate by tenant in LogQL: `sum by(__tenant_id__) (rate({job="app"}[5m]))`
- Display tenant information in Grafana dashboards

## Troubleshooting

### "wildcard tenant queries are not enabled"

Ensure both configuration options are set:

```yaml
querier:
  multi_tenant_queries_enabled: true
  wildcard_tenant_queries_enabled: true
```

### "wildcard query resolved to zero tenants"

This occurs when:
- No tenants have written data yet
- All discovered tenants were excluded
- There's an issue with the storage backend

Check the querier logs for more details.

### "tenant exclusion (!) syntax requires wildcard (*)"

You cannot use exclusion syntax without the wildcard:

```bash
# Invalid - exclusion without wildcard
X-Scope-OrgID: tenant1|!tenant2

# Valid - exclusion with wildcard  
X-Scope-OrgID: *|!tenant2
```

### Slow Queries

If wildcard queries are slow:

1. Check the number of tenants discovered (logged at INFO level)
2. Increase `wildcard_tenant_cache_ttl` to reduce storage operations
3. Use more specific label selectors to reduce data scanned
4. Consider excluding inactive tenants

## Security Considerations

- Wildcard queries bypass tenant isolation by design
- Only grant wildcard access to trusted administrators
- Consider using RBAC in Grafana to control who can use admin data sources
- Audit log access when using wildcard queries in production

## Known Limitations

- Tenant discovery relies on index storage - tenants with only recent (not yet indexed) data may not appear immediately
- The cache is per-querier instance - in a distributed setup, different queriers may have slightly different tenant lists during cache refresh
- Detected fields endpoint returns limited results for multi-tenant queries
