# Loki Documentation

<p align="center"> <img src="logo_and_name.png" alt="Loki Logo"> <br>
  <small>Like Prometheus, but for logs!</small> </p>

Grafana Loki is a set of components that can be compsed into a fully featured
logging stack.

Unlike other logging systems, Loki is built around the idea of only indexing
labels for logs and leaving the original log message unindexed. This means
that Loki is cheaper to operate and can be orders of magnitude more efficent.

## Table of Contents

1. [Overview](overview/README.md)
    1. [Comparison to other Log Systems](overview/comparisons.md)
2. [Installation](installation/README.md)
    1. [Installing Locally](installation/local.md)
    2. [Installing with Helm](installation/helm.md)
    3. [Installing with Ksonnet](installation/ksonnet.md)
3. [Getting Started](getting-started/README.md)
    1. [Grafana](getting-started/grafana.md)
    2. [Logcli](getting-started/logcli.md)
    4. [Troubleshooting](getting-started/troubleshooting.md)
4. [Configuration](configuration/README.md)
    1. [Examples](configuration/examples.md)
5. [Clients](clients/README.md)
    1. [promtail](clients/promtail/README.md)
        1. [Installation](clients/promtail/installation.md)
        2. [Configration](clients/promtail/configuration.md)
        3. [Scraping](clients/promtail/scraping.md)
        4. [Pipelines](clients/promtail/pipelines.md)
        5. [Troubleshooting](clients/promtail/troubleshooting.md)
    2. [Docker Driver](clients/docker-driver/README.md)
        1. [Configration](clients/docker-driver/configuration.md)
    3. [fluentd](clients/fluentd.md)
    4. [fluent-bit](clients/fluent-bit.md)
6. [LogQL](logql.md)
7. [Operations](operations/README.md)
    1. [Authentication](operations/authentication.md)
    2. [Observability](operations/observability.md)
    3. [Scalability](operations/scalability.md)
    4. [Storage](operations/storage/README.md)
        1. [Table Manager](operations/storage/table-manager.md)
        2. [Retention](operations/storage/retention.md)
    5. [Multi-tenancy](operations/multi-tenancy.md)
    6. [Loki Canary](operations/loki-canary.md)
8. [HTTP API](api.md)
9. [Architecture](architecture.md)
10. [Community](community/README.md)
    1. [Governance](community/governance.md)
    2. [Getting in Touch](community/getting-in-touch.md)
    3. [Contributing](community/contributing.md)
11. [Maintaining Loki](./maintaining/README.md)
    1. [Releasing Loki](./maintaining/release.md)
