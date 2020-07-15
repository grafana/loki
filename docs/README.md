# Loki Documentation

<p align="center"> <img src="logo_and_name.png" alt="Loki Logo"> <br>
  <small>Like Prometheus, but for logs!</small> </p>

Grafana Loki is a set of components that can be composed into a fully featured
logging stack.

Unlike other logging systems, Loki is built around the idea of only indexing
metadata about your logs: labels (just like Prometheus labels). Log data itself
is then compressed and stored in chunks in object stores such as S3 or GCS, or
even locally on the filesystem. A small index and highly compressed chunks
simplifies the operation and significantly lowers the cost of Loki.

## Table of Contents

1. [Overview](overview/README.md)
    1. [Comparison to other Log Systems](overview/comparisons.md)
2. [Installation](installation/README.md)
    1. [Installing with Tanka](installation/tanka.md)
    2. [Installing with Helm](installation/helm.md)
    3. [Installing with Docker](installation/docker.md)
    4. [Installing locally](installation/local.md)
3. [Getting Started](getting-started/README.md)
    1. [Grafana](getting-started/grafana.md)
    2. [LogCLI](getting-started/logcli.md)
    3. [Labels](getting-started/labels.md)
    4. [Troubleshooting](getting-started/troubleshooting.md)
4. [Best Practices](best-practices/README.md)
    1. [Current Best Practices](best-practices/current-best-practices.md)
5. [Configuration](configuration/README.md)
    1. [Examples](configuration/examples.md)
6. [Clients](clients/README.md)
    1. [Promtail](clients/promtail/README.md)
        1. [Installation](clients/promtail/installation.md)
        2. [Configuration](clients/promtail/configuration.md)
        3. [Scraping](clients/promtail/scraping.md)
        4. [Pipelines](clients/promtail/pipelines.md)
        5. [Troubleshooting](clients/promtail/troubleshooting.md)
    2. [Docker Driver](clients/docker-driver/README.md)
        1. [Configuration](clients/docker-driver/configuration.md)
    4. [Fluent Bit](../cmd/fluent-bit/README.md)
    3. [Fluentd](clients/fluentd/README.md)
7. [LogQL](logql.md)
8. [Operations](operations/README.md)
    1. [Authentication](operations/authentication.md)
    2. [Observability](operations/observability.md)
    3. [Scalability](operations/scalability.md)
    4. [Storage](storage.md)
        1. [Operations](operations/storage/README.md)
            1. [Table Manager](operations/storage/table-manager.md)
            2. [Retention](operations/storage/retention.md)
            3. [BoltDB Shipper](operations/storage/boltdb-shipper.md)
    5. [Multi-tenancy](operations/multi-tenancy.md)
    6. [Loki Canary](operations/loki-canary.md)
9. [HTTP API](api.md)
10. [Architecture](architecture.md)
11. [Community](community/README.md)
    1. [Governance](community/governance.md)
    2. [Getting in Touch](community/getting-in-touch.md)
    3. [Contributing to Loki](community/contributing.md)
12. [Loki Maintainers Guide](./maintaining/README.md)
    1. [Releasing Loki](./maintaining/release.md)
