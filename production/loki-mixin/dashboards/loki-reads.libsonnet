local utils = import 'mixin-utils/utils.libsonnet';

(import 'dashboard-utils.libsonnet') {
  grafanaDashboards+: {
    local dashboards = self,
    local showBigTable = false,

    local http_routes = 'loki_api_v1_series|api_prom_series|api_prom_query|api_prom_label|api_prom_label_name_values|loki_api_v1_query|loki_api_v1_query_range|loki_api_v1_labels|loki_api_v1_label_name_values',
    local grpc_routes = '/logproto.Querier/Query|/logproto.Querier/Label|/logproto.Querier/Series|/logproto.Querier/QuerySample|/logproto.Querier/GetChunkIDs',

    local p99LatencyByPod(metric, selectorStr) =
      $.panel('Per Pod Latency (p99)') +
      {
        targets: [
          {
            expr:
              |||
                histogram_quantile(0.99,
                  sum(
                   rate(%s%s[$__rate_interval])
                   ) by (pod, le)
                 )
              ||| % [metric, selectorStr],
            instant: false,
            legendFormat: '__auto',
            range: true,
            refId: 'A',
          },
        ],
        fieldConfig+: {
          custom+: {
            fillOpacity: 50,
            showPoints: 'never',
            stacking: {
              group: 'A',
              mode: 'normal',
            },
          },
        },
      },

    'loki-reads.json': {
                         local cfg = self,

                         showMultiCluster:: true,
                         clusterLabel:: $._config.per_cluster_label,
                         clusterMatchers::
                           if cfg.showMultiCluster then
                             [utils.selector.re(cfg.clusterLabel, '$cluster')]
                           else
                             [],

                         matchers:: {
                           cortexgateway: [utils.selector.re('job', '($namespace)/cortex-gw(-internal)?')],
                           queryFrontend: [utils.selector.re('job', '($namespace)/%s' % (if $._config.ssd.enabled then '%s-read' % $._config.ssd.pod_prefix_matcher else 'query-frontend'))],
                           querier: [utils.selector.re('job', '($namespace)/%s' % (if $._config.ssd.enabled then '%s-write' % $._config.ssd.pod_prefix_matcher else 'querier'))],
                           ingester: [utils.selector.re('job', '($namespace)/%s' % (if $._config.ssd.enabled then '%s-write' % $._config.ssd.pod_prefix_matcher else 'ingester'))],
                           ingesterZoneAware: [utils.selector.re('job', '($namespace)/%s' % (if $._config.ssd.enabled then '%s-write' % $._config.ssd.pod_prefix_matcher else 'ingester-zone.*'))],
                           querierOrIndexGateway: [utils.selector.re('job', '($namespace)/%s' % (if $._config.ssd.enabled then '%s-read' % $._config.ssd.pod_prefix_matcher else '(querier|index-gateway)'))],
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
                         ingesterZoneSelector:: selector('ingesterZoneAware'),
                         querierOrIndexGatewaySelector:: selector('querierOrIndexGateway'),
                       } +
                       $.dashboard('Loki / Reads', uid='reads')
                       .addCluster()
                       .addNamespace()
                       .addTag()
                       .addRowIf(
                         $._config.internal_components,
                         $.row('Frontend (cortex_gw)')
                         .addPanel(
                           $.panel('QPS') +
                           $.qpsPanel('loki_request_duration_seconds_count{%s route=~"%s"}' % [dashboards['loki-reads.json'].cortexGwSelector, http_routes])
                         )
                         .addPanel(
                           $.panel('Latency') +
                           utils.latencyRecordingRulePanel(
                             'loki_request_duration_seconds',
                             dashboards['loki-reads.json'].clusterMatchers + dashboards['loki-reads.json'].matchers.cortexgateway + [utils.selector.re('route', http_routes)],
                             sum_by=['route']
                           )
                         )
                         .addPanel(
                           p99LatencyByPod(
                             'loki_request_duration_seconds_bucket',
                             $.toPrometheusSelector(
                               dashboards['loki-reads.json'].clusterMatchers +
                               dashboards['loki-reads.json'].matchers.cortexgateway +
                               [utils.selector.re('route', http_routes)]
                             ),
                           )
                         )
                       )
                       .addRow(
                         $.row(if $._config.ssd.enabled then 'Read Path' else 'Frontend (query-frontend)')
                         .addPanel(
                           $.panel('QPS') +
                           $.qpsPanel('loki_request_duration_seconds_count{%s route=~"%s"}' % [dashboards['loki-reads.json'].queryFrontendSelector, http_routes])
                         )
                         .addPanel(
                           $.panel('Latency') +
                           utils.latencyRecordingRulePanel(
                             'loki_request_duration_seconds',
                             dashboards['loki-reads.json'].clusterMatchers + dashboards['loki-reads.json'].matchers.queryFrontend + [utils.selector.re('route', http_routes)],
                             sum_by=['route']
                           )
                         )
                         .addPanel(
                           p99LatencyByPod(
                             'loki_request_duration_seconds_bucket',
                             $.toPrometheusSelector(
                               dashboards['loki-reads.json'].clusterMatchers +
                               dashboards['loki-reads.json'].matchers.queryFrontend +
                               [utils.selector.re('route', http_routes)]
                             ),
                           )
                         )
                       )
                       .addRowIf(
                         !$._config.ssd.enabled,
                         $.row('Querier')
                         .addPanel(
                           $.panel('QPS') +
                           $.qpsPanel('loki_request_duration_seconds_count{%s route=~"%s"}' % [dashboards['loki-reads.json'].querierSelector, http_routes])
                         )
                         .addPanel(
                           $.panel('Latency') +
                           utils.latencyRecordingRulePanel(
                             'loki_request_duration_seconds',
                             dashboards['loki-reads.json'].clusterMatchers + dashboards['loki-reads.json'].matchers.querier + [utils.selector.re('route', http_routes)],
                             sum_by=['route']
                           )
                         )
                         .addPanel(
                           p99LatencyByPod(
                             'loki_request_duration_seconds_bucket',
                             $.toPrometheusSelector(
                               dashboards['loki-reads.json'].clusterMatchers +
                               dashboards['loki-reads.json'].matchers.querier +
                               [utils.selector.re('route', http_routes)]
                             ),
                           )
                         )
                       )
                       .addRowIf(
                         !$._config.ssd.enabled,
                         $.row('Ingester')
                         .addPanel(
                           $.panel('QPS') +
                           $.qpsPanel('loki_request_duration_seconds_count{%s route=~"%s"}' % [dashboards['loki-reads.json'].ingesterSelector, grpc_routes])
                         )
                         .addPanel(
                           $.panel('Latency') +
                           utils.latencyRecordingRulePanel(
                             'loki_request_duration_seconds',
                             dashboards['loki-reads.json'].clusterMatchers + dashboards['loki-reads.json'].matchers.ingester + [utils.selector.re('route', grpc_routes)],
                             sum_by=['route']
                           )
                         )
                         .addPanel(
                           p99LatencyByPod(
                             'loki_request_duration_seconds_bucket',
                             $.toPrometheusSelector(
                               dashboards['loki-reads.json'].clusterMatchers +
                               dashboards['loki-reads.json'].matchers.ingester +
                               [utils.selector.re('route', grpc_routes)]
                             ),
                           )
                         )
                       )
                       // todo: add row iff multi zone ingesters are enabled
                       .addRowIf(
                         !$._config.ssd.enabled,
                         $.row('Ingester - Zone Aware')
                         .addPanel(
                           $.panel('QPS') +
                           $.qpsPanel('loki_request_duration_seconds_count{%s route=~"%s"}' % [dashboards['loki-reads.json'].ingesterZoneSelector, grpc_routes])
                         )
                         .addPanel(
                           $.panel('Latency') +
                           utils.latencyRecordingRulePanel(
                             'loki_request_duration_seconds',
                             dashboards['loki-reads.json'].clusterMatchers + dashboards['loki-reads.json'].matchers.ingesterZoneAware + [utils.selector.re('route', grpc_routes)],
                             sum_by=['route']
                           )
                         )
                         .addPanel(
                           p99LatencyByPod(
                             'loki_request_duration_seconds_bucket',
                             $.toPrometheusSelector(
                               dashboards['loki-reads.json'].clusterMatchers +
                               dashboards['loki-reads.json'].matchers.ingesterZoneAware +
                               [utils.selector.re('route', grpc_routes)]
                             ),
                           )
                         )
                       )
                       .addRowIf(
                         !$._config.ssd.enabled,
                         $.row('Index')
                         .addPanel(
                           $.panel('QPS') +
                           $.qpsPanel('loki_index_request_duration_seconds_count{%s operation!="index_chunk"}' % dashboards['loki-reads.json'].querierSelector)
                         )
                         .addPanel(
                           $.panel('Latency') +
                           $.latencyPanel('loki_index_request_duration_seconds', '{%s operation!="index_chunk"}' % dashboards['loki-reads.json'].querierSelector)
                         )
                         .addPanel(
                           p99LatencyByPod(
                             'loki_index_request_duration_seconds_bucket',
                             '{%s operation!="index_chunk"}' % dashboards['loki-reads.json'].querierSelector
                           )
                         )
                       )
                       .addRowIf(
                         showBigTable,
                         $.row('BigTable')
                         .addPanel(
                           $.panel('QPS') +
                           $.qpsPanel('loki_bigtable_request_duration_seconds_count{%s operation="/google.bigtable.v2.Bigtable/ReadRows"}' % dashboards['loki-reads.json'].querierSelector)
                         )
                         .addPanel(
                           $.panel('Latency') +
                           utils.latencyRecordingRulePanel(
                             'loki_bigtable_request_duration_seconds',
                             dashboards['loki-reads.json'].clusterMatchers + dashboards['loki-reads.json'].matchers.querier + [utils.selector.eq('operation', '/google.bigtable.v2.Bigtable/ReadRows')]
                           )
                         )
                       )
                       .addRow(
                         $.row('BoltDB Shipper')
                         .addPanel(
                           $.panel('QPS') +
                           $.qpsPanel('loki_boltdb_shipper_request_duration_seconds_count{%s operation="Shipper.Query"}' % dashboards['loki-reads.json'].querierOrIndexGatewaySelector)
                         )
                         .addPanel(
                           $.panel('Latency') +
                           $.latencyPanel('loki_boltdb_shipper_request_duration_seconds', '{%s operation="Shipper.Query"}' % dashboards['loki-reads.json'].querierOrIndexGatewaySelector)
                         )
                         .addPanel(
                           p99LatencyByPod(
                             'loki_boltdb_shipper_request_duration_seconds_bucket',
                             '{%s operation="Shipper.Query"}' % dashboards['loki-reads.json'].querierOrIndexGatewaySelector
                           )
                         )
                       ),
  },
}
