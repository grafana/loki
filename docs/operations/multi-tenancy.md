# Loki Multi-Tenancy

Loki is a multi-tenant system; requests and data for tenant A are isolated from
tenant B. Requests to the Loki API should include an HTTP header
(`X-Scope-OrgID`) that identifies the tenant for the request.

Tenant IDs can be any alphanumeric string that fits within the Go HTTP header
limit (1MB). Operators are recommended to use a reasonable limit for uniquely
identifying tenants; 20 bytes is usually enough.

To run in multi-tenant mode, Loki should be started with `auth_enabled: true`.

Loki can be run in "single-tenant" mode where the `X-Scope-OrgID` header is not
required. In single-tenant mode, the tenant ID defaults to `fake`.

