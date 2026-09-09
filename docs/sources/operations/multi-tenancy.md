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

Tenant IDs must not be longer than 150 bytes and can only include a specific
set of characters; see [Restrictions](#restrictions) below for the full rules.
Operators are recommended to use a reasonable limit for uniquely identifying
tenants; 20 bytes is usually enough.

Loki defaults to running in multi-tenant mode.
Multi-tenant mode is set in the configuration with `auth_enabled: true`, or with the equivalent command line flag `-auth.enabled` (default `true`).

When configured with `auth_enabled: false`, Loki uses a single tenant.
The `X-Scope-OrgID` header is not required in Loki API requests.
The single tenant ID defaults to `fake` and can be changed via the `no_auth_tenant` configuration option.
On a fresh cluster with an empty bucket this is safe to change at any time; on a cluster that has already
written data, changing the tenant ID requires migrating existing data to the new tenant path (see [Loki Migrate Tool](https://github.com/grafana/loki/tree/main/cmd/migrate)).

When `auth_enabled: true`, a request that does not include the `X-Scope-OrgID` header fails with an HTTP `401 Unauthorized` error and the message `no org id`.
For more information about setting this header, including examples of configuring a reverse proxy to add it, refer to [Manage authentication](https://grafana.com/docs/loki/<LOKI_VERSION>/operations/authentication/).
To troubleshoot this error, refer to [No org ID](https://grafana.com/docs/loki/<LOKI_VERSION>/operations/troubleshooting/troubleshoot-operations/#error-no-org-id).

## Multi-tenant Queries

In multi-tenant mode, queries may gather results from multiple tenants.
Set the querier configuration option `multi_tenant_queries_enabled: true`, or the equivalent command line flag `-querier.multi-tenant-queries-enabled` (default `false`), to enable queries across tenants.
The query API request defines the tenants.
Specify multiple tenants
in the query request HTTP header `X-Scope-OrgID` by separating the tenant IDs with the pipe character (`|`).
For example, a query for tenants `A` and `B` requires the header `X-Scope-OrgID: A|B`.

Only query endpoints support multi-tenant calls.
Calls to `GET /loki/api/v1/tail`, `POST /loki/api/v1/push`, and `POST /otlp/v1/logs` will return an HTTP 400 error if more than one tenant is defined in the HTTP header.

Instant and range queries support label filtering using tenant IDs.
For example, the query

```logql
{app="foo", __tenant_id__=~"a.+"} | logfmt
```

will return results for all tenants
that have a tenant ID that begins with the character `a`.

The `__tenant_id__` label is not stored with the log data.
Loki adds it to the results only when a query spans more than one tenant.
If the request names a single tenant, the results do not include this label.

If the label `__tenant_id__` is already present in a log stream, it is prepended with the string `original_`.

Tenant ID filtering in stages is not supported.
An example of a query that will _not_ work:

```logql
{app="foo"} | __tenant_id__="1" | logfmt
```

## Restrictions

Tenant IDs must not be longer than 150 bytes and can only include the following characters:

- Alphanumeric characters
  - `0-9`
  - `a-z`
  - `A-Z`
- Special characters
  - Exclamation point (`!`)
  - Hyphen (`-`)
  - Underscore (`_`)
  - Single period (`.`)
  - Asterisk (`*`)
  - Single quote (`'`)
  - Open parenthesis (`(`)
  - Close parenthesis (`)`)

{{< admonition type="note" >}}
For security reasons, `.` and `..` aren't valid tenant IDs.
{{< /admonition >}}

{{< admonition type="warning" >}}
Do not include a colon (`:`) in the `X-Scope-OrgID` header.
A colon is not a valid tenant ID character.
Most Loki components stop reading the tenant ID at the first colon and ignore the rest of the value, and the ignored part is not checked against the rules on this page.
Other components read the whole header value instead, so a header that contains a colon can produce inconsistent results.
{{< /admonition >}}
