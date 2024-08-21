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
