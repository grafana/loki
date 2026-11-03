---
title: Single binary meta-monitoring
menuTitle: Single Binary Meta Monitoring
description: Describes how to deploy Meta Monitoring for single binary
weight: 400
---

# Single binary meta-monitoring

Meta monitoring for [monolithic mode](https://grafana.com/docs/loki/<LOKI_VERSION>/get-started/deployment-modes/#monolithic-mode) deployments involves some additional configuration. This approach does not use the Kubernetes Monitoring Helm chart.

## Metrics

Configure Alloy or Prometheus to scrape the Loki metrics endpoint, adding the additional labels that are expected by the [mixin](https://grafana.com/docs/loki/<LOKI_VERSION>/operations/meta-monitoring/mixins) dashboards, alerts, and recording rules:

### Alloy

```alloy
prometheus.scrape "loki" {
  targets = [{
    __address__ = "localhost:3100",
    cluster     = "prod",
    namespace   = "default",
    job         = "default/loki-single-binary",
    pod         = "loki-single-binary",
    container   = "loki",
  }]
  forward_to = [prometheus.remote_write.default.receiver]
}

prometheus.remote_write "default" {
  endpoint {
    url = "<PROMETHEUS_REMOTE_WRITE_URL>"
  }
}
```

### Prometheus

```yaml
scrape_configs:
  - job_name: loki
    static_configs:
      - targets: ['localhost:3100']
        labels:
          cluster: prod
          namespace: default
          job: default/loki-single-binary
          pod: loki-single-binary
          container: loki
```

## Mixins

To generate the [mixins](https://grafana.com/docs/loki/<LOKI_VERSION>/operations/meta-monitoring/mixins):

1. Install [jb](https://github.com/jsonnet-bundler/jsonnet-bundler).
1. Install [mixtool](https://github.com/monitoring-mixins/mixtool).
1. Clone the Loki repository from Github.

    ```bash
    git clone https://github.com/grafana/loki
    cd loki
    ```

1. Check out the tag that matches the version of Loki you are running, so that the dashboards, alerts, and recording rules match the metrics your Loki cluster exposes. For example, if you are running Loki 3.7.6:

    ```bash
    git checkout v3.7.6
    ```

1. Create the output directory.

    ```bash
    mkdir production/loki-mixin-compiled-single-binary
    ```

1. Navigate to the mixin directory.

    ```bash
    cd production/loki-mixin
    ```

1. Use `jb` to install mixin dependencies and generate the `vendor` directory.

    ```bash
    jb install
    ```

1. Create the mixin configuration file `single-binary.libsonnet`.

    ```bash
    cat <<EOF > single-binary.libsonnet
    local loki = import 'mixin.libsonnet';

    loki + {
      _config+:: {
        meta_monitoring+: {
          enabled: true,
        },
        canary+: {
          // Whether or not to include the loki-canary dashboard.
          // Set to false if you don't run loki-canary.
          enabled: true,
        },
        promtail+: {
          // Whether or not to include promtail specific dashboards
          enabled: false,
        },
        // Tunes histogram recording rules to aggregate over this interval.
        // Set to at least twice the scrape interval; otherwise, recording rules will output no data.
        // Set to four times the scrape interval to account for edge cases: https://www.robustperception.io/what-range-should-i-use-with-rate/
        recording_rules_range_interval: '5m',
      },
    }
    EOF
    ```

1. Generate dashboards, alerts, and recording rules.

    ```bash
    mixtool generate all \
      --output-alerts ../loki-mixin-compiled-single-binary/alerts.yaml \
      --output-rules ../loki-mixin-compiled-single-binary/rules.yaml \
      --directory ../loki-mixin-compiled-single-binary/dashboards \
      single-binary.libsonnet
    ```

1. See the generated dashboards, alerts, and recording rules in the `production/loki-mixin-compiled-single-binary` directory.
1. Follow the instructions in the [Install Mixins](https://grafana.com/docs/loki/<LOKI_VERSION>/operations/meta-monitoring/mixins) documentation to set up the dashboards, alerts, and recordings.
