// imports
local config = import '../../config.libsonnet';
local variables = import '../variables.libsonnet';
local common = import './common.libsonnet';
local selector = (import '../selectors.libsonnet').new;

{
  default_overrides:
    common.queryType('instant')
      + common.promql.withExpr(|||
        max by (limit_name) (
            loki_overrides_defaults{%s}
        )
      ||| % [
        selector().overridesExporter().build()
      ])
      + common.promql.withRefId('Default Overrides')
      + common.promql.withFormat('table'),

  overrides:
    common.queryType('instant')
      + common.promql.withExpr(|||
        max by (user, limit_name)(
            loki_overrides{%s}
        )
      ||| % [
        selector().overridesExporter().user().build()
      ])
      + common.promql.withRefId('Tenant Overrides')
      + common.promql.withFormat('table'),

  merged_overrides:
    common.queryType('instant')
      + common.promql.withExpr(|||
        max by (limit_name) (loki_overrides{%s})
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
       ])
      + common.promql.withFormat('table'),

  ingestion_bytes_rate:
    common.queryType('range')
      + common.promql.withExpr(
        if config.use_recording_rules then
          'loki_tenant:bytes:rate5m{%s}' % [
            selector().tenant().build()
          ]
        else |||
          sum(rate(loki_distributor_bytes_received_total{%s}[$__rate_interval]))
        ||| % [
          selector().distributor().tenant().build()
        ])
      + common.promql.withLegendFormat('Tenant')
      + common.promql.withRefId('Ingestion Bytes Rate'),

  ingestion_bytes_rate_limit:
    common.queryType('range')
      + common.promql.withExpr(|||
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
      ])
      + common.promql.withLegendFormat('Rate Limit')
      + common.promql.withRefId('Ingestion Rate Limit'),

  ingestion_lines_rate:
    common.queryType('range')
      + common.promql.withExpr(
        if config.use_recording_rules then
          'loki_tenant:lines:rate5m{%s}' % [
            selector().tenant()
              .build()
          ]
        else |||
          sum(rate(loki_distributor_lines_received_total{%s}[$__rate_interval]))
        ||| % [
          selector().distributor().tenant().build()
      ])
      + common.promql.withLegendFormat('Tenant')
      + common.promql.withRefId('Lines Rate'),

  structured_metadata_bytes_rate:
    common.queryType('range')
      + common.promql.withExpr(
        if config.use_recording_rules then
          'loki_tenant:structured_metadata_bytes:rate5m{%s}' % [
            selector().tenant().build()
          ]
        else |||
          sum(rate(loki_distributor_structured_metadata_bytes_received_total{%s}[$__rate_interval]))
        ||| % [
          selector().distributor().tenant().build()
        ])
      + common.promql.withLegendFormat('Tenant')
      + common.promql.withRefId('Structured Metadata Bytes Rate'),

  average_log_size:
    common.queryType('range')
      + common.promql.withExpr(
        if config.use_recording_rules then
          |||
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
          sum(rate(loki_distributor_lines_received_total{%s}[$__rate_interval]))
        ||| % std.repeat([selector().distributor().tenant().build()], 3)
      )
      + common.promql.withLegendFormat('Tenant')
      + common.promql.withRefId('Tenant Ingestion Bytes Rate'),

  discarded_bytes_rate:
    common.queryType('range')
      + common.promql.withExpr(
        if config.use_recording_rules then
          |||
            loki_tenant:discarded_bytes:rate5m{%s}
          ||| % [
            selector().tenant().build()
          ]
        else |||
          sum(rate(loki_discarded_bytes_total{%s}[$__rate_interval]))
        ||| % [
          selector().write().tenant().build()
        ])
      + common.promql.withLegendFormat('{{reason}}')
      + common.promql.withRefId('Tenant Discarded Bytes Rate'),

  discarded_lines_rate:
    common.queryType('range')
      + common.promql.withExpr(
        if config.use_recording_rules then
          |||
            loki_tenant:discarded_lines:rate5m{%s}
          ||| % [
            selector().tenant().build()
          ]
        else |||
          sum(rate(loki_discarded_lines_total{%s}[$__rate_interval]))
        ||| % [
          selector().write().tenant().build()
        ])
      + common.promql.withLegendFormat('{{reason}}')
      + common.promql.withRefId('Tenant Discarded Lines Rate'),

  active_streams:
    common.queryType('range')
      + common.promql.withExpr(
        if config.use_recording_rules then
          'loki_tenant:active_streams{%s}' % [
            selector().tenant().build()
          ]
        else |||
          sum by (cluster, namespace, tenant) (loki_ingester_memory_streams{%s})
          /
          on (namespace, cluster) group_left ()
          max by (namespace, cluster) (loki_distributor_replication_factor{%s})
        ||| % [
          selector().ingester().build(),
          selector().distributor().build()
      ])
      + common.promql.withRefId('Active Streams')
      + common.promql.withLegendFormat('Streams'),
}
