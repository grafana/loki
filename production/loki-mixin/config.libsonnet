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

    // Enable dashboard and panels for Grafana Labs internal components.
    internal_components: false,

    promtail: {
      // Whether or not to include promtail specific dashboards
      enabled: true,
    },

    operational: {
      // Whether or not to include memcached in the operational dashboard
      memcached: true,
      // Whether or not to include consul in the operational dashboard
      consul: true,
      // Whether or not to include big table in the operational dashboard
      bigTable: true,
      // Whether or not to include dynamo in the operational dashboard
      dynamo: true,
      // Whether or not to include gcs in the operational dashboard
      gcs: true,
      // Whether or not to include s3 in the operational dashboard
      s3: true,
      // Whether or not to include azure blob in the operational dashboard
      azureBlob: true,
      // Whether or not to include bolt db in the operational dashboard
      boltDB: true,
    },

    // Enable TSDB specific dashboards
    tsdb: true,

    // SSD related configuration for dashboards.
    ssd: {
      // Support Loki SSD mode on dashboards.
      enabled: false,

      // The prefix used to match the write and read pods on SSD mode.
      pod_prefix_matcher: '(loki.*|enterprise-logs)',
    },

    // Meta-monitoring related configuration
    meta_monitoring: {
      enabled: false,
    },
  },
}
