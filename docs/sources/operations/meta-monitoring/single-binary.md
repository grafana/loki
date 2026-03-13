---
title: Single binary meta-monitoring
menuTitle: Single Binary Meta Monitoring
description: Describes how to deploy Meta Monitoring for single binary
weight: 100
---

# Single binary meta-monitoring

Meta monitoring for [single binary](https://grafana.com/docs/loki/latest/setup/install/local/) deployments involves some additional configuration. This approach does not use the Kubernetes Monitoring Helm chart.
  
## Prometheus

Configure Prometheus to scrape the Loki metrics endpoint, adding the additional labels that are expected by the [mixin](https://grafana.com/docs/loki/latest/operations/meta-monitoring/mixins) dashboards, alerts, and recording rules:

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

To generate the [mixins](https://grafana.com/docs/loki/latest/operations/meta-monitoring/mixins):

1. Install [jb](https://github.com/jsonnet-bundler/jsonnet-bundler)
2. Install [mixtool](https://github.com/monitoring-mixins/mixtool)
3. Clone the Loki repository from Github

   ```bash
   git clone https://github.com/grafana/loki
   cd loki
   ```

4. Create the output directory

    ```bash
    mkdir loki/production/loki-mixin-compiled-single-binary
    ```

5. Navigate to the mixin directory

   ```bash
   cd loki/production/loki-mixin
   ```

6. Use `jb` to install mixin dependencies and generate the `vender` directory

   ```bash
   jb install
   ```

7. Create the mixin configuration file `single-binary.libsonnet`

    ```bash
    cat <<EOF > single-binary.libsonnet
    local loki = import 'mixin.libsonnet';
    
    loki + {
      _config+:: {
        meta_monitoring: {
          enabled: true,
        },
        canary+: {
          // Whether or not to include the loki-canary dashboard
          enabled: true
        },
        promtail: {
          // Whether or not to include promtail specific dashboards
          enabled: false,
        },
        // Tunes histogram recording rules to aggregate over this interval.
        // Set to at least twice the scrape interval; otherwise, recording rules will output no data.
        // Set to four times the scrape interval to account for edge cases: https://www.robustperception.io/what-range-should-i-use-with-rate/        
        recording_rules_range_interval: '5m'
      }  
    }
    EOF
    ```

8. Generate dashboards, alerts, and recording rules

    ```bash
    mixtool generate all \
      --output-alerts ../loki-mixin-compiled-single/alerts.yaml \
      --output-rules ../loki-mixin-compiled-single/rules.yaml \
      --directory ../loki-mixin-compiled-single/dashboards \
      single-binary.libsonnet
     ```

9. See the generated dashboards, alerts, and recording rules in the `loki/production/loki-mixin-compiled-single-binary` directory
10. Follow the instructions in the [Install Mixins](https://grafana.com/docs/loki/latest/operations/meta-monitoring/mixins) documentation to set up the dashboards, alerts, and recordings
