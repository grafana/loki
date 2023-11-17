{
  _config+:: {
    // Tags for dashboards.
    tags: ['loki'],

    // The label used to differentiate between different clusters.
    per_cluster_label: 'cluster',

    // Dashboard name.
    dashboard_name: 'Loki / Promtail',

    // The label selector used to differentiate between different clusters and namespaces.
    dashboard_labels_selector: $._config.per_cluster_label + '=~"$cluster", namespace=~"$namespace"',

    // The label selector used for Latency quantile querys.
    dashboard_quantile_label_selector: $._config.per_cluster_label + '=~"$cluster", job=~"$namespace/promtail.*"',
  },
}
