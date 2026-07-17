{
  local makePrefix(groups) = std.join('_', groups),
  local makeGroupBy(groups) = std.join(', ', groups),

  _config+:: {
    // Tags for dashboards.
    tags: ['loki'],

    // The label used to differentiate between different Loki components
    per_component_label: 'container',

    // The label used to differentiate between different application instances (i.e. 'pod' in a kubernetes install).
    per_instance_label: 'pod',

    // The label used to differentiate between different nodes (i.e. servers).
    per_node_label: 'instance',

    // The label used to differentiate between different clusters.
    per_cluster_label: 'cluster',
    per_namespace_label: 'namespace',
    per_job_label: 'job',

    // The Log Formater that is used within the Dashboards (logfmt,json)
    log_format: 'logfmt',

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

    thanos: {
      // Whether or not to include thanos specific dashboards
      enabled: true,
    },

    operational: {
      // Whether or not to include memcached in the operational dashboard
      memcached: true,
      // Whether or not to include consul in the operational dashboard
      consul: true,
      // Whether or not to include gcs in the operational dashboard
      gcs: true,
      // Whether or not to include s3 in the operational dashboard
      s3: true,
      // Whether or not to include azure blob in the operational dashboard
      azureBlob: true,
    },

    // Enable TSDB specific dashboards
    tsdb: true,

    // Tunes histogram recording rules to aggregate over this interval.
    // Set to at least twice the scrape interval; otherwise, recording rules will output no data.
    // Set to four times the scrape interval to account for edge cases: https://www.robustperception.io/what-range-should-i-use-with-rate/
    recording_rules_range_interval: '1m',

    // Meta-monitoring related configuration
    meta_monitoring: {
      enabled: false,
    },

    // Enable panels that depend on autoscaler metrics
    // (loki_autoscaler_min_replicas / loki_autoscaler_max_replicas).
    autoscaling_metrics: false,
    // Per-component autoscaling status.
    // Only consulted when autoscaling_metrics is true.
    // If true, min and max replicas for the component are shown.
    // If false, a panel that says "not autoscaled" is shown instead.
    autoscaled: {
      gateway: false,  // only when internal_components=true
      query_frontend: false,
      query_scheduler: false,
      querier: false,
      index_gateway: false,
      bloom_gateway: false,
      ingester: false,
      ruler: false,
    },
  },
}
