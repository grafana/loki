local utils = import 'mixin-utils/utils.libsonnet';

{
  prometheusRules+:: {
    groups+: [{
      name: 'loki_rules',
      rules:
        utils.histogramRules('loki_request_duration_seconds', [$._config.per_cluster_label, 'job']) +
        utils.histogramRules('loki_request_duration_seconds', [$._config.per_cluster_label, 'job', 'route']) +
        utils.histogramRules('loki_request_duration_seconds', [$._config.per_cluster_label, 'namespace', 'job', 'route']),
    }],
  },
}
