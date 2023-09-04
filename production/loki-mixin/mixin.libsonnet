(import 'dashboards.libsonnet') +
(import 'alerts.libsonnet') +
(import 'recording_rules.libsonnet') + {
  grafanaDashboardFolder: 'Loki',

  _config+:: {
    show_tsdb_graphs+: {
      enabled: true,
    }
  }
}
