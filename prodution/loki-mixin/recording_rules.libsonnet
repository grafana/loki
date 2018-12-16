local histogramRules(metric, labels) =
  local vars = {
    metric: metric,
    labels_underscore: std.join('_', labels),
    labels_comma: std.join(', ', labels),
  };
  [
    {
      record: '%(labels_underscore)s:%(metric)s:99quantile' % vars,
      expr: 'histogram_quantile(0.99, sum(rate(%(metric)s_bucket[5m])) by (le, %(labels_comma)s))' % vars,
    },
    {
      record: '%(labels_underscore)s:%(metric)s:50quantile' % vars,
      expr: 'histogram_quantile(0.50, sum(rate(%(metric)s_bucket[5m])) by (le, %(labels_comma)s))' % vars,
    },
    {
      record: '%(labels_underscore)s:%(metric)s:avg' % vars,
      expr: 'sum(rate(%(metric)s_sum[5m])) by (%(labels_comma)s) / sum(rate(%(metric)s_count[5m])) by (%(labels_comma)s)' % vars,
    },
  ];

{
  prometheus_rules+:: {
    groups+: [{
      name: 'loki_rules',
      rules:
        histogramRules('loki_request_duration_seconds', ['job']) +
        histogramRules('loki_request_duration_seconds', ['job', 'route']) +
        histogramRules('loki_request_duration_seconds', ['namespace', 'job', 'route']),
    }, {
      name: 'loki_frontend_rules',
      rules:
        histogramRules('cortex_gw_request_duration_seconds', ['job']) +
        histogramRules('cortex_gw_request_duration_seconds', ['job', 'route']) +
        histogramRules('cortex_gw_request_duration_seconds', ['namespace', 'job', 'route']),
    }, {
      name: 'promtail_rules',
      rules:
        histogramRules('promtail_request_duration_seconds', ['job']) +
        histogramRules('promtail_request_duration_seconds', ['job', 'status_code']),
    }],
  },
}
