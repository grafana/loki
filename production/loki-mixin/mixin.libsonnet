{
  grafanaDashboards: {
    'overview.json': (import 'dashboards/overview.libsonnet'),
    'tenant-overrides.json': (import 'dashboards/tenant-overrides.libsonnet'),
  },

  prometheusRules: [],

  prometheusAlerts: [],
}
