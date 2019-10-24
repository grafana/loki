# Authentication with Loki

Loki does not come with any included authentication layer. Operators are
expected to run an authenticating reverse proxy in front of your services, such
as NGINX using basic auth or an OAuth2 proxy.

Note that when using Loki in multi-tenant mode, Loki requires the HTTP header
`X-Scope-OrgID` to be set to a string identifying the user; the responsibility
of populating this value should be handled by the authenticating reverse proxy.
For more information on multi-tenancy please read its
[documentation](multi-tenancy.md).

For information on authenticating Promtail, please see the docs for [how to
configure Promtail](../clients/promtail/configuration.md).
