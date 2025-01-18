---
title: Manage tenant isolation
menuTitle: Multi-tenancy
description: Describes how Grafana Loki implements multi-tenancy to isolate tenant data and queries.
weight: 
---
# Manage tenant isolation

Grafana Loki is a multi-tenant system; requests and data for tenant A are isolated from
tenant B. Requests to the Loki API should include an HTTP header
(`X-Scope-OrgID`) that identifies the tenant for the request.

Tenant IDs can be any alphanumeric string that fits within the Go HTTP header
limit (1MB). Operators are recommended to use a reasonable limit for uniquely
identifying tenants; 20 bytes is usually enough.

Loki defaults to running in multi-tenant mode.
Multi-tenant mode is set in the configuration with `auth_enabled: true`.

When configured with `auth_enabled: false`, Loki uses a single tenant.
The `X-Scope-OrgID` header is not required in Loki API requests.
The single tenant ID will be the string `fake`.

## Multi-tenant Queries

In multi-tenant mode, queries may gather results from multiple tenants.
Set the querier configuration option `multi_tenant_queries_enabled: true` to enable queries across tenants.
The query API request defines the tenants.
Specify multiple tenants
in the query request HTTP header `X-Scope-OrgID` by separating the tenant IDs with the pipe character (`|`).
For example, a query for tenants `A` and `B` requires the header `X-Scope-OrgID: A|B`.

Only query endpoints support multi-tenant calls.
Calls to `GET /loki/api/v1/tail` and `POST /loki/api/v1/push` will return an HTTP 400 error if more than one tenant is defined in the HTTP header.

Instant and range queries support label filtering using tenant IDs.
For example, the query

```
{app="foo", __tenant_id__=~"a.+"} | logfmt
```
will return results for all tenants
that have a tenant ID that begins with the character `a`.

If the label `__tenant_id__` is already present in a log stream, it is prepended with the string `original_`.

Tenant ID filtering in stages is not supported.
An example of a query that will _not_ work:

```
{app="foo"} | __tenant_id__="1" | logfmt
```
