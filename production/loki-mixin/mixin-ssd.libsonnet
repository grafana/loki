(import 'dashboards.libsonnet') +
(import 'alerts.libsonnet') +
(import 'recording_rules.libsonnet') + {
  grafanaDashboardFolder: 'Loki SSD',

  _config+:: {
    ssd+: {
      enabled: true,
    },
  },
}
