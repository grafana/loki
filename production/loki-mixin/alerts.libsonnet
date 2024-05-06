{
  prometheusAlerts+:: {
    groups+: [
      {
        name: 'loki_alerts',
        rules: [
          {
            alert: 'LokiRequestErrors',
            expr: |||
              100 * sum(rate(loki_request_duration_seconds_count{status_code=~"5.."}[2m])) by (cluster, namespace, job, route)
                /
              sum(rate(loki_request_duration_seconds_count[2m])) by (cluster, namespace, job, route)
                > 10
            |||,
            'for': '15m',
            labels: {
              severity: 'critical',
            },
            annotations: {
              summary: 'Loki request error rate is high.',
              description: std.strReplace(|||
                {{ $labels.cluster }} {{ $labels.job }} {{ $labels.route }} is experiencing {{ printf "%.2f" $value }}% errors.
              |||, 'cluster', $._config.per_cluster_label),
            },
          },
          {
            alert: 'LokiRequestPanics',
            expr: |||
              sum(increase(loki_panic_total[10m])) by (cluster, namespace, job) > 0
            |||,
            labels: {
              severity: 'critical',
            },
            annotations: {
              summary: 'Loki requests are causing code panics.',
              description: std.strReplace(|||
                {{ $labels.cluster }} {{ $labels.job }} is experiencing {{ printf "%.2f" $value }}% increase of panics.
              |||, 'cluster', $._config.per_cluster_label),
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
              summary: 'Loki request error latency is high.',
              description: std.strReplace(|||
                {{ $labels.cluster }} {{ $labels.job }} {{ $labels.route }} is experiencing {{ printf "%.2f" $value }}s 99th percentile latency.
              |||, 'cluster', $._config.per_cluster_label),
            },
          },
          {
            alert: 'LokiTooManyCompactorsRunning',
            expr: |||
              sum(loki_boltdb_shipper_compactor_running) by (%s, namespace) > 1
            ||| % $._config.per_cluster_label,
            'for': '5m',
            labels: {
              severity: 'warning',
            },
            annotations: {
              summary: 'Loki deployment is running more than one compactor.',
              description: std.strReplace(|||
                {{ $labels.cluster }} {{ $labels.namespace }} has had {{ printf "%.0f" $value }} compactors running for more than 5m. Only one compactor should run at a time.
              |||, 'cluster', $._config.per_cluster_label),
            },
          },
        ],
      },
    ],
  },
}
