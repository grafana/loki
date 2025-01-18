---
title: Manage authentication
menuTitle: Authentication
description: Describes how to add authentication to Grafana Loki.
weight: 
---
# Manage authentication

Grafana Loki does not come with any included authentication layer. Operators are
expected to run an authenticating reverse proxy in front of your services.

The simple scalable [deployment mode]({{< relref "../get-started/deployment-modes" >}}) requires a reverse proxy to be deployed in front of Loki, to direct client API requests to either the read or write nodes. The Loki Helm chart includes a default reverse proxy configuration, using Nginx.

A list of open-source reverse proxies you can use:

-  [Pomerium](https://www.pomerium.com/docs), which has a [guide for securing Grafana](https://www.pomerium.com/docs/guides/grafana)
-  [NGINX](https://docs.nginx.com/nginx/) using their [guide on restricting access with HTTP basic authentication](https://docs.nginx.com/nginx/admin-guide/security-controls/configuring-http-basic-authentication/)
-  [OAuth2 proxy](https://github.com/oauth2-proxy/oauth2-proxy)
-  [HAProxy](https://www.haproxy.org/)

{{< admonition type="note" >}}
When using Loki in multi-tenant mode, Loki requires the HTTP header
`X-Scope-OrgID` to be set to a string identifying the tenant; the responsibility
of populating this value should be handled by the authenticating reverse proxy.
For more information, read the [multi-tenancy]({{< relref "./multi-tenancy" >}}) documentation.{{< /admonition >}}

For information on authenticating Promtail, see the documentation for [how to
configure Promtail]({{< relref "../send-data/promtail/configuration" >}}).
