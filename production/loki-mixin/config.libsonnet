{
  local makePrefix(groups) = std.join('_', groups),
  local makeGroupBy(groups) = std.join(', ', groups),

  _config+:: {
    // Tags for dashboards.
    tags: ['loki'],

    // The label used to differentiate between different application instances (i.e. 'pod' in a kubernetes install).
    per_instance_label: 'pod',

    // The label used to differentiate between different nodes (i.e. servers).
    per_node_label: 'instance',

    // The label used to differentiate between different clusters.
    per_cluster_label: 'cluster',
    per_namespace_label: 'namespace',
    per_job_label: 'job',

    // Grouping labels, to uniquely identify and group by {jobs, clusters}
    job_labels: [$._config.per_cluster_label, $._config.per_namespace_label, $._config.per_job_label],
    cluster_labels: [$._config.per_cluster_label, $._config.per_namespace_label],

    // Each group prefix is composed of `_`-separated labels
    group_prefix_jobs: makePrefix($._config.job_labels),
    group_prefix_clusters: makePrefix($._config.cluster_labels),

    // Each group-by label list is `, `-separated and unique identifies
    group_by_job: makeGroupBy($._config.job_labels),
    group_by_cluster: makeGroupBy($._config.cluster_labels),

    // Enable dashboard and panels for Grafana Labs internal components.
    internal_components: false,

    blooms: {
      // Whether or not to include blooms specific dashboards
      enabled: true,
    },

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

    // Tunes histogram recording rules to aggregate over this interval.
    // Set to at least twice the scrape interval; otherwise, recording rules will output no data.
    // Set to four times the scrape interval to account for edge cases: https://www.robustperception.io/what-range-should-i-use-with-rate/
    recording_rules_range_interval: '1m',

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
