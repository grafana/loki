---
title: Manage authentication
menuTitle: Authentication
description: Describes how to add authentication to Grafana Loki.
weight: 
---
# Manage authentication

Grafana Loki does not come with any included authentication layer. You must run an authenticating reverse proxy in front of your services.

The simple scalable and microservices [deployment modes](https://grafana.com/docs/loki/latest<LOKI_VERSION)/get-started/deployment-modes/) require a reverse proxy to be deployed in front of Loki, to direct client API requests to the various components.

By default the Loki Helm chart includes a default reverse proxy configuration, using an nginx container to handle routing traffic and authorization.

A list of open-source reverse proxies you can use:

- [HAProxy](https://docs.haproxy.org/ )
- [NGINX](https://docs.nginx.com/nginx/) using their [guide on restricting access with HTTP basic authentication](https://docs.nginx.com/nginx/admin-guide/security-controls/configuring-http-basic-authentication/)
- [OAuth2 proxy](https://oauth2-proxy.github.io/oauth2-proxy/)
- [Pomerium](https://www.pomerium.com/docs), which has a [guide for securing Grafana](https://www.pomerium.com/docs/guides/grafana)

{{< admonition type="note" >}}
When using Loki in multi-tenant mode, Loki requires the HTTP header
`X-Scope-OrgID` to be set to a string identifying the tenant; the responsibility
of populating this value should be handled by the authenticating reverse proxy.
For more information, read the [multi-tenancy](https://grafana.com/docs/loki/<LOKI_VERSION>/operations/multi-tenancy/) documentation.{{< /admonition >}}

For information on authenticating Promtail, see the documentation for [how to
configure Promtail](https://grafana.com/docs/loki/<LOKI_VERSION>/send-data/promtail/configuration/).
