---
title: Usage statistics
description: Describes the anonymous usage statistics that Loki collects and how to disable reporting.
weight: 490
---
# Usage statistics

Loki sends anonymous usage statistics to Grafana Labs. This reporting is enabled by default and helps the Loki team understand aggregate usage patterns. Reports are sent to `https://stats.grafana.org/loki-usage-report` by default.

## What Loki collects

Loki reports aggregate operational metadata, including:

- An anonymous cluster identifier.
- Build and runtime metadata, such as version, OS, architecture, and Go version.
- Deployment metadata, such as the configured Loki target.
- Go runtime metrics, such as memory and goroutine counts.
- Aggregate product metrics, such as ingestion, query throughput, and chunk flush statistics.
- Storage and compactor configuration metadata, such as schema, storage backend type, and retention settings.

Loki does not report log payloads, label values, tenant identifiers, or query strings. You can view the code in [stats.go](https://github.com/grafana/loki/blob/main/pkg/analytics/stats.go)

## Disable usage statistics reporting

To disable reporting in the Loki config file, set `analytics.reporting_enabled` to `false`:

```yaml
analytics:
  reporting_enabled: false
```

To disable reporting with a CLI flag, start Loki with:

```sh
-reporting.enabled=false
```

To disable reporting with the Helm chart, set the following values:

```yaml
loki:
  analytics:
    reporting_enabled: false
```

For all analytics configuration options, refer to the [analytics configuration reference](../#analytics).
