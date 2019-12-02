{
  prometheusAlerts+:: {
    groups+: [
      {
        name: 'loki_alerts',
        rules: [
          {
            alert: 'LokiRequestErrors',
            expr: |||
              100 * sum(rate(loki_request_duration_seconds_count{status_code=~"5.."}[1m])) by (namespace, job, route)
                /
              sum(rate(loki_request_duration_seconds_count[1m])) by (namespace, job, route)
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
            alert: 'LokiRequestLatency',
            expr: |||
              namespace_job_route:loki_request_duration_seconds:99quantile{route!~"(?i).*tail.*"} > 1
            |||,
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
        ],
      },
      {
        name: 'promtail_alerts',
        rules: [
          {
            alert: 'PromtailRequestsErrors',
            expr: |||
              100 * sum(rate(promtail_request_duration_seconds_count{status_code=~"5..|failed"}[1m])) by (namespace, job, route, instance)
                /
              sum(rate(promtail_request_duration_seconds_count[1m])) by (namespace, job, route, instance)
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
            alert: 'PromtailRequestLatency',
            expr: |||
              job_status_code:promtail_request_duration_seconds:99quantile > 1
            |||,
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
            alert: 'PromtailFileLagging',
            expr: |||
              abs(promtail_file_bytes_total - promtail_read_bytes_total) > 100000
            |||,
            'for': '15m',
            labels: {
              severity: 'critical',
            },
            annotations: {
              message: |||
                {{ $labels.instance }} {{ $labels.job }} {{ $labels.path }} has been lagging by more than 100kb for more than 15m.
              |||,
            },
          },
          {
            alert: 'PromtailFileMissing',
            expr: |||
              count by (path,instance,job) (promtail_file_bytes_total) unless count by (path,instance,job) (promtail_read_bytes_total)
            |||,
            'for': '15m',
            labels: {
              severity: 'critical',
            },
            annotations: {
              message: |||
                {{ $labels.instance }} {{ $labels.job }} {{ $labels.path }} matches the glob but is not being tailed.
              |||,
            },
          },
        ],
      },
    ],
  },
}
