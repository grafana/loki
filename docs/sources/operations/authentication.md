---
title: Authentication
description: Authentication
weight: 10
---
# Authentication

Grafana Loki does not come with any included authentication layer. Operators are
expected to run an authenticating reverse proxy in front of your services, such
as NGINX using basic auth or an OAuth2 proxy.

Note that when using Loki in multi-tenant mode, Loki requires the HTTP header
`X-Scope-OrgID` to be set to a string identifying the tenant; the responsibility
of populating this value should be handled by the authenticating reverse proxy.
Read the [multi-tenancy]({{<relref "multi-tenancy">}}) documentation for more information.

For information on authenticating Promtail, please see the docs for [how to
configure Promtail]({{<relref "../clients/promtail/configuration">}}).
