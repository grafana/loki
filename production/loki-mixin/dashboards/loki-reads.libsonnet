local utils = import 'mixin-utils/utils.libsonnet';

{
  grafanaDashboards+: {
    local dashboards = self,

    local http_routes = 'loki_api_v1_series|api_prom_series|api_prom_query|api_prom_label|api_prom_label_name_values|loki_api_v1_query|loki_api_v1_query_range|loki_api_v1_labels|loki_api_v1_label_name_values',
    local grpc_routes = '/logproto.Querier/Query|/logproto.Querier/Label|/logproto.Querier/Series|/logproto.Querier/QuerySample|/logproto.Querier/GetChunkIDs',

    'loki-reads.json':
      ($.dashboard('Loki / Reads'))
      .addClusterSelectorTemplates()
      .addRow(
        $.row('Frontend (cortex_gw)')
        .addPanel(
          $.panel('QPS') +
          $.qpsPanel('loki_request_duration_seconds_count{%s, route=~"%s"}' % [$.jobMatcher($._config.job_names.gateway), http_routes])
        )
        .addPanel(
          $.panel('Latency') +
          utils.latencyRecordingRulePanel(
            'loki_request_duration_seconds',
            $.jobSelector($._config.job_names.gateway) + [utils.selector.re('route', http_routes)],
            sum_by=['route']
          )
        )
      )
      .addRow(
        $.row('Frontend (query-frontend)')
        .addPanel(
          $.panel('QPS') +
          $.qpsPanel('loki_request_duration_seconds_count{%s, route=~"%s"}' % [$.jobMatcher($._config.job_names.query_frontend), http_routes])
        )
        .addPanel(
          $.panel('Latency') +
          utils.latencyRecordingRulePanel(
            'loki_request_duration_seconds',
            $.jobSelector($._config.job_names.query_frontend) + [utils.selector.re('route', http_routes)],
            sum_by=['route']
          )
        )
      )
      .addRow(
        $.row('Querier')
        .addPanel(
          $.panel('QPS') +
          $.qpsPanel('loki_request_duration_seconds_count{%s, route=~"%s"}' % [$.jobMatcher($._config.job_names.querier), http_routes])
        )
        .addPanel(
          $.panel('Latency') +
          utils.latencyRecordingRulePanel(
            'loki_request_duration_seconds',
            $.jobSelector($._config.job_names.querier) + [utils.selector.re('route', http_routes)],
            sum_by=['route']
          )
        )
      )
      .addRow(
        $.row('Ingester')
        .addPanel(
          $.panel('QPS') +
          $.qpsPanel('loki_request_duration_seconds_count{%s, route=~"%s"}' % [$.jobMatcher($._config.job_names.ingester), grpc_routes])
        )
        .addPanel(
          $.panel('Latency') +
          utils.latencyRecordingRulePanel(
            'loki_request_duration_seconds',
            $.jobSelector($._config.job_names.ingester) + [utils.selector.re('route', grpc_routes)],
            sum_by=['route']
          )
        )
      )
      .addRow(
        $.row('BigTable')
        .addPanel(
          $.panel('QPS') +
          $.qpsPanel('cortex_bigtable_request_duration_seconds_count{%s, operation="/google.bigtable.v2.Bigtable/ReadRows"}' % $.jobMatcher($._config.job_names.querier))
        )
        .addPanel(
          $.panel('Latency') +
          utils.latencyRecordingRulePanel(
            'cortex_bigtable_request_duration_seconds',
            $.jobSelector($._config.job_names.querier) + [utils.selector.eq('operation', '/google.bigtable.v2.Bigtable/ReadRows')]
          )
        )
      )
      .addRow(
        $.row('BoltDB Shipper')
        .addPanel(
          $.panel('QPS') +
          $.qpsPanel('loki_boltdb_shipper_request_duration_seconds_count{%s, operation="QUERY"}' % $.jobMatcher($._config.job_names.index_gateway))
        )
        .addPanel(
          $.panel('Latency') +
          $.latencyPanel('loki_boltdb_shipper_request_duration_seconds', '{%s, operation="QUERY"}' % $.jobMatcher($._config.job_names.index_gateway))
        )
      ),
  },
}
