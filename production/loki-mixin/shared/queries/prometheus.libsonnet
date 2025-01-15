// imports
local config = import '../../config.libsonnet';
local variables = import '../variables.libsonnet';
local selector = (import '../selectors.libsonnet').new;
local lib = import '../../lib/_imports.libsonnet';

{
  _resourceSelector(component)::
    selector()
      .resource(component),

  // Gets the rate of attempted dialer connections
  net_conntrack_dialer_conn_attempted_rate(component, by='')::
    |||
      sum by (%s) (
        rate(net_conntrack_dialer_conn_attempted_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of closed dialer connections
  net_conntrack_dialer_conn_closed_rate(component, by='')::
    |||
      sum by (%s) (
        rate(net_conntrack_dialer_conn_closed_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of established dialer connections
  net_conntrack_dialer_conn_established_rate(component, by='')::
    |||
      sum by (%s) (
        rate(net_conntrack_dialer_conn_established_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of failed dialer connections
  net_conntrack_dialer_conn_failed_rate(component, by='')::
    |||
      sum by (%s) (
        rate(net_conntrack_dialer_conn_failed_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the dialer connection success ratio
  net_conntrack_dialer_conn_success_ratio(component, by='')::
    |||
      sum by (%s) (rate(net_conntrack_dialer_conn_established_total{%s}[$__rate_interval]))
      /
      sum by (%s) (rate(net_conntrack_dialer_conn_attempted_total{%s}[$__rate_interval]))
    ||| % std.repeat([by, self._resourceSelector(component).build()], 2),

  // Gets the rate of exemplars received by remote storage
  remote_storage_exemplars_in_rate(component, by='')::
    |||
      sum by (%s) (
        rate(prometheus_remote_storage_exemplars_in_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of histograms received by remote storage
  remote_storage_histograms_in_rate(component, by='')::
    |||
      sum by (%s) (
        rate(prometheus_remote_storage_histograms_in_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of samples received by remote storage
  remote_storage_samples_in_rate(component, by='')::
    |||
      sum by (%s) (
        rate(prometheus_remote_storage_samples_in_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of string interner zero reference releases
  remote_storage_string_interner_zero_reference_releases_rate(component, by='')::
    |||
      sum by (%s) (
        rate(prometheus_remote_storage_string_interner_zero_reference_releases_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of DNS lookup failures in service discovery
  sd_dns_lookup_failures_rate(component, by='')::
    |||
      sum by (%s) (
        rate(prometheus_sd_dns_lookup_failures_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of DNS lookups in service discovery
  sd_dns_lookups_rate(component, by='')::
    |||
      sum by (%s) (
        rate(prometheus_sd_dns_lookups_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the duration of service discovery refresh operations
  sd_refresh_duration_seconds(component, by='')::
    |||
      sum by (%s) (
        prometheus_sd_refresh_duration_seconds{%s}
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of service discovery refresh failures
  sd_refresh_failures_rate(component, by='')::
    |||
      sum by (%s) (
        rate(prometheus_sd_refresh_failures_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of template text expansion failures
  template_text_expansion_failures_rate(component, by='')::
    |||
      sum by (%s) (
        rate(prometheus_template_text_expansion_failures_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of template text expansions
  template_text_expansions_rate(component, by='')::
    |||
      sum by (%s) (
        rate(prometheus_template_text_expansions_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of WAL completed pages
  tsdb_wal_completed_pages_rate(component, by='')::
    |||
      sum by (%s) (
        rate(prometheus_tsdb_wal_completed_pages_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the percentile of WAL fsync duration
  tsdb_wal_fsync_duration_percentile(component, percentile, by='')::
    |||
      histogram_quantile(
        %g,
        sum by (le, %s) (
          rate(prometheus_tsdb_wal_fsync_duration_seconds_bucket{%s}[$__rate_interval])
        )
      )
    ||| % [
      percentile,
      by,
      self._resourceSelector(component).build(),
    ],

  // Gets the 99th percentile of WAL fsync duration
  tsdb_wal_fsync_duration_p99(component, by=''):: self.tsdb_wal_fsync_duration_percentile(component, 0.99, by),
  // Gets the 95th percentile of WAL fsync duration
  tsdb_wal_fsync_duration_p95(component, by=''):: self.tsdb_wal_fsync_duration_percentile(component, 0.95, by),
  // Gets the 90th percentile of WAL fsync duration
  tsdb_wal_fsync_duration_p90(component, by=''):: self.tsdb_wal_fsync_duration_percentile(component, 0.90, by),
  // Gets the 50th percentile of WAL fsync duration
  tsdb_wal_fsync_duration_p50(component, by=''):: self.tsdb_wal_fsync_duration_percentile(component, 0.50, by),

  // Gets the average WAL fsync duration
  tsdb_wal_fsync_duration_average(component, by='')::
    |||
      sum by (%s) (
        rate(prometheus_tsdb_wal_fsync_duration_seconds_sum{%s}[$__rate_interval])
      )
      /
      sum by (%s) (
        rate(prometheus_tsdb_wal_fsync_duration_seconds_count{%s}[$__rate_interval])
      )
    ||| % std.repeat([by, self._resourceSelector(component).build()], 2),

  // Gets the histogram of WAL fsync duration
  tsdb_wal_fsync_duration_histogram(component)::
    |||
      sum by (le) (
        rate(prometheus_tsdb_wal_fsync_duration_seconds_bucket{%s}[$__rate_interval])
      )
    ||| % [self._resourceSelector(component).build()],

  // Gets the rate of WAL page flushes
  tsdb_wal_page_flushes_rate(component, by='')::
    |||
      sum by (%s) (
        rate(prometheus_tsdb_wal_page_flushes_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the current WAL segment
  tsdb_wal_segment_current(component, by='')::
    |||
      sum by (%s) (
        prometheus_tsdb_wal_segment_current{%s}
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the WAL storage size in bytes
  tsdb_wal_storage_size_bytes(component, by='')::
    |||
      sum by (%s) (
        prometheus_tsdb_wal_storage_size_bytes{%s}
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of failed WAL truncations
  tsdb_wal_truncations_failed_rate(component, by='')::
    |||
      sum by (%s) (
        rate(prometheus_tsdb_wal_truncations_failed_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of WAL truncations
  tsdb_wal_truncations_rate(component, by='')::
    |||
      sum by (%s) (
        rate(prometheus_tsdb_wal_truncations_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of failed WAL writes
  tsdb_wal_writes_failed_rate(component, by='')::
    |||
      sum by (%s) (
        rate(prometheus_tsdb_wal_writes_failed_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the number of in-flight HTTP metric handler requests
  http_metric_handler_requests_in_flight(component, by='')::
    |||
      sum by (%s) (
        promhttp_metric_handler_requests_in_flight{%s}
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of HTTP metric handler requests
  http_metric_handler_requests_rate(component, by='')::
    |||
      sum by (%s) (
        rate(promhttp_metric_handler_requests_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the number of samples from last evaluation
  last_evaluation_samples(component, by='')::
    |||
      sum by (%s) (
        loki_prometheus_last_evaluation_samples{%s}
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the number of discovered alertmanagers
  notifications_alertmanagers_discovered(component, by='')::
    |||
      sum by (%s) (
        loki_prometheus_notifications_alertmanagers_discovered{%s}
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the percentile of notification latency
  notifications_latency_percentile(component, percentile, by='')::
    |||
      histogram_quantile(
        %g,
        sum by (le, %s) (
          rate(loki_prometheus_notifications_latency_seconds_bucket{%s}[$__rate_interval])
        )
      )
    ||| % [
      percentile,
      by,
      self._resourceSelector(component).build(),
    ],

  // Gets the 99th percentile of notification latency
  notifications_latency_p99(component, by=''):: self.notifications_latency_percentile(component, 0.99, by),
  // Gets the 95th percentile of notification latency
  notifications_latency_p95(component, by=''):: self.notifications_latency_percentile(component, 0.95, by),
  // Gets the 90th percentile of notification latency
  notifications_latency_p90(component, by=''):: self.notifications_latency_percentile(component, 0.90, by),
  // Gets the 50th percentile of notification latency
  notifications_latency_p50(component, by=''):: self.notifications_latency_percentile(component, 0.50, by),

  // Gets the average notification latency
  notifications_latency_average(component, by='')::
    |||
      sum by (%s) (
        rate(loki_prometheus_notifications_latency_seconds_sum{%s}[$__rate_interval])
      )
      /
      sum by (%s) (
        rate(loki_prometheus_notifications_latency_seconds_count{%s}[$__rate_interval])
      )
    ||| % std.repeat([by, self._resourceSelector(component).build()], 2),

  // Gets the histogram of notification latency
  notifications_latency_histogram(component)::
    |||
      sum by (le) (
        rate(loki_prometheus_notifications_latency_seconds_bucket{%s}[$__rate_interval])
      )
    ||| % [self._resourceSelector(component).build()],

  // Gets the rate of notifications sent
  notifications_sent_rate(component, by='')::
    |||
      sum by (%s) (
        rate(loki_prometheus_notifications_sent_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the percentile of rule evaluation duration
  rule_evaluation_duration_percentile(component, percentile, by='')::
    |||
      histogram_quantile(
        %g,
        sum by (le, %s) (
          rate(loki_prometheus_rule_evaluation_duration_seconds_bucket{%s}[$__rate_interval])
        )
      )
    ||| % [
      percentile,
      by,
      self._resourceSelector(component).build(),
    ],

  // Gets the 99th percentile of rule evaluation duration
  rule_evaluation_duration_p99(component, by=''):: self.rule_evaluation_duration_percentile(component, 0.99, by),
  // Gets the 95th percentile of rule evaluation duration
  rule_evaluation_duration_p95(component, by=''):: self.rule_evaluation_duration_percentile(component, 0.95, by),
  // Gets the 90th percentile of rule evaluation duration
  rule_evaluation_duration_p90(component, by=''):: self.rule_evaluation_duration_percentile(component, 0.90, by),
  // Gets the 50th percentile of rule evaluation duration
  rule_evaluation_duration_p50(component, by=''):: self.rule_evaluation_duration_percentile(component, 0.50, by),

  // Gets the average rule evaluation duration
  rule_evaluation_duration_average(component, by='')::
    |||
      sum by (%s) (
        rate(loki_prometheus_rule_evaluation_duration_seconds_sum{%s}[$__rate_interval])
      )
      /
      sum by (%s) (
        rate(loki_prometheus_rule_evaluation_duration_seconds_count{%s}[$__rate_interval])
      )
    ||| % std.repeat([by, self._resourceSelector(component).build()], 2),

  // Gets the histogram of rule evaluation duration
  rule_evaluation_duration_histogram(component)::
    |||
      sum by (le) (
        rate(loki_prometheus_rule_evaluation_duration_seconds_bucket{%s}[$__rate_interval])
      )
    ||| % [self._resourceSelector(component).build()],

  // Gets the rate of rule evaluation failures
  rule_evaluation_failures_rate(component, by='')::
    |||
      sum by (%s) (
        rate(loki_prometheus_rule_evaluation_failures_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of rule evaluations
  rule_evaluations_rate(component, by='')::
    |||
      sum by (%s) (
        rate(loki_prometheus_rule_evaluations_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the percentile of rule group duration
  rule_group_duration_percentile(component, percentile, by='')::
    |||
      histogram_quantile(
        %g,
        sum by (le, %s) (
          rate(loki_prometheus_rule_group_duration_seconds_bucket{%s}[$__rate_interval])
        )
      )
    ||| % [
      percentile,
      by,
      self._resourceSelector(component).build(),
    ],

  // Gets the 99th percentile of rule group duration
  rule_group_duration_p99(component, by=''):: self.rule_group_duration_percentile(component, 0.99, by),
  // Gets the 95th percentile of rule group duration
  rule_group_duration_p95(component, by=''):: self.rule_group_duration_percentile(component, 0.95, by),
  // Gets the 90th percentile of rule group duration
  rule_group_duration_p90(component, by=''):: self.rule_group_duration_percentile(component, 0.90, by),
  // Gets the 50th percentile of rule group duration
  rule_group_duration_p50(component, by=''):: self.rule_group_duration_percentile(component, 0.50, by),

  // Gets the average rule group duration
  rule_group_duration_average(component, by='')::
    |||
      sum by (%s) (
        rate(loki_prometheus_rule_group_duration_seconds_sum{%s}[$__rate_interval])
      )
      /
      sum by (%s) (
        rate(loki_prometheus_rule_group_duration_seconds_count{%s}[$__rate_interval])
      )
    ||| % std.repeat([by, self._resourceSelector(component).build()], 2),

  // Gets the histogram of rule group duration
  rule_group_duration_histogram(component)::
    |||
      sum by (le) (
        rate(loki_prometheus_rule_group_duration_seconds_bucket{%s}[$__rate_interval])
      )
    ||| % [self._resourceSelector(component).build()],

  // Gets the rule group interval in seconds
  rule_group_interval_seconds(component, by='')::
    |||
      sum by (%s) (
        loki_prometheus_rule_group_interval_seconds{%s}
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of missed rule group iterations
  rule_group_iterations_missed_rate(component, by='')::
    |||
      sum by (%s) (
        rate(loki_prometheus_rule_group_iterations_missed_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the last rule group duration in seconds
  rule_group_last_duration_seconds(component, by='')::
    |||
      sum by (%s) (
        loki_prometheus_rule_group_last_duration_seconds{%s}
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the last rule group evaluation timestamp
  rule_group_last_evaluation_timestamp_seconds(component, by='')::
    |||
      sum by (%s) (
        loki_prometheus_rule_group_last_evaluation_timestamp_seconds{%s}
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the number of rules in rule groups
  rule_group_rules(component, by='')::
    |||
      sum by (%s) (
        loki_prometheus_rule_group_rules{%s}
      )
    ||| % [by, self._resourceSelector(component).build()],
}
