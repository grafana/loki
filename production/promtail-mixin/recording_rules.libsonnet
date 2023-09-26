local utils = import 'mixin-utils/utils.libsonnet';

{
  prometheusRules+:: {
    groups+: [{
      name: 'promtail_rules',
      rules:
        utils.histogramRules('promtail_request_duration_seconds', ['job']) +
        utils.histogramRules('promtail_request_duration_seconds', ['job', 'namespace']) +
        utils.histogramRules('promtail_request_duration_seconds', ['job', 'status_code', 'namespace']),
    }],
  },
}
