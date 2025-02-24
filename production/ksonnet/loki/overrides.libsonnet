local common = import 'common.libsonnet';
local k = import 'ksonnet-util/kausal.libsonnet';
{
  _config+: {
    overrides: {
      // insert tenant overrides here. see https://grafana.com/docs/loki/latest/configuration/#limits_config
      //
      //   'tenant_x': {
      //     ingestion_rate_strategy: 'global',
      //     ingestion_rate_mb: 12,
      //     ingestion_burst_size_mb: 20,
      //     max_line_size: 2048,
      //     split_queries_by_interval: '30m',
      //     max_concurrent_tail_requests: 10,
      //     max_query_parallelism: 32,
      //   },
    },

    runtimeConfigs: {
      // insert runtime configs here. see pkg/runtime/config.go
      //
      // tenant_x: {
      //   log_stream_creation: true,
      //   log_push_request: true,
      //   log_push_request_streams: true,
      // },
    },
  },
  local configMap = k.core.v1.configMap,

  overrides_config:
    configMap.new($._config.overrides_configmap_name) +
    configMap.withData({
      'overrides.yaml': k.util.manifestYaml(
        {
          overrides: $._config.overrides,
          configs: $._config.runtimeConfigs,
        }
        + (if std.length($._config.multi_kv_config) > 0 then { multi_kv_config: $._config.multi_kv_config } else {}),
      ),
    }),

  local checkRetentionStreams(retentionStreams, maxQueryLookback) =
    std.foldl(
      function(acc, retentionStream) acc && common.parseDuration(retentionStream.period) <= common.parseDuration(maxQueryLookback),
      retentionStreams,
      true
    ),

  isLookbackLongerThanRetention(tenantCfg)::
    local retentionPeriod = tenantCfg.retention_period;
    local lookback = tenantCfg.max_query_lookback;
    if std.objectHas(tenantCfg, 'max_query_lookback') &&
       std.objectHas(tenantCfg, 'retention_period') then
      common.parseDuration(lookback) >= common.parseDuration(retentionPeriod)
    else
      true,

  isLookbackLongerThanStreamRetention(tenantCfg)::
    local retentionStream = tenantCfg.retention_stream;
    local lookback = tenantCfg.max_query_lookback;
    if std.objectHas(tenantCfg, 'max_query_lookback') &&
       std.objectHas(tenantCfg, 'retention_stream') then
      checkRetentionStreams(retentionStream, lookback)
    else
      true,

  checkTenantRetention(tenant)::
    local tenantCfg = $._config.overrides[tenant];
    if $.isLookbackLongerThanRetention(tenantCfg) &&
       $.isLookbackLongerThanStreamRetention(tenantCfg) then
      true
    else
      false,

  local tenants = std.objectFields($._config.overrides),

  local validRetentionsCheck = std.foldl(
    function(acc, tenant) if !$.checkTenantRetention(tenant) then { valid: false, failedTenant: [tenant] + acc.failedTenant } else acc,
    tenants,
    { valid: true, failedTenant: [] }
  ),

  assert validRetentionsCheck.valid : 'retention period longer than max_query_lookback for tenants %s' % std.join(', ', validRetentionsCheck.failedTenant),
}
