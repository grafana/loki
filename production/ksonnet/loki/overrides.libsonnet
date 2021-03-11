{
  _config+: {
    overrides: {
      // insert tenant overrides here. see https://github.com/grafana/loki/tree/master/docs/configuration#limits_config
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
  },
  local configMap = $.core.v1.configMap,

  overrides_config:
    configMap.new('overrides') +
    configMap.withData({
      'overrides.yaml': $.util.manifestYaml(
        {
          overrides: $._config.overrides,
        }
      ),
    }),
}
