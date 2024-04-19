(import 'dashboards.libsonnet') +
(import 'alerts.libsonnet') +
(import 'recording_rules.libsonnet') + {
  grafanaDashboardFolder: 'Loki Meta Monitoring',

  _config+:: {
    internal_components: false,

    // The Meta Monitoring helm chart uses Grafana Alloy instead of promtail
    promtail+: {
      enabled: false,
    },

    meta_monitoring+: {
      enabled: true,
    },
  },
}
