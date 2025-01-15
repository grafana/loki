// imports
local config = import '../../config.libsonnet';
local variables = import '../variables.libsonnet';
local common = import './common.libsonnet';
local selector = (import '../selectors.libsonnet').new;
local lib = import '../../lib/_imports.libsonnet';

{
  // gets the frontend retries percentiles
  latency_percentile(component, percentile, by='type, range', recording_rule=config.recording_rules.loki)::
    if recording_rule then
      local componentSelector =
        selector()
          .component(component)
          .label('quantile').eq(percentile)
          .build();
      |||
        sum by (%s) (
          component_type_range:loki_logql_querystats_latency_seconds{%s}
        )
      ||| % [
        by,
        componentSelector,
      ]
    else
      local resourceSelector =
        selector()
          .resource(component)
          .build();
      |||
        histogram_quantile(
          %g,
          sum by (le, %s) (
            rate(loki_logql_querystats_latency_seconds_bucket{%s}[$__rate_interval])
          )
        )
      ||| % [
        percentile,
        by,
        resourceSelector,
      ],

  // gets the 99th percentile latency for a component
  latency_p99(component, by='type, range', recording_rule=config.recording_rules.loki)::
    self.latency_percentile(
      component=component,
      percentile=0.99,
      by=by,
      recording_rule=recording_rule,
    ),

  // gets the 95th percentile latency for a component
  latency_p95(component, by='type, range', recording_rule=config.recording_rules.loki)::
    self.latency_percentile(
      component=component,
      percentile=0.95,
      by=by,
      recording_rule=recording_rule,
  ),

  // gets the 90th percentile latency for a component
  latency_p90(component, by='type, range', recording_rule=config.recording_rules.loki)::
    self.latency_percentile(
      component=component,
      percentile=0.90,
      by=by,
      recording_rule=recording_rule,
    ),

  // gets the 50th percentile latency for a component
  latency_p50(component, by='type, range', recording_rule=config.recording_rules.loki)::
    self.latency_percentile(
      component=component,
      percentile=0.50,
      by=by,
      recording_rule=recording_rule,
    ),

  // gets the average number of frontend retries
  latency_average(component, by='type, range', recording_rule=config.recording_rules.loki)::
    if recording_rule then
      local componentSelector =
        selector()
          .component(component)
          .build();
      |||
        sum by (%s) (
          component_type_range:loki_logql_querystats_latency_seconds:avg{%s}
        )
      ||| % [
        by,
        componentSelector,
      ]
    else
      local resourceSelector =
        selector()
          .resource(component)
          .build();
      |||
        sum by (%s) (
          rate(loki_logql_querystats_latency_seconds_sum{%s}[$__rate_interval])
        )
        /
        sum by (%s) (
          rate(loki_logql_querystats_latency_seconds_count{%s}[$__rate_interval])
        )
      ||| % std.repeat([by, resourceSelector], 2),

  // gets the number of queries per second
  qps(component, by='status_code, type, range', recording_rule=config.recording_rules.loki)::
    if recording_rule then
      local componentSelector =
        selector()
          .component(component)
          .build();
      |||
        sum by (%s) (
          label_replace(
            component_type_range:loki_logql_querystats_latency_seconds_count:sum_rate{%s},
            "range", "n/a", "range", ""
          )
        )
      ||| % [
        by,
        componentSelector,
      ]
    else
      local resourceSelector =
        selector()
          .resource(component)
          .build();
      |||
        sum by (%s) (
          label_replace(
            rate(loki_logql_querystats_latency_seconds_count{%s}[$__rate_interval]),
            "range", "n/a", "range", ""
          )
        )
      ||| % [
        by,
        resourceSelector,
      ],

  // gets the bytes processed per second percentiles
  bytes_processed_rate_percentile(component, percentile, by='type, range')::
    local resourceSelector =
      selector()
        .resource(component)
        .build();
    |||
      histogram_quantile(
        %g,
        sum by (le, %s) (
          rate(loki_logql_querystats_bytes_processed_per_seconds_bucket{%s}[$__rate_interval])
        )
      )
    ||| % [
      percentile,
      by,
      resourceSelector,
    ],

  // gets the bytes processed per second histogram
  bytes_processed_histogram(component)::
    local resourceSelector =
      selector()
        .resource(component)
        .build();
    |||
      sum by (le) (
        rate(loki_logql_querystats_bytes_processed_per_seconds_bucket{%s}[$__rate_interval])
      )
    ||| % [resourceSelector],

  // gets the 99th percentile bytes processed per second for a component
  bytes_processed_rate_p99(component, by='type, range')::
    self.bytes_processed_rate_percentile(
      component=component,
      percentile=0.99,
      by=by,
    ),

  // gets the 95th percentile bytes processed per second for a component
  bytes_processed_rate_p95(component, by='type, range')::
    self.bytes_processed_rate_percentile(
      component=component,
      percentile=0.95,
      by=by,
    ),

  // gets the 90th percentile bytes processed per second for a component
  bytes_processed_rate_p90(component, by='type, range')::
    self.bytes_processed_rate_percentile(
      component=component,
      percentile=0.90,
      by=by,
    ),

  // gets the 50th percentile bytes processed per second for a component
  bytes_processed_rate_p50(component, by='type, range')::
    self.bytes_processed_rate_percentile(
      component=component,
      percentile=0.50,
      by=by,
    ),

  // gets the average bytes processed per second
  bytes_processed_rate_average(component, by='type, range')::
    local resourceSelector =
      selector()
        .resource(component)
        .build();
    |||
      sum by (%s) (
        rate(loki_logql_querystats_bytes_processed_per_seconds_sum{%s}[$__rate_interval])
      )
      /
      sum by (%s) (
        rate(loki_logql_querystats_bytes_processed_per_seconds_count{%s}[$__rate_interval])
      )
    ||| % std.repeat([by, resourceSelector], 2),


  // gets the chunk download latency percentiles
  chunk_download_latency_percentile(component, percentile)::
    |||
      histogram_quantile(
        %g,
        sum by (le) (
          rate(loki_logql_querystats_chunk_download_latency_seconds_bucket{%s}[$__rate_interval])
        )
      )
    ||| % [
      percentile,
      self._resourceSelector,
    ],

  // gets the 99th percentile chunk download latency for a component
  chunk_download_latency_p99(component, by='type, range', recording_rule=config.recording_rules.loki)::
    self.chunk_download_latency_percentile(
      component=component,
      percentile=0.99,
      by=by,
      recording_rule=recording_rule,
    ),

  // gets the 95th percentile chunk download latency for a component
  chunk_download_latency_p95(component, by='type, range', recording_rule=config.recording_rules.loki)::
    self.chunk_download_latency_percentile(
      component=component,
      percentile=0.95,
      by=by,
      recording_rule=recording_rule,
    ),

  // gets the 90th percentile chunk download latency for a component
  chunk_download_latency_p90(component, by='type, range', recording_rule=config.recording_rules.loki)::
    self.chunk_download_latency_percentile(
      component=component,
      percentile=0.90,
      by=by,
      recording_rule=recording_rule,
    ),

  // gets the 50th percentile chunk download latency for a component
  chunk_download_latency_p50(component, by='type, range', recording_rule=config.recording_rules.loki)::
    self.chunk_download_latency_percentile(
      component=component,
      percentile=0.50,
      by=by,
      recording_rule=recording_rule,
    ),

  // gets the average chunk download latency
  chunk_download_latency_average(component)::
    |||
      sum(
        rate(loki_logql_querystats_chunk_download_latency_seconds_sum{%s}[$__rate_interval])
      )
      /
      sum(
        rate(loki_logql_querystats_chunk_download_latency_seconds_count{%s}[$__rate_interval])
      )
    ||| % std.repeat([self._resourceSelector], 2),

  // gets the chunk downloaded per second
  chunk_downloads_rate(component, by='type, range')::
    local resourceSelector =
      selector()
        .resource(component)
        .build();
    |||
      sum by (%s)(
        label_replace(
          rate(loki_logql_querystats_downloaded_chunk_total{%s}[$__rate_interval]),
          "range", "n/a", "range", ""
        )
      )
    ||| % [
      by,
      resourceSelector,
    ],

  // gets the number of duplicates
  duplicates_rate(component)::
    local resourceSelector =
      selector()
        .resource(component)
        .build();
    |||
      sum(
        rate(loki_logql_querystats_duplicates_total{%s}[$__rate_interval])
      )
    ||| % [resourceSelector],

  // gets the number of lines per second sent to the ingester
  ingester_sent_lines(component)::
    local resourceSelector =
      selector()
        .resource(component)
        .build();
    |||
      sum(
        rate(loki_logql_querystats_ingester_sent_lines_total{%s}[$__rate_interval])
      )
    ||| % [resourceSelector],
}
