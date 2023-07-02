(import 'dashboards.libsonnet') +
(import 'alerts.libsonnet') +
(import 'recording_rules.libsonnet') + {
  grafanaDashboardFolder: 'Loki SSD',

  _config+:: {
    internal_components: false,

    // By default the helm chart uses the Grafana Agent instead of promtail
    promtail+: {
      enabled: false,
    },

    ssd+: {
      enabled: true,
    },
  },
}
