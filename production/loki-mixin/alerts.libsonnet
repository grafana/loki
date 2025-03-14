{
  prometheusAlerts+:: {
    groups+: [
      {
        name: 'loki_alerts',
        rules: [
          {
            alert: 'LokiRequestErrors',
            expr: |||
              100 * sum(rate(loki_request_duration_seconds_count{status_code=~"5.."}[2m])) by (%(group_by_cluster)s, job, route)
                /
              sum(rate(loki_request_duration_seconds_count[2m])) by (%(group_by_cluster)s, job, route)
                > 10
            ||| % $._config,
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
              sum(increase(loki_panic_total[10m])) by (%(group_by_cluster)s, job) > 0
            ||| % $._config,
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
              %(group_prefix_jobs)s_route:loki_request_duration_seconds:99quantile{route!~"(?i).*tail.*|/schedulerpb.SchedulerForQuerier/QuerierLoop"} > 1
            ||| % $._config,
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
              sum(loki_boltdb_shipper_compactor_running) by (%(group_by_cluster)s) > 1
            ||| % $._config,
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
            // Alert if the compactor has not successfully run compaction in the last 3h since the last compaction.
            alert: 'LokiCompactorHasNotSuccessfullyRunCompaction',
            expr: |||
              # The "last successful run" metric is updated even if the compactor owns no tenants,
              # so this alert correctly doesn't fire if compactor has nothing to do.
              min (
                time() - (loki_boltdb_shipper_compact_tables_operation_last_successful_run_timestamp_seconds{} > 0)
              )
              by (%s, namespace)
              > 60 * 60 * 3
            ||| % $._config.per_cluster_label,
            'for': '1h',
            labels: {
              severity: 'critical',
            },
            annotations: {
              summary: 'Loki compaction has not run in the last 3 hours since the last compaction.',
              description: std.strReplace(|||
                {{ $labels.cluster }} {{ $labels.namespace }} has not run compaction in the last 3 hours since the last compaction. This may indicate a problem with the compactor.
              |||, 'cluster', $._config.per_cluster_label),
            },
          },
          {
            // Alert if the compactor has not successfully run compaction in the last 3h since startup.
            alert: 'LokiCompactorHasNotSuccessfullyRunCompaction',
            expr: |||
              # The "last successful run" metric is updated even if the compactor owns no tenants,
              # so this alert correctly doesn't fire if compactor has nothing to do.
              max(
                max_over_time(
                  loki_boltdb_shipper_compact_tables_operation_last_successful_run_timestamp_seconds{}[3h]
                )
              ) by (%s, namespace)
              == 0
            ||| % $._config.per_cluster_label,
            'for': '1h',
            labels: {
              severity: 'critical',
            },
            annotations: {
              summary: 'Loki compaction has not run in the last 3h since startup.',
              description: std.strReplace(|||
                {{ $labels.cluster }} {{ $labels.namespace }} has not run compaction in the last 3h since startup. This may indicate a problem with the compactor.
              |||, 'cluster', $._config.per_cluster_label),
            },
          },
        ],
      },
    ],
  },
}
