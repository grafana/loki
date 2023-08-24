---
title: Authentication
description: Authentication
weight: 10
---
# Authentication

Grafana Loki does not come with any included authentication layer. Operators are
expected to run an authenticating reverse proxy in front of your services. A helpful list of open-source reverse proxies to use:

-  [Pomerium](https://www.pomerium.com/docs), which has a [guide for securing Grafana](https://www.pomerium.com/docs/guides/grafana)
-  [NGINX](https://docs.nginx.com/nginx/) using their [guide on restricting access with HTTP basic authentication](https://docs.nginx.com/nginx/admin-guide/security-controls/configuring-http-basic-authentication/)
-  [OAuth2 proxy](https://github.com/oauth2-proxy/oauth2-proxy)
-  [HAProxy](https://www.haproxy.org/)


Note that when using Loki in multi-tenant mode, Loki requires the HTTP header
`X-Scope-OrgID` to be set to a string identifying the tenant; the responsibility
of populating this value should be handled by the authenticating reverse proxy.
Read the [multi-tenancy]({{< relref "./multi-tenancy" >}}) documentation for more information.

For information on authenticating Promtail, please see the docs for [how to
configure Promtail]({{< relref "../send-data/promtail/configuration" >}}).
