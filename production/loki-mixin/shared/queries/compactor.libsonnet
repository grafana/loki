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

  // Counter rates
  // Gets the rate of retention operations
  retention_operation_rate(component, by=config.labels.resource_selector)::
    |||
      sum by (%s) (
        rate(loki_compactor_apply_retention_operation_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of delete processing failures
  delete_processing_fails_rate(component, by=config.labels.resource_selector)::
    |||
      sum by (%s) (
        rate(loki_compactor_delete_processing_fails_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of load pending requests attempts
  load_pending_requests_attempts_rate(component, by=config.labels.resource_selector)::
    |||
      sum by (%s) (
        rate(loki_compactor_load_pending_requests_attempts_total{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gets the rate of locked table successive compaction skips
  locked_table_compaction_skips_rate(component, by=config.labels.resource_selector)::
    |||
      sum by (%s) (
        rate(loki_compactor_locked_table_successive_compaction_skips{%s}[$__rate_interval])
      )
    ||| % [by, self._resourceSelector(component).build()],

  // Gauge metrics
  // Gets whether the boltdb shipper compactor is running
  boltdb_shipper_compactor_running(component, by=config.labels.resource_selector)::
    |||
      loki_boltdb_shipper_compactor_running{%s}
    ||| % [self._resourceSelector(component).build()],

  // Gets the timestamp of last successful retention run
  retention_last_successful_run(component, by=config.labels.resource_selector)::
    |||
      loki_compactor_apply_retention_last_successful_run_timestamp_seconds{%s}
    ||| % [self._resourceSelector(component).build()],

  // Gets the duration of retention operations
  retention_operation_duration(component, by=config.labels.resource_selector)::
    |||
      loki_compactor_apply_retention_operation_duration_seconds{%s}
    ||| % [self._resourceSelector(component).build()],

  // Gets the age of oldest pending delete request
  oldest_pending_delete_seconds(component, by=config.labels.resource_selector)::
    |||
      loki_compactor_oldest_pending_delete_request_age_seconds{%s}
    ||| % [self._resourceSelector(component).build()],

  // Gets the count of pending delete requests
  pending_delete_requests_count(component, by=config.labels.resource_selector)::
    |||
      loki_compactor_pending_delete_requests_count{%s}
    ||| % [self._resourceSelector(component).build()],

  // Calculated metrics
  // Gets the time since last successful retention run
  retention_last_run_age_seconds(component, by=config.labels.resource_selector)::
    |||
      time() - loki_compactor_apply_retention_last_successful_run_timestamp_seconds{%s}
    ||| % [self._resourceSelector(component).build()],

  // Gets the delete processing success ratio
  delete_processing_success_ratio(component, by=config.labels.resource_selector)::
    |||
      1 - (
        sum by (%s) (rate(loki_compactor_delete_processing_fails_total{%s}[$__rate_interval]))
        /
        sum by (%s) (rate(loki_compactor_apply_retention_operation_total{%s}[$__rate_interval]))
      )
    ||| % std.repeat([by, self._resourceSelector(component).build()], 2),

  // Gets the delete processing error ratio
  delete_processing_error_ratio(component, by=config.labels.resource_selector)::
    |||
      sum by (%s) (rate(loki_compactor_delete_processing_fails_total{%s}[$__rate_interval]))
      /
      sum by (%s) (rate(loki_compactor_apply_retention_operation_total{%s}[$__rate_interval]))
    ||| % std.repeat([by, self._resourceSelector(component).build()], 2),
}
