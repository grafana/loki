{
  _config+:: {
    // Tags for dashboards.
    tags: ['loki'],

    // The label used to differentiate between different application instances (i.e. 'pod' in a kubernetes install).
    per_instance_label: 'pod',

    // The label used to differentiate between different nodes (i.e. servers).
    per_node_label: 'instance',

    // The label used to differentiate between different clusters.
    per_cluster_label: 'cluster',

    // Support Loki SSD mode on dashboards.
    ssd: false,

    // Enable dashboard and panels for Grafana Labs internal components.
    internal_components: false,
  },
}
