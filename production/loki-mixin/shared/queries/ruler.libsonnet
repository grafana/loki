// imports
local config = import '../../config.libsonnet';
local variables = import '../variables.libsonnet';
local selector = (import '../selectors.libsonnet').new;
local lib = import '../../lib/_imports.libsonnet';

{
  _resourceSelector(component)::
    local sel = selector();
    if std.isArray(component) then
      sel.resources(component)
    else
      sel.resource(component),

  // Histogram queries for client request duration
  // Gets the percentile of client request duration
  client_request_duration_percentile(component, percentile, by=config.labels.resource_selector)::
    |||
      histogram_quantile(
        %g,
        sum by (le, %s) (
          rate(loki_ruler_client_request_duration_seconds_bucket{%s}[$__rate_interval])
        )
      )
    ||| % [
      percentile,
      by,
      self._resourceSelector(component).build(),
    ],

  // Gets the percentile of remote eval request duration
  remote_eval_request_duration_percentile(component, percentile, by=config.labels.resource_selector)::
    |||
      histogram_quantile(
        %g,
        sum by (le, %s) (
          rate(loki_ruler_remote_eval_request_duration_seconds_bucket{%s}[$__rate_interval])
        )
      )
    ||| % [
      percentile,
      by,
      self._resourceSelector(component).build(),
    ],

  // Gets the percentile of WAL cleanup duration
  wal_cleaner_cleanup_duration_percentile(component, percentile, by=config.labels.resource_selector)::
    |||
      histogram_quantile(
        %g,
        sum by (le, %s) (
          rate(loki_ruler_wal_cleaner_cleanup_seconds_bucket{%s}[$__rate_interval])
        )
      )
    ||| % [
      percentile,
      by,
      self._resourceSelector(component).build(),
    ],

  // Counter rates
  // Gets the rate of remote evaluation failures
  remote_eval_failures_rate(component, by=config.labels.resource_selector)::
    |||
      sum by (%s) (
        rate(loki_ruler_remote_eval_failure_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of remote evaluation successes
  remote_eval_success_rate(component, by=config.labels.resource_selector)::
    |||
      sum by (%s) (
        rate(loki_ruler_remote_eval_success_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of rule syncs
  sync_rules_rate(component, by=config.labels.resource_selector)::
    |||
      sum by (%s) (
        rate(loki_ruler_sync_rules_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of WAL samples appended
  wal_samples_appended_rate(component, by=config.labels.resource_selector)::
    |||
      sum by (%s) (
        rate(loki_ruler_wal_samples_appended_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of remote storage samples dropped
  remote_storage_samples_dropped_rate(component, by=config.labels.resource_selector)::
    |||
      sum by (%s) (
        rate(loki_ruler_wal_prometheus_remote_storage_samples_dropped_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of remote storage samples failed
  remote_storage_samples_failed_rate(component, by=config.labels.resource_selector)::
    |||
      sum by (%s) (
        rate(loki_ruler_wal_prometheus_remote_storage_samples_failed_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gauge metrics
  // Gets the number of connected clients
  clients(component, by=config.labels.resource_selector)::
    |||
      loki_ruler_clients{%s}
    ||| % [self._resourceSelector(component).build()],

  // Gets whether the last config reload was successful
  config_last_reload_successful(component, by=config.labels.resource_selector)::
    |||
      loki_ruler_config_last_reload_successful{%s}
    ||| % [self._resourceSelector(component).build()],

  // Gets the timestamp of the last successful config reload
  config_last_reload_success_timestamp(component, by=config.labels.resource_selector)::
    |||
      loki_ruler_config_last_reload_successful_seconds{%s}
    ||| % [self._resourceSelector(component).build()],

  // Gets the total number of rule managers
  managers_total(component, by=config.labels.resource_selector)::
    |||
      loki_ruler_managers_total{%s}
    ||| % [self._resourceSelector(component).build()],

  // Gets whether the WAL appender is ready
  wal_appender_ready(component, by=config.labels.resource_selector)::
    |||
      loki_ruler_wal_appender_ready{%s}
    ||| % [self._resourceSelector(component).build()],

  // Gets the WAL disk size
  wal_disk_size(component, by=config.labels.resource_selector)::
    |||
      loki_ruler_wal_disk_size{%s}
    ||| % [self._resourceSelector(component).build()],

  // Gets the number of pending remote storage samples
  remote_storage_samples_pending(component, by=config.labels.resource_selector)::
    |||
      loki_ruler_wal_prometheus_remote_storage_samples_pending{%s}
    ||| % [self._resourceSelector(component).build()],

  // Gets the number of desired remote storage shards
  remote_storage_shards_desired(component, by=config.labels.resource_selector)::
    |||
      loki_ruler_wal_prometheus_remote_storage_shards_desired{%s}
    ||| % [self._resourceSelector(component).build()],

  // Calculated metrics
  // Gets the remote evaluation success ratio
  remote_eval_success_ratio(component, by=config.labels.resource_selector)::
    |||
      sum by (%s) (rate(loki_ruler_remote_eval_success_total{%s}[$__rate_interval]))
      /
      (
        sum by (%s) (rate(loki_ruler_remote_eval_success_total{%s}[$__rate_interval]))
        +
        sum by (%s) (rate(loki_ruler_remote_eval_failure_total{%s}[$__rate_interval]))
      )
    ||| % std.repeat([by, self._resourceSelector(component).build()], 3),

  // Gets the remote storage sample failure ratio
  remote_storage_failure_ratio(component, by=config.labels.resource_selector)::
    |||
      (
        sum by (%s) (rate(loki_ruler_wal_prometheus_remote_storage_samples_failed_total{%s}[$__rate_interval]))
        +
        sum by (%s) (rate(loki_ruler_wal_prometheus_remote_storage_samples_dropped_total{%s}[$__rate_interval]))
      )
      /
      sum by (%s) (rate(loki_ruler_wal_prometheus_remote_storage_samples_total{%s}[$__rate_interval]))
    ||| % std.repeat([by, self._resourceSelector(component).build()], 3),
}
