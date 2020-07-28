# Loki Documentation

<p align="center"> <img src="sources/logo_and_name.png" alt="Loki Logo"> <br>
  <small>Like Prometheus, but for logs!</small> </p>

Grafana Loki is a set of components that can be composed into a fully featured
logging stack.

Unlike other logging systems, Loki is built around the idea of only indexing
metadata about your logs: labels (just like Prometheus labels). Log data itself
is then compressed and stored in chunks in object stores such as S3 or GCS, or
even locally on the filesystem. A small index and highly compressed chunks
simplifies the operation and significantly lowers the cost of Loki.

## Table of Contents

1. [Overview](sources/overview/_index.md)
    1. [Comparison to other Log Systems](sources/overview/comparisons.md)
2. [Installation](sources/installation/_index.md)
    1. [Installing with Tanka](sources/installation/tanka.md)
    2. [Installing with Helm](sources/installation/helm.md)
    3. [Installing with Docker](sources/installation/docker.md)
    4. [Installing locally](sources/installation/local.md)
3. [Getting Started](sources/getting-started/_index.md)
    1. [Grafana](sources/getting-started/grafana.md)
    2. [LogCLI](sources/getting-started/logcli.md)
    3. [Labels](sources/getting-started/labels.md)
    4. [Troubleshooting](sources/getting-started/troubleshooting.md)
4. [Best Practices](sources/best-practices/_index.md)
    1. [Current Best Practices](sources/best-practices/current-best-practices.md)
5. [Configuration](sources/configuration/_index.md)
    1. [Examples](sources/configuration/examples.md)
6. [Clients](sources/clients/_index.md)
    1. [Promtail](sources/clients/promtail/_index.md)
        1. [Installation](sources/clients/promtail/installation.md)
        2. [Configuration](sources/clients/promtail/configuration.md)
        3. [Scraping](sources/clients/promtail/scraping.md)
        4. [Pipelines](sources/clients/promtail/pipelines.md)
        5. [Troubleshooting](sources/clients/promtail/troubleshooting.md)
    2. [Docker Driver](sources/clients/docker-driver/_index.md)
        1. [Configuration](sources/clients/docker-driver/configuration.md)
    4. [Fluent Bit](sources/../cmd/fluent-bit/_index.md)
    3. [Fluentd](sources/clients/fluentd/_index.md)
7. [LogQL](sources/logql/_index.md)
8. [Operations](sources/operations/_index.md)
    1. [Authentication](sources/operations/authentication.md)
    2. [Observability](sources/operations/observability.md)
    3. [Scalability](sources/operations/scalability.md)
    4. [Storage](sources/operations/storage/_index.md)
        1. [Table Manager](sources/operations/storage/table-manager.md)
        2. [Retention](sources/operations/storage/retention.md)
        3. [BoltDB Shipper](sources/operations/storage/boltdb-shipper.md)
    5. [Multi-tenancy](sources/operations/multi-tenancy.md)
    6. [Loki Canary](sources/operations/loki-canary.md)
9. [HTTP API](sources/api/_index.md)
10. [Architecture](sources/architecture/_index.md)
11. [Community](sources/community/_index.md)
    1. [Governance](sources/community/governance.md)
    2. [Getting in Touch](sources/community/getting-in-touch.md)
    3. [Contributing to Loki](sources/community/contributing.md)
12. [Loki Maintainers Guide](sources/./maintaining/_index.md)
    1. [Releasing Loki](sources/./maintaining/release.md)

## Updating docs

### Building the docs locally

When you contribute to documentation, it is a good practice to build the docs on your local machine to make sure your changes appear as you expect. This README explains the process for doing that.

#### Requirements

Docker >= 2.1.0.3

### Build the doc site

1. In the command line, make sure you are in the docs folder: `cd docs`.
2. Run `make docs`. This launches a preview of the docs website at `http://localhost:3002/docs/loki/latest/` which will refresh automatically when changes to content in the `sources` directory are made.

---

### Content guidelines

Edit content in the `sources` directory.

### Edit the side menu

The side menu is built automatically from the folder structure. Ordering is done with the `weight` front matter param.

### Add images

Images are sourced in this repo alonside content. They will sync to the website repo just like markdown files.

---

## Deploy changes to grafana.com

When a PR is merged to master with changes in the `docs/sources` directory, those changes are automatically synched to the grafana/website repo and published to the staging site.

Generally, someone from marketing will publish to production each day, so as long as the sync is successful your docs edits will be published. Alternatively, you can refer to [publishing to production](https://github.com/grafana/website#publishing-to-production-grafanacom) if you'd like to do it yourself.
