// imports
local config = import '../../config.libsonnet';
local variables = import '../variables.libsonnet';
local common = import './common.libsonnet';
local selector = (import '../selectors.libsonnet').new;

{
  // Shows the system-wide default values for all Loki limits
  default_overrides:
    |||
      max by (limit_name) (
        loki_overrides_defaults{%s}
      )
    ||| % [
      selector().component('overrides-exporter').build()
    ],

  // Shows the tenant-specific limit configurations
  overrides:
    |||
      max by (user, limit_name)(
        loki_overrides{%s}
      )
    ||| % [
      selector().component('overrides-exporter').user().build()
    ],

  // Shows effective limits by combining tenant-specific and default overrides
  merged_overrides:
    |||
      max by (limit_name) (
        loki_overrides{%s}
      )
      or
      max by (limit_name) (
        loki_overrides_defaults{%s}
        unless
        loki_overrides{%s}
      )
    ||| % [
      selector().component('overrides-exporter').user().build(),
      selector().component('overrides-exporter').build(),
      selector().component('overrides-exporter').user().build(),
    ],

  // Measures bytes per second being ingested
  ingestion_bytes_rate(recording_rule=config.use_recording_rules):
    if recording_rule then
      'loki_tenant:bytes:rate5m{%s}' % [selector().tenant().build()]
    else |||
      sum(
        rate(loki_distributor_bytes_received_total{%s}[$__rate_interval])
      )
    ||| % [
      selector().component('distributor').tenant().build()
    ],

  // Maximum allowed ingestion rate in bytes
  ingestion_bytes_rate_limit:
    |||
      max(
        loki_overrides{%s, limit_name="ingestion_rate_mb"}
        or
        loki_overrides_defaults{%s, limit_name="ingestion_rate_mb"}
      )
      # convert MB to bytes
      * 2^20
    ||| % [
      selector().component('overrides-exporter').user().build(),
      selector().component('overrides-exporter').build()
    ],

  // Measures log lines per second being ingested
  ingestion_lines_rate(recording_rule=config.use_recording_rules):
    if recording_rule then
      'loki_tenant:lines:rate5m{%s}' % [selector().tenant().build()]
    else |||
      sum(
        rate(loki_distributor_lines_received_total{%s}[$__rate_interval])
      )
    ||| % [
      selector().component('distributor').tenant().build()
    ],

  // Measures structured metadata bytes per second being ingested
  structured_metadata_bytes_rate(recording_rule=config.use_recording_rules):
    if recording_rule then
      'loki_tenant:structured_metadata_bytes:rate5m{%s}' % [selector().tenant().build()]
    else |||
      sum(
        rate(loki_distributor_structured_metadata_bytes_received_total{%s}[$__rate_interval])
      )
    ||| % [
      selector().component('distributor').tenant().build()
    ],

  // Average size of individual log lines including their metadata
  average_log_size(recording_rule=config.use_recording_rules):
    if recording_rule then |||
      (
        loki_tenant:bytes:rate5m{%s}
        +
        loki_tenant:structured_metadata_bytes:rate5m{%s}
      )
      /
      loki_tenant:lines:rate5m{%s}
    ||| % std.repeat([selector().tenant().build()], 3)
    else |||
      (
        sum(rate(loki_distributor_bytes_received_total{%s}[$__rate_interval]))
        +
        sum(rate(loki_distributor_structured_metadata_bytes_received_total{%s}[$__rate_interval]))
      )
      /
      sum(
        rate(loki_distributor_lines_received_total{%s}[$__rate_interval])
      )
    ||| % std.repeat([selector().component('distributor').tenant().build()], 3),

  // Measures bytes per second being rejected
  discarded_bytes_rate(recording_rule=config.use_recording_rules):
    if recording_rule then
      'loki_tenant:discarded_bytes:rate5m{%s}' % [selector().tenant().build()]
    else |||
      sum(
        rate(loki_discarded_bytes_total{%s}[$__rate_interval])
      )
    ||| % [selector().write().tenant().build()],

  // Measures log lines per second being rejected
  discarded_lines_rate(recording_rule=config.use_recording_rules):
    if recording_rule then
      'loki_tenant:discarded_lines:rate5m{%s}' % [selector().tenant().build()]
    else |||
      sum(
        rate(loki_discarded_lines_total{%s}[$__rate_interval])
      )
    ||| % [selector().write().tenant().build()],

  // Number of unique log streams currently active
  active_streams(recording_rule=config.use_recording_rules):
    if recording_rule then
        'loki_tenant:active_streams{%s}' % [selector().tenant().build()]
      else |||
        sum by (cluster, namespace, tenant) (
          loki_ingester_memory_streams{%s}
        )
        /
        on (namespace, cluster) group_left ()
        max by (namespace, cluster) (
          loki_distributor_replication_factor{%s}
        )
      ||| % [
        selector().component('ingester').build(),
        selector().component('distributor').build()
    ],
}
