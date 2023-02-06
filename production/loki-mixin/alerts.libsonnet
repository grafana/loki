{
  prometheusAlerts+:: {
    groups+: [
      {
        name: 'loki_alerts',
        rules: [
          {
            alert: 'LokiRequestErrors',
            expr: |||
              100 * sum(rate(loki_request_duration_seconds_count{status_code=~"5.."}[2m])) by (namespace, job, route)
                /
              sum(rate(loki_request_duration_seconds_count[2m])) by (namespace, job, route)
                > 10
            |||,
            'for': '15m',
            labels: {
              severity: 'critical',
            },
            annotations: {
              message: |||
                {{ $labels.job }} {{ $labels.route }} is experiencing {{ printf "%.2f" $value }}% errors.
              |||,
            },
          },
          {
            alert: 'LokiRequestPanics',
            expr: |||
              sum(increase(loki_panic_total[10m])) by (namespace, job) > 0
            |||,
            labels: {
              severity: 'critical',
            },
            annotations: {
              message: |||
                {{ $labels.job }} is experiencing {{ printf "%.2f" $value }}% increase of panics.
              |||,
            },
          },
          {
            alert: 'LokiRequestLatency',
            expr: |||
              %s_namespace_job_route:loki_request_duration_seconds:99quantile{route!~"(?i).*tail.*|/schedulerpb.SchedulerForQuerier/QuerierLoop"} > 1
            ||| % $._config.per_cluster_label,
            'for': '15m',
            labels: {
              severity: 'critical',
            },
            annotations: {
              message: |||
                {{ $labels.job }} {{ $labels.route }} is experiencing {{ printf "%.2f" $value }}s 99th percentile latency.
              |||,
            },
          },
          {
            alert: 'LokiTooManyCompactorsRunning',
            expr: |||
              sum(loki_boltdb_shipper_compactor_running) by (namespace, cluster) > 1
            |||,
            'for': '5m',
            labels: {
              severity: 'warning',
            },
            annotations: {
              message: |||
                {{ $labels.cluster }} {{ $labels.namespace }} has had {{ printf "%.0f" $value }} compactors running for more than 5m. Only one compactor should run at a time.
              |||,
            },
          },
        ],
      },
    ],
  },
}
