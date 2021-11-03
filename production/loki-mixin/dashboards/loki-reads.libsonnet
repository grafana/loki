local utils = import 'mixin-utils/utils.libsonnet';

(import 'dashboard-utils.libsonnet') {
  grafanaDashboards+: {
    local dashboards = self,

    local http_routes = 'loki_api_v1_series|api_prom_series|api_prom_query|api_prom_label|api_prom_label_name_values|loki_api_v1_query|loki_api_v1_query_range|loki_api_v1_labels|loki_api_v1_label_name_values',
    local grpc_routes = '/logproto.Querier/Query|/logproto.Querier/Label|/logproto.Querier/Series|/logproto.Querier/QuerySample|/logproto.Querier/GetChunkIDs',

    'loki-reads.json': {
      local cfg = self,

      showMultiCluster:: true,
      clusterLabel:: 'cluster',
      clusterMatchers::
        if cfg.showMultiCluster then
          [utils.selector.re(cfg.clusterLabel, '$cluster')]
        else
          [],

      matchers:: {
        cortexgateway: [utils.selector.re('job', '($namespace)/cortex-gw')],
        queryFrontend: [utils.selector.re('job', '($namespace)/query-frontend')],
        querier: [utils.selector.re('job', '($namespace)/querier')],
        ingester: [utils.selector.re('job', '($namespace)/ingester')],
        querierOrIndexGateway: [utils.selector.re('job', '($namespace)/(querier|index-gateway)')],
      },

      local selector(matcherId) =
        local ms = (cfg.clusterMatchers + cfg.matchers[matcherId]);
        if std.length(ms) > 0 then
          std.join(',', ['%(label)s%(op)s"%(value)s"' % matcher for matcher in ms]) + ','
        else '',

      cortexGwSelector:: selector('cortexgateway'),
      queryFrontendSelector:: selector('queryFrontend'),
      querierSelector:: selector('querier'),
      ingesterSelector:: selector('ingester'),
      querierOrIndexGatewaySelector:: selector('querierOrIndexGateway'),
    } +
    $.dashboard('Loki / Reads')
    .addClusterSelectorTemplates(false)
    .addRow(
      $.row('Frontend (cortex_gw)')
      .addPanel(
        $.panel('QPS') +
        $.qpsPanel('loki_request_duration_seconds_count{%s route=~"%s"}' % [dashboards['loki-reads.json'].cortexGwSelector, http_routes])
      )
      .addPanel(
        $.panel('Latency') +
        utils.latencyRecordingRulePanel(
          'loki_request_duration_seconds',
          dashboards['loki-reads.json'].matchers.cortexgateway + [utils.selector.re('route', http_routes)],
          extra_selectors=dashboards['loki-reads.json'].clusterMatchers,
          sum_by=['route']
        )
      )
    )
    .addRow(
      $.row('Frontend (query-frontend)')
      .addPanel(
        $.panel('QPS') +
        $.qpsPanel('loki_request_duration_seconds_count{%s route=~"%s"}' % [dashboards['loki-reads.json'].queryFrontendSelector, http_routes])
      )
      .addPanel(
        $.panel('Latency') +
        utils.latencyRecordingRulePanel(
          'loki_request_duration_seconds',
          dashboards['loki-reads.json'].matchers.queryFrontend + [utils.selector.re('route', http_routes)],
          extra_selectors=dashboards['loki-reads.json'].clusterMatchers,
          sum_by=['route']
        )
      )
    )
    .addRow(
      $.row('Querier')
      .addPanel(
        $.panel('QPS') +
        $.qpsPanel('loki_request_duration_seconds_count{%s route=~"%s"}' % [dashboards['loki-reads.json'].querierSelector, http_routes])
      )
      .addPanel(
        $.panel('Latency') +
        utils.latencyRecordingRulePanel(
          'loki_request_duration_seconds',
          dashboards['loki-reads.json'].matchers.querier + [utils.selector.re('route', http_routes)],
          extra_selectors=dashboards['loki-reads.json'].clusterMatchers,
          sum_by=['route']
        )
      )
    )
    .addRow(
      $.row('Ingester')
      .addPanel(
        $.panel('QPS') +
        $.qpsPanel('loki_request_duration_seconds_count{%s route=~"%s"}' % [dashboards['loki-reads.json'].ingesterSelector, grpc_routes])
      )
      .addPanel(
        $.panel('Latency') +
        utils.latencyRecordingRulePanel(
          'loki_request_duration_seconds',
          dashboards['loki-reads.json'].matchers.ingester + [utils.selector.re('route', grpc_routes)],
          extra_selectors=dashboards['loki-reads.json'].clusterMatchers,
          sum_by=['route']
        )
      )
    )
    .addRow(
      $.row('BigTable')
      .addPanel(
        $.panel('QPS') +
        $.qpsPanel('cortex_bigtable_request_duration_seconds_count{%s operation="/google.bigtable.v2.Bigtable/ReadRows"}' % dashboards['loki-reads.json'].querierSelector)
      )
      .addPanel(
        $.panel('Latency') +
        utils.latencyRecordingRulePanel(
          'cortex_bigtable_request_duration_seconds',
          dashboards['loki-reads.json'].matchers.querier + [utils.selector.eq('operation', '/google.bigtable.v2.Bigtable/ReadRows')]
        )
      )
    )
    .addRow(
      $.row('BoltDB Shipper')
      .addPanel(
        $.panel('QPS') +
        $.qpsPanel('loki_boltdb_shipper_request_duration_seconds_count{%s operation="QUERY"}' % dashboards['loki-reads.json'].querierOrIndexGatewaySelector)
      )
      .addPanel(
        $.panel('Latency') +
        $.latencyPanel('loki_boltdb_shipper_request_duration_seconds', '{%s operation="QUERY"}' % dashboards['loki-reads.json'].querierOrIndexGatewaySelector)
      )
    )
  },
}
