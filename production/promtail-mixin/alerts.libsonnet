{
  prometheusAlerts+:: {
    groups+: [
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
              job_status_code_namespace:promtail_request_duration_seconds:99quantile > 1
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
              promtail_stream_lag_seconds > 10
            |||,
            'for': '5m',
            labels: {
              severity: 'warning',
            },
            annotations: {
              message: |||
                {{ $labels.instance }} {{ $labels.job }} {{ $labels.path }} has been lagging by more than 10 seconds for more than 15m.
              |||,
            },
          },
          {
            alert: 'PromtailFileMissing',
            expr: |||
              promtail_file_bytes_total unless promtail_read_bytes_total
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
