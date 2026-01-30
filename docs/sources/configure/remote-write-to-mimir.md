# Remote Write Metrics to Grafana Mimir

This guide explains how to send metrics to Grafana Mimir using **Prometheus** or **Grafana Agent**.

> Note: Loki itself does not support `remote_write`.  
> To push metrics to Mimir, you configure `remote_write` on Prometheus or Grafana Agent.

---

## Prerequisites

- Running Grafana Mimir instance  
- HTTP endpoint for pushing metrics  
- Tenant ID and authentication token (if applicable)

---

## Using Prometheus remote_write

If you are using **Prometheus**, add the following to your `prometheus.yml`:

```yaml
remote_write:
  - url: https://mimir.example.com/api/v1/push
    basic_auth:
      username: <tenant-id>
      password: <token>
```

## Using Grafana Agent remote_write

If you are using **Grafana Agent**, add the following to your Agent configuration file:

```yaml
metrics:
  wal_directory: /tmp/grafana-agent-wal

  global:
    scrape_interval: 15s

  configs:
    - name: agent
      scrape_configs:
        - job_name: 'agent'
          static_configs:
            - targets: ['localhost:9090']

      remote_write:
        - url: https://mimir.example.com/api/v1/push
          basic_auth:
            username: <tenant-id>
            password: <token>
```
