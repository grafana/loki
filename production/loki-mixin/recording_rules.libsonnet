local utils = import 'mixin-utils/utils.libsonnet';

{
  prometheusRules+:: {
    groups+: [{
      name: 'loki_api_1',
      rules:
        utils.histogramRules('loki_request_duration_seconds', [$._config.per_cluster_label, 'job']),
    }, {
      name: 'loki_api_2',
      rules:
        utils.histogramRules('loki_request_duration_seconds', [$._config.per_cluster_label, 'job', 'route']),
    }, {
      name: 'loki_api_3',
      rules:
        utils.histogramRules('loki_request_duration_seconds', [$._config.per_cluster_label, 'namespace', 'job', 'route']),
    }],
  },
}
