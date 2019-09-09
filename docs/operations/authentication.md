# Authentication with Loki

Loki does not come with any included authentication layer. Operators are
expected to run an authenticating reverse proxy in front of your services, such
as Nginx using basic auth or an OAuth2 proxy.

For information on authenticating promtail, please see the docs for [how to
configure Promtail](../clients/promtail/configuration.md).
