{
  prometheusAlerts+:: {
    groups+: [
      {
        name: 'tempo_alerts',
        rules: [
          {
            alert: 'TempoRequestErrors',
            expr: |||
              100 * sum(rate(tempo_request_duration_seconds_count{status_code=~"5.."}[1m])) by (namespace, job, route)
                /
              sum(rate(tempo_request_duration_seconds_count[1m])) by (namespace, job, route)
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
            alert: 'TempoRequestLatency',
            expr: |||
              namespace_job_route:tempo_request_duration_seconds:99quantile > 1
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
        name: 'tempo_frontend_alerts',
        rules: [
          {
            alert: 'FrontendRequestErrors',
            expr: |||
              100 * sum(rate(cortex_gw_request_duration_seconds_count{status_code=~"5.."}[1m])) by (namespace, job, route)
                /
              sum(rate(cortex_gw_request_duration_seconds_count[1m])) by (namespace, job, route)
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
            alert: 'FrontendRequestLatency',
            expr: |||
              namespace_job_route:cortex_gw_request_duration_seconds:99quantile > 1
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
        ],
      },
    ],
  },
}
