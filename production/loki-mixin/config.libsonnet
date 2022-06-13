{
  _config+:: {
    // Tags for dashboards.
    tags: ['loki'],

    // The label used to differentiate between different application instances (i.e. 'pod' in a kubernetes install).
    per_instance_label: 'pod',

    // The label used to differentiate between different nodes (i.e. servers).
    per_node_label: 'instance',
  },
}
