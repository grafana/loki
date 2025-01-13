// imports
local config = import '../../config.libsonnet';
local variables = import '../variables.libsonnet';
local common = import './common.libsonnet';
local selector = (import '../selectors.libsonnet').new;

{
  // Shows the system-wide default values for all Loki limits
  default_overrides:
    local resourceSelector =
      selector()
        .overridesExporter()
        .build();
    |||
      max by (limit_name) (
        loki_overrides_defaults{%s}
      )
    ||| % [resourceSelector],

  // Shows the tenant-specific limit configurations
  overrides:
    local resourceSelector =
      selector()
        .overridesExporter()
        .build();
    |||
      max by (user, limit_name)(
        loki_overrides{%s}
      )
    ||| % [resourceSelector],

  // Shows effective limits by combining tenant-specific and default overrides
  merged_overrides:
    local resourceSelector =
      selector()
        .overridesExporter()
        .build();
    local userSelector =
      selector()
        .overridesExporter()
        .user()
        .build();
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
    ||| % [userSelector, resourceSelector, userSelector],

  // Measures bytes per second being ingested
  ingestion_bytes_rate(recording_rule=config.use_recording_rules):
    if recording_rule then
      local tenantSelector =
        selector()
          .tenant()
          .build();
      'loki_tenant:bytes:rate5m{%s}' % [tenantSelector]
    else
      local tenantSelector =
        selector()
          .distributor()
          .tenant()
          .build();
      |||
        sum(
          rate(loki_distributor_bytes_received_total{%s}[$__rate_interval])
        )
      ||| % [tenantSelector],

  // Maximum allowed ingestion rate in bytes
  ingestion_bytes_rate_limit:
    local resourceSelector =
      selector()
        .overridesExporter()
        .build();
    local userSelector =
      selector()
        .overridesExporter()
        .user()
        .build();
    |||
      max(
        loki_overrides{%s, limit_name="ingestion_rate_mb"}
        or
        loki_overrides_defaults{%s, limit_name="ingestion_rate_mb"}
      )
      # convert MB to bytes
      * 2^20
    ||| % [userSelector, resourceSelector],

  // Measures log lines per second being ingested
  ingestion_lines_rate(recording_rule=config.use_recording_rules):
    if recording_rule then
      local tenantSelector =
        selector()
          .tenant()
          .build();
      'loki_tenant:lines:rate5m{%s}' % [tenantSelector]
    else
      local tenantSelector =
        selector()
          .distributor()
          .tenant()
          .build();
      |||
        sum(
          rate(loki_distributor_lines_received_total{%s}[$__rate_interval])
        )
      ||| % [tenantSelector],

  // Measures structured metadata bytes per second being ingested
  structured_metadata_bytes_rate(recording_rule=config.use_recording_rules):
    if recording_rule then
      local tenantSelector =
        selector()
          .tenant()
          .build();
      'loki_tenant:structured_metadata_bytes:rate5m{%s}' % [tenantSelector]
    else
      local tenantSelector =
        selector()
          .distributor()
          .tenant()
          .build();
      |||
        sum(
          rate(loki_distributor_structured_metadata_bytes_received_total{%s}[$__rate_interval])
        )
      ||| % [tenantSelector],

  // Average size of individual log lines including their metadata
  average_log_size(recording_rule=config.use_recording_rules):
    if recording_rule then
      local tenantSelector =
        selector()
          .tenant()
          .build();
      |||
        (
          loki_tenant:bytes:rate5m{%s}
          +
          loki_tenant:structured_metadata_bytes:rate5m{%s}
        )
        /
        loki_tenant:lines:rate5m{%s}
      ||| % std.repeat([tenantSelector], 3)
    else
      local tenantSelector =
        selector()
          .distributor()
          .tenant()
          .build();
      |||
        (
          sum(rate(loki_distributor_bytes_received_total{%s}[$__rate_interval]))
          +
          sum(rate(loki_distributor_structured_metadata_bytes_received_total{%s}[$__rate_interval]))
        )
        /
        sum(
          rate(loki_distributor_lines_received_total{%s}[$__rate_interval])
        )
      ||| % std.repeat([tenantSelector], 3),

  // Measures bytes per second being rejected
  discarded_bytes_rate(recording_rule=config.use_recording_rules):
    if recording_rule then
      local tenantSelector =
        selector()
          .tenant()
          .build();
      'loki_tenant:discarded_bytes:rate5m{%s}' % [tenantSelector]
    else
      local tenantSelector =
        selector()
          .resources(['distributor', 'ingester'])
          .tenant()
          .build();
      |||
        sum(
          rate(loki_discarded_bytes_total{%s}[$__rate_interval])
        )
      ||| % [tenantSelector],

  // Measures log lines per second being rejected
  discarded_lines_rate(recording_rule=config.use_recording_rules):
    if recording_rule then
      local tenantSelector =
        selector()
          .tenant()
          .build();
      'loki_tenant:discarded_lines:rate5m{%s}' % [tenantSelector]
    else
      local tenantSelector =
        selector()
          .resources(['distributor', 'ingester'])
          .tenant()
          .build();
      |||
      sum(
        rate(loki_discarded_lines_total{%s}[$__rate_interval])
      )
    ||| % [tenantSelector],

  // Number of unique log streams currently active
  active_streams(recording_rule=config.use_recording_rules):
    if recording_rule then
      local tenantSelector =
        selector()
          .tenant()
          .build();
      'loki_tenant:active_streams{%s}' % [tenantSelector]
    else
      local ingesterSelector =
        selector()
          .ingester()
          .tenant()
          .build();
      local distributorSelector =
        selector()
          .distributor()
          .tenant()
          .build();
      |||
      sum by (cluster, namespace, tenant) (
        loki_ingester_memory_streams{%s}
      )
      /
      on (namespace, cluster) group_left ()
      max by (namespace, cluster) (
        loki_distributor_replication_factor{%s}
      )
    ||| % [ingesterSelector, distributorSelector],
}
