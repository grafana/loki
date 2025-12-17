---
title: Manage authentication
menuTitle: Authentication
description: Describes how to add authentication to Grafana Loki.
weight: 
---
# Manage authentication

Grafana Loki does not come with any included authentication layer. You must run an authenticating reverse proxy in front of your services.

The simple scalable and microservices [deployment modes](https://grafana.com/docs/loki/<LOKI_VERSION>/get-started/deployment-modes/) require a reverse proxy to be deployed in front of Loki, to direct client API requests to the various components.

By default the Loki Helm chart includes a default reverse proxy configuration, using an nginx container to handle routing traffic and authorization.

A list of open-source reverse proxies you can use:

- [HAProxy](https://docs.haproxy.org/ )
- [nginx](https://docs.nginx.com/nginx/) using their [guide on restricting access with HTTP basic authentication](https://docs.nginx.com/nginx/admin-guide/security-controls/configuring-http-basic-authentication/)
- [OAuth2 proxy](https://oauth2-proxy.github.io/oauth2-proxy/)
- [Pomerium](https://www.pomerium.com/docs), which has a [guide for securing Grafana](https://www.pomerium.com/docs/guides/grafana)

{{< admonition type="note" >}}
When using Loki in multi-tenant mode, Loki requires the HTTP header
`X-Scope-OrgID` to be set to a string identifying the tenant; the responsibility
of populating this value should be handled by the authenticating reverse proxy.
For more information, read the [multi-tenancy](https://grafana.com/docs/loki/<LOKI_VERSION>/operations/multi-tenancy/) documentation.{{< /admonition >}}

For information on authenticating Promtail, see the documentation for [how to
configure Promtail](https://grafana.com/docs/loki/<LOKI_VERSION>/send-data/promtail/configuration/).

## Enable basic authentication for Loki using nginx

This section describes the process of enabling basic authentication for Loki using [nginx](https://docs.nginx.com/nginx/).

### Prerequisites

* A running Loki instance
* A running nginx instance

### Configure nginx

You must create a new nginx configuration file for the Loki instance.

This example assumes the following:

* nginx is running in `/opt/homebrew`
* Loki is running on port 3100 on the local machine
* Your Loki tenant id is `fake`
* The configuration file is named `/opt/homebrew/etc/nginx/loki.conf`

If you used different configuration parameters for Loki, adjust the examples to match your configuration.

`loki.conf` configuration:

```conf
upstream loki {
  server 127.0.0.1:3100;
  keepalive 15;
}

server {
  listen 80;
  server_name loki.localhost;

  auth_basic "loki auth";
  auth_basic_user_file /opt/homebrew/etc/nginx/passwords;

  location / {
    proxy_read_timeout 1800s;
    proxy_connect_timeout 1600s;
    proxy_pass http://loki;
    proxy_http_version 1.1;
    proxy_set_header Upgrade $http_upgrade;
    proxy_set_header Connection "Keep-Alive";
    proxy_set_header Proxy-Connection "Keep-Alive";
    proxy_redirect off;
  }

  location /ready {
    proxy_pass http://loki;
    proxy_http_version 1.1;
    proxy_set_header Connection "Keep-Alive";
    proxy_set_header Proxy-Connection "Keep-Alive";
    proxy_redirect off;
    auth_basic "off";
  }
}
```

This configuration must be included in your main nginx configuration, for example, by including it in `nginx.conf` like:

```
include /opt/homebrew/etc/nginx/loki.conf;
```

Restart the nginx server to ensure all configuration changes are updated.

### Validate your nginx configuration

To validate the nginx configuration for Loki, you can send a `curl` request to two endpoints:

* The `/ready` endpoint, which is not protected by a basic authentication mechanism.

```curl
% curl -i http://loki.localhost/ready

HTTP/1.1 200 OK
Server: nginx/1.29.2
Date: Thu, 16 Oct 2025 14:28:31 GMT
Content-Type: text/plain; charset=utf-8
Content-Length: 6
Connection: keep-alive
X-Content-Type-Options: nosniff

ready
```

* The `/` endpoint, which is protected by a basic authentication mechanism.

```curl
curl -i http://loki.localhost/

HTTP/1.1 401 Unauthorized
Server: nginx/1.29.2
Date: Thu, 16 Oct 2025 14:32:43 GMT
Content-Type: text/html
Content-Length: 179
Connection: keep-alive
WWW-Authenticate: Basic realm="loki auth"

<html>
<head><title>401 Authorization Required</title></head>
<body>
<center><h1>401 Authorization Required</h1></center>
<hr><center>nginx/1.29.2</center>
</body>
</html>
```

### Update passwords

The password file can be seeded using whatever mechanism you may use for other web services.

In this example, `htpasswd` is utilized:

```
% htpasswd -c /opt/homebrew/etc/nginx/passwords loki123

New password:
Re-type new password:
Adding password for user loki123
```

Restart the nginx server to ensure all configuration changes are updated.

### Validate passwords

Enter your password into a temporary file, such as:

```
% vi lokipw
```

Then, store it as an environment variable::

```
% pass=$(cat lokipw)
```

You can validate basic authentication is then working by issuing a curl command to the protected resource:

```curl
curl -i -u loki123:$pass -H "X-Scope-OrgID:fake" "http://loki.localhost/loki/api/v1/labels"

HTTP/1.1 200 OK
Server: nginx/1.29.2
Date: Thu, 16 Oct 2025 14:46:09 GMT
Content-Type: application/json; charset=UTF-8
Content-Length: 21
Connection: keep-alive

{"status":"success"}
```
