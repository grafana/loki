local utils = import 'mixin-utils/utils.libsonnet';

{
  prometheusRules+:: {
    groups+: [{
      name: 'loki_rules',
      rules:
        utils.histogramRules('loki_request_duration_seconds', [$._config.labels.cluster, 'job'], $._config.recording_rules_range_interval) +
        utils.histogramRules('loki_request_duration_seconds', [$._config.labels.cluster, 'job', 'route'], $._config.recording_rules_range_interval) +
        utils.histogramRules('loki_request_duration_seconds', [$._config.labels.cluster, 'namespace', 'job', 'route'], $._config.recording_rules_range_interval),
    }],
  },
}
