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
              summary: 'Loki request error rate is high.',
              description: |||
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
              summary: 'Loki requests are causing code panics.',
              description: |||
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
              summary: 'Loki request error latency is high.',
              description: |||
                {{ $labels.job }} {{ $labels.route }} is experiencing {{ printf "%.2f" $value }}s 99th percentile latency.
              |||,
            },
          },
          {
            alert: 'LokiTooManyCompactorsRunning',
            expr: |||
              sum(loki_boltdb_shipper_compactor_running) by (namespace, %s) > 1
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
          {
            // Alert if the compactor has not successfully run compaction in the last 24h.
            alert: 'LokiCompactorHasNotSuccessfullyRunCompaction',
            expr: |||
              # The "last successful run" metric is updated even if the compactor owns no tenants,
              # so this alert correctly doesn't fire if compactor has nothing to do.
              (time() - loki_compactor_apply_retention_last_successful_run_timestamp_seconds > 60 * 60 * 24)
            |||,
            'for': '1h',
            labels: {
              severity: 'critical',
            },
            annotations: {
              summary: 'Loki compaction has not run in the last 24 hours.',
              description: std.strReplace(|||
                {{ $labels.cluster }} {{ $labels.namespace }} has not run compaction in the last 24 hours. This may indicate a problem with the compactor.
              |||, 'cluster', $._config.per_cluster_label),
            },
          },
        ],
      },
    ],
  },
}
