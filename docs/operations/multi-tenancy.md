# Loki Multi-Tenancy

Loki is a multitenant system; requests and data for tenant A are isolated from
tenant B. Requests to the Loki API should include an HTTP header
(`X-Scope-OrgID`) that identifies the the tenant for the request.

Tenant IDs can be any alphanumeric string; limiting them to 20 bytes is
reasonable. To run in multitenant mode, loki should be started with
`auth_enabled: true`.

Loki can be run in "single-tenant" mode where the `X-Scope-OrgID` header is not
required. In single-tenant mode, the tenant ID defaults to `fake`.

