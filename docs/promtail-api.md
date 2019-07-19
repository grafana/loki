# Promtail API

Promtail features an embedded web server exposing a web console at `/` and the following API endpoints:

- `GET /ready`

  This endpoint returns 200 when Promtail is up and running, and there's at least one working target.

- `GET /metrics`

  This endpoint returns Promtail metrics for Prometheus. See "[Operations > Observability > Metrics](./operations.md)" to have a list of exported metrics.


## Promtail web server config

The web server exposed by Promtail can be configured in the promtail `.yaml` config file:

```
server:
  http_listen_host: 127.0.0.1
  http_listen_port: 9080
```
