// imports
local config = import '../../config.libsonnet';
local variables = import '../variables.libsonnet';
local common = import './common.libsonnet';
local selector = (import '../selectors.libsonnet').new;

{
  default_overrides:
    |||
      max by (limit_name) (
        loki_overrides_defaults{%s}
      )
    ||| % [
      selector().overridesExporter().build()
    ],

  overrides:
    |||
      max by (user, limit_name)(
        loki_overrides{%s}
      )
    ||| % [
      selector().overridesExporter().user().build()
    ],

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
      selector().overridesExporter().user().build(),
      selector().overridesExporter().build(),
      selector().overridesExporter().user().build(),
    ],

  ingestion_bytes_rate:
    if config.use_recording_rules then
      'loki_tenant:bytes:rate5m{%s}' % [selector().tenant().build()]
    else |||
      sum(
        rate(loki_distributor_bytes_received_total{%s}[$__rate_interval])
      )
    ||| % [
      selector().distributor().tenant().build()
    ],

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
      selector().overridesExporter().user().build(),
      selector().overridesExporter().build()
    ],

  ingestion_lines_rate:
    if config.use_recording_rules then
      'loki_tenant:lines:rate5m{%s}' % [selector().tenant().build()]
    else |||
      sum(
        rate(loki_distributor_lines_received_total{%s}[$__rate_interval])
      )
    ||| % [
      selector().distributor().tenant().build()
    ],

  structured_metadata_bytes_rate:
    if config.use_recording_rules then
      'loki_tenant:structured_metadata_bytes:rate5m{%s}' % [selector().tenant().build()]
    else |||
      sum(
        rate(loki_distributor_structured_metadata_bytes_received_total{%s}[$__rate_interval])
      )
    ||| % [
      selector().distributor().tenant().build()
    ],

  average_log_size:
    if config.use_recording_rules then |||
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
    ||| % std.repeat([selector().distributor().tenant().build()], 3),

  discarded_bytes_rate:
    if config.use_recording_rules then
      'loki_tenant:discarded_bytes:rate5m{%s}' % [selector().tenant().build()]
    else |||
      sum(
        rate(loki_discarded_bytes_total{%s}[$__rate_interval])
      )
    ||| % [selector().write().tenant().build()],

  discarded_lines_rate:
    if config.use_recording_rules then
      'loki_tenant:discarded_lines:rate5m{%s}' % [selector().tenant().build()]
    else |||
      sum(
        rate(loki_discarded_lines_total{%s}[$__rate_interval])
      )
    ||| % [selector().write().tenant().build()],

  active_streams:
    if config.use_recording_rules then
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
        selector().ingester().build(),
        selector().distributor().build()
    ],
}
