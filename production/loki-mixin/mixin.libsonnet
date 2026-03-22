(import 'dashboards.libsonnet') +
(import 'alerts.libsonnet') +
(import 'config.libsonnet') +
(import 'recording_rules.libsonnet') + {
  grafanaDashboardFolder: 'Loki',
  // Without this, configs is not taken into account
  _config+:: {},
}
