local utils = import 'mixin-utils/utils.libsonnet';

{
  prometheusRules+:: {
    groups+: [{
      name: 'loki_rules',
      rules:
        utils.histogramRules('loki_request_duration_seconds', ['job']) +
        utils.histogramRules('loki_request_duration_seconds', ['job', 'route']) +
        utils.histogramRules('loki_request_duration_seconds', ['namespace', 'job', 'route']),
    }],
  },
}
