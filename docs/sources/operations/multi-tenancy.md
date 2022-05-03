---
title: Multi-tenancy
weight: 50
---
# Grafana Loki Multi-Tenancy

Grafana Loki is a multi-tenant system; requests and data for tenant A are isolated from
tenant B. Requests to the Loki API should include an HTTP header
(`X-Scope-OrgID`) that identifies the tenant for the request.

Tenant IDs can be any alphanumeric string that fits within the Go HTTP header
limit (1MB). Operators are recommended to use a reasonable limit for uniquely
identifying tenants; 20 bytes is usually enough.

To run in multi-tenant mode, Loki should be started with `auth_enabled: true`.

Loki can be run in "single-tenant" mode where the `X-Scope-OrgID` header is not
required. In single-tenant mode, the tenant ID defaults to `fake`.

## Multi-tenant Queries

If run in multi-tenant mode, queries across different tenants can be enabled via
`multi_tenant_queries_enabled: true` option in the querier. Once enabled multiple
tenant IDs can be defined in the HTTP header `X-Scope-OrgID` by concatenating them
with `|`. For instance a query for tenant A and B can set `X-Scope-OrgID: A|B`.

Only query endpoints support multi-tenant calls. Calls to `GET /loki/api/v1/tail`
and `POST /loki/api/v1/push` will return an HTTP 400 error if more than one tenant
is defined in the HTTP header.

Instant and range queries support label filtering on the tenant IDs. For example
`{app="foo", __tenant_id__=~"a.+"} | logfmt` will return results for all tenants
whose ID stat with `a`. Tenant ID filtering in stages is not supported; `{app="foo"} | __tenant_id__="1" | logfmt` will not work.

In case the label `__tenant_id__` is already present in a log stream it is prepended with `original_`.
