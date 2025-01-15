// imports
local config = import '../../../config.libsonnet';
local variables = import '../../variables.libsonnet';
local selector = (import '../../selectors.libsonnet').new;
local lib = import '../../../lib/_imports.libsonnet';
{
  _component:: 'bloom-planner',

  _resourceSelector::
    selector()
      .resource(self._component)
      .build(),

  // Histogram queries for build time
  // Gets the percentile of build time duration
  build_time_percentile(percentile, by='')::
    |||
      histogram_quantile(
        %g,
        sum by (le, %s) (
          rate(loki_bloomplanner_build_time_seconds_bucket{%s}[$__rate_interval])
        )
      )
    ||| % [
      percentile,
      by,
      self._resourceSelector,
    ],

  // Gets the 99th percentile of build time duration
  build_time_p99(by=''):: self.build_time_percentile(0.99, by),
  // Gets the 95th percentile of build time duration
  build_time_p95(by=''):: self.build_time_percentile(0.95, by),
  // Gets the 90th percentile of build time duration
  build_time_p90(by=''):: self.build_time_percentile(0.90, by),
  // Gets the 50th percentile of build time duration
  build_time_p50(by=''):: self.build_time_percentile(0.50, by),

  // Gets the average build time duration
  build_time_average(by='')::
    |||
      sum by (%s) (
        rate(loki_bloomplanner_build_time_seconds_sum{%s}[$__rate_interval])
      )
      /
      sum by (%s) (
        rate(loki_bloomplanner_build_time_seconds_count{%s}[$__rate_interval])
      )
    ||| % std.repeat([by, self._resourceSelector], 2),

  // Gets the average build time duration
  build_time_histogram()::
    |||
      sum by (le) (
        rate(loki_bloomplanner_build_time_seconds_bucket{%s}[$__rate_interval])
      )
      /
      sum by (%s) (
        rate(loki_bloomplanner_build_time_seconds_count{%s}[$__rate_interval])
      )
    ||| % [self._resourceSelector],

  // Histogram queries for planning time
  // Gets the percentile of planning time duration
  planning_time_percentile(percentile, by='')::
    |||
      histogram_quantile(
        %g,
        sum by (le, %s) (
          rate(loki_bloomplanner_planning_time_seconds_bucket{%s}[$__rate_interval])
        )
      )
    ||| % [
      percentile,
      by,
      self._resourceSelector,
    ],

  // Gets the 99th percentile of planning time duration
  planning_time_p99(by=''):: self.planning_time_percentile(0.99, by),
  // Gets the 95th percentile of planning time duration
  planning_time_p95(by=''):: self.planning_time_percentile(0.95, by),
  // Gets the 90th percentile of planning time duration
  planning_time_p90(by=''):: self.planning_time_percentile(0.90, by),
  // Gets the 50th percentile of planning time duration
  planning_time_p50(by=''):: self.planning_time_percentile(0.50, by),

  // Gets the average planning time duration
  planning_time_average(by='')::
    |||
      sum by (%s) (
        rate(loki_bloomplanner_planning_time_seconds_sum{%s}[$__rate_interval])
      )
      /
      sum by (%s) (
        rate(loki_bloomplanner_planning_time_seconds_count{%s}[$__rate_interval])
      )
    ||| % std.repeat([by, self._resourceSelector], 2),

  // Histogram queries for queue duration
  // Gets the percentile of queue duration
  queue_duration_percentile(percentile, by='')::
    |||
      histogram_quantile(
        %g,
        sum by (le, %s) (
          rate(loki_bloomplanner_queue_duration_seconds_bucket{%s}[$__rate_interval])
        )
      )
    ||| % [
      percentile,
      by,
      self._resourceSelector,
    ],

  // Gets the 99th percentile of queue duration
  queue_duration_p99(by=''):: self.queue_duration_percentile(0.99, by),
  // Gets the 95th percentile of queue duration
  queue_duration_p95(by=''):: self.queue_duration_percentile(0.95, by),
  // Gets the 90th percentile of queue duration
  queue_duration_p90(by=''):: self.queue_duration_percentile(0.90, by),
  // Gets the 50th percentile of queue duration
  queue_duration_p50(by=''):: self.queue_duration_percentile(0.50, by),

  // Gets the average queue duration
  queue_duration_average(by='')::
    |||
      sum by (%s) (
        rate(loki_bloomplanner_queue_duration_seconds_sum{%s}[$__rate_interval])
      )
      /
      sum by (%s) (
        rate(loki_bloomplanner_queue_duration_seconds_count{%s}[$__rate_interval])
      )
    ||| % std.repeat([by, self._resourceSelector], 2),

  // Counter rates
  // Gets the rate of completed builds
  build_completed_rate::
    |||
      sum(
        rate(loki_bloomplanner_build_completed_total{%s}[$__rate_interval])
      )
    ||| % [self._resourceSelector],

  // Gets the rate of started builds
  build_started_rate::
    |||
      sum(
        rate(loki_bloomplanner_build_started_total{%s}[$__rate_interval])
      )
    ||| % [self._resourceSelector],

  // Gets the rate of lost tasks
  tasks_lost_rate::
    |||
      sum(
        rate(loki_bloomplanner_tasks_lost_total{%s}[$__rate_interval])
      )
    ||| % [self._resourceSelector],

  // Gets the rate of requeued tasks
  tasks_requeued_rate::
    |||
      sum(
        rate(loki_bloomplanner_tasks_requeued_total{%s}[$__rate_interval])
      )
    ||| % [self._resourceSelector],

  // Gets the rate of discovered tenants
  tenants_discovered_rate::
    |||
      sum(
        rate(loki_bloomplanner_tenants_discovered_total{%s}[$__rate_interval])
      )
    ||| % [self._resourceSelector],

  // Gauge metrics
  // Gets the timestamp of last successful build
  build_last_successful_run::
    |||
      loki_bloomplanner_build_last_successful_run_timestamp_seconds{%s}
    ||| % [self._resourceSelector],

  // Gets the number of connected builders
  connected_builders::
    |||
      loki_bloomplanner_connected_builders{%s}
    ||| % [self._resourceSelector],

  // Gets the number of inflight tasks
  inflight_tasks::
    |||
      loki_bloomplanner_inflight_tasks{%s}
    ||| % [self._resourceSelector],

  // Gets whether retention is running
  retention_running::
    |||
      loki_bloomplanner_retention_running{%s}
    ||| % [self._resourceSelector],

  // Gets the number of tenants exceeding lookback
  retention_tenants_exceeding_lookback::
    |||
      loki_bloomplanner_retention_tenants_exceeding_lookback{%s}
    ||| % [self._resourceSelector],

  // Gets whether the bloom planner is running
  running::
    |||
      loki_bloomplanner_running{%s}
    ||| % [self._resourceSelector],

  // Histogram queries for retention days processed
  retention_days_processed_percentile(percentile, by='')::
    |||
      histogram_quantile(
        %g,
        sum by (le, %s) (
          rate(loki_bloomplanner_retention_days_processed_bucket{%s}[$__rate_interval])
        )
      )
    ||| % [
      percentile,
      by,
      self._resourceSelector,
    ],

  // Gets the 99th percentile of retention days processed
  retention_days_processed_p99(by=''):: self.retention_days_processed_percentile(0.99, by),
  // Gets the 95th percentile of retention days processed
  retention_days_processed_p95(by=''):: self.retention_days_processed_percentile(0.95, by),
  // Gets the 90th percentile of retention days processed
  retention_days_processed_p90(by=''):: self.retention_days_processed_percentile(0.90, by),
  // Gets the 50th percentile of retention days processed
  retention_days_processed_p50(by=''):: self.retention_days_processed_percentile(0.50, by),

  // Gets the average retention days processed
  retention_days_processed_average(by='')::
    |||
      sum by (%s) (
        rate(loki_bloomplanner_retention_days_processed_sum{%s}[$__rate_interval])
      )
      /
      sum by (%s) (
        rate(loki_bloomplanner_retention_days_processed_count{%s}[$__rate_interval])
      )
    ||| % std.repeat([by, self._resourceSelector], 2),

  // Gets the histogram of retention days processed
  retention_days_processed_histogram()::
    |||
      sum by (le) (
        rate(loki_bloomplanner_retention_days_processed_bucket{%s}[$__rate_interval])
      )
      /
      sum by (%s) (
        rate(loki_bloomplanner_retention_days_processed_count{%s}[$__rate_interval])
      )
    ||| % [self._resourceSelector],

  // Histogram queries for tenant task size
  tenant_task_size_percentile(percentile, by='')::
    |||
      histogram_quantile(
        %g,
        sum by (le, %s) (
          rate(loki_bloomplanner_tenant_task_size_bytes_bucket{%s}[$__rate_interval])
        )
      )
    ||| % [
      percentile,
      by,
      self._resourceSelector,
    ],

  // Gets the 99th percentile of tenant task size
  tenant_task_size_p99(by=''):: self.tenant_task_size_percentile(0.99, by),
  // Gets the 95th percentile of tenant task size
  tenant_task_size_p95(by=''):: self.tenant_task_size_percentile(0.95, by),
  // Gets the 90th percentile of tenant task size
  tenant_task_size_p90(by=''):: self.tenant_task_size_percentile(0.90, by),
  // Gets the 50th percentile of tenant task size
  tenant_task_size_p50(by=''):: self.tenant_task_size_percentile(0.50, by),

  // Gets the average tenant task size
  tenant_task_size_average(by='')::
    |||
      sum by (%s) (
        rate(loki_bloomplanner_tenant_task_size_bytes_sum{%s}[$__rate_interval])
      )
      /
      sum by (%s) (
        rate(loki_bloomplanner_tenant_task_size_bytes_count{%s}[$__rate_interval])
      )
    ||| % std.repeat([by, self._resourceSelector], 2),

  // Gets the histogram of tenant task size
  tenant_task_size_histogram()::
    |||
      sum by (le) (
        rate(loki_bloomplanner_tenant_task_size_bytes_bucket{%s}[$__rate_interval])
      )
      /
      sum by (%s) (
        rate(loki_bloomplanner_tenant_task_size_bytes_count{%s}[$__rate_interval])
      )
    ||| % [self._resourceSelector],
}
