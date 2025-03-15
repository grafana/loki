(import 'mixin.libsonnet') + {
  grafanaDashboardFolder: 'Loki Single Binary',

  _config+:: {
    internal_components: false,

    // By default the helm chart uses the Grafana Agent instead of promtail
    promtail+: {
      enabled: false,
    },

    sb+: {
      enabled: true,
    },
  },
}
