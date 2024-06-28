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

    promtail: {
      // Whether or not to include promtail specific dashboards
      enabled: true,
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
