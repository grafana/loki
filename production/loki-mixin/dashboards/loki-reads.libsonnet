local utils = import 'mixin-utils/utils.libsonnet';

(import 'dashboard-utils.libsonnet') {
  grafanaDashboards+: {
    local dashboards = self,
    local showBigTable = false,

    local http_routes = 'loki_api_v1_series|api_prom_series|api_prom_query|api_prom_label|api_prom_label_name_values|loki_api_v1_query|loki_api_v1_query_range|loki_api_v1_labels|loki_api_v1_label_name_values',
    local grpc_routes = '/logproto.Querier/Query|/logproto.Querier/Label|/logproto.Querier/Series|/logproto.Querier/QuerySample|/logproto.Querier/GetChunkIDs|/logproto.BloomGateway/FilterChunkRefs',

    local latencyPanelWithExtraGrouping(metricName, selector, multiplier='1e3', extra_grouping='') = {
      nullPointMode: 'null as zero',
      targets: [
        {
          expr: 'histogram_quantile(0.99, sum(rate(%s_bucket%s[$__rate_interval])) by (le,%s)) * %s' % [metricName, selector, extra_grouping, multiplier],
          format: 'time_series',
          intervalFactor: 2,
          refId: 'A',
          step: 10,
          interval: '1m',
          legendFormat: '__auto',
        },
      ],
    },

    local p99LatencyByPod(metric, selectorStr) =
      $.newQueryPanel('Per Pod Latency (p99)', 'ms') +
      latencyPanelWithExtraGrouping(metric, selectorStr, '1e3', 'pod'),

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
                           queryFrontend: if $._config.meta_monitoring.enabled
                              then [utils.selector.re('job', '($namespace)/(query-frontend|%s-read|loki-single-binary' % $._config.ssd.pod_prefix_matcher)]
                              else [utils.selector.re('job', '($namespace)/%s' % (if $._config.ssd.enabled then '%s-read' % $._config.ssd.pod_prefix_matcher else 'query-frontend'))],
                           querier: if $._config.meta_monitoring.enabled
                              then [utils.selector.re('job', '($namespace)/(querier|%s-read|loki-single-binary' % $._config.ssd.pod_prefix_matcher)]
                              else [utils.selector.re('job', '($namespace)/%s' % (if $._config.ssd.enabled then '%s-read' % $._config.ssd.pod_prefix_matcher else 'querier'))],
                           ingester: if $._config.meta_monitoring.enabled
                              then [utils.selector.re('job', '($namespace)/(ingester|%s-write|loki-single-binary' % $._config.ssd.pod_prefix_matcher)]
                              else [utils.selector.re('job', '($namespace)/%s' % (if $._config.ssd.enabled then '%s-write' % $._config.ssd.pod_prefix_matcher else 'ingester'))],
                           ingesterZoneAware: if $._config.meta_monitoring.enabled
                              then [utils.selector.re('job', '($namespace)/(ingester-zone-.*|%s-write|loki-single-binary' % $._config.ssd.pod_prefix_matcher)]
                              else [utils.selector.re('job', '($namespace)/%s' % (if $._config.ssd.enabled then '%s-write' % $._config.ssd.pod_prefix_matcher else 'ingester-zone.*'))],
                           querierOrIndexGateway: if $._config.meta_monitoring.enabled
                              then [utils.selector.re('job', '($namespace)/(querier|index-gateway|%s-read|loki-single-binary' % $._config.ssd.pod_prefix_matcher)]
                              else [utils.selector.re('job', '($namespace)/%s' % (if $._config.ssd.enabled then '%s-read' % $._config.ssd.pod_prefix_matcher else '(querier|index-gateway)'))],
                           bloomGateway: if $._config.meta_monitoring.enabled
                              then [utils.selector.re('job', '($namespace)/(bloom-gateway|%s-backend|loki-single-binary' % $._config.ssd.pod_prefix_matcher)]
                              else [utils.selector.re('job', '($namespace)/%s' % (if $._config.ssd.enabled then '%s-backend' % $._config.ssd.pod_prefix_matcher else 'bloom-gateway'))],
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
                         bloomGatewaySelector:: selector('bloomGateway'),
                       } +
                       $.dashboard('Loki / Reads', uid='reads')
                       .addCluster()
                       .addNamespace()
                       .addTag()
                       .addRowIf(
                         $._config.internal_components,
                         $.row('Frontend (cortex_gw)')
                         .addPanel(
                           $.newQueryPanel('QPS') +
                           $.newQpsPanel('loki_request_duration_seconds_count{%s route=~"%s"}' % [dashboards['loki-reads.json'].cortexGwSelector, http_routes])
                         )
                         .addPanel(
                           $.newQueryPanel('Latency', 'ms') +
                           utils.latencyRecordingRulePanel(
                             'loki_request_duration_seconds',
                             dashboards['loki-reads.json'].clusterMatchers + dashboards['loki-reads.json'].matchers.cortexgateway + [utils.selector.re('route', http_routes)],
                             sum_by=['route']
                           )
                         )
                         .addPanel(
                           p99LatencyByPod(
                             'loki_request_duration_seconds',
                             $.toPrometheusSelector(
                               dashboards['loki-reads.json'].clusterMatchers + dashboards['loki-reads.json'].matchers.cortexgateway + [utils.selector.re('route', http_routes)]
                             ),
                           )
                         )
                       )
                       .addRow(
                         $.row(if $._config.ssd.enabled then 'Read Path' else 'Frontend (query-frontend)')
                         .addPanel(
                           $.newQueryPanel('QPS') +
                           $.newQpsPanel('loki_request_duration_seconds_count{%s route=~"%s"}' % [dashboards['loki-reads.json'].queryFrontendSelector, http_routes])
                         )
                         .addPanel(
                           $.newQueryPanel('Latency', 'ms') +
                           utils.latencyRecordingRulePanel(
                             'loki_request_duration_seconds',
                             dashboards['loki-reads.json'].clusterMatchers + dashboards['loki-reads.json'].matchers.queryFrontend + [utils.selector.re('route', http_routes)],
                             sum_by=['route']
                           )
                         )
                         .addPanel(
                           p99LatencyByPod(
                             'loki_request_duration_seconds',
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
                           $.newQueryPanel('QPS') +
                           $.newQpsPanel('loki_request_duration_seconds_count{%s route=~"%s"}' % [dashboards['loki-reads.json'].querierSelector, http_routes])
                         )
                         .addPanel(
                           $.newQueryPanel('Latency', 'ms') +
                           utils.latencyRecordingRulePanel(
                             'loki_request_duration_seconds',
                             dashboards['loki-reads.json'].clusterMatchers + dashboards['loki-reads.json'].matchers.querier + [utils.selector.re('route', http_routes)],
                             sum_by=['route']
                           )
                         )
                         .addPanel(
                           p99LatencyByPod(
                             'loki_request_duration_seconds',
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
                           $.newQueryPanel('QPS') +
                           $.newQpsPanel('loki_request_duration_seconds_count{%s route=~"%s"}' % [dashboards['loki-reads.json'].ingesterSelector, grpc_routes])
                         )
                         .addPanel(
                           $.newQueryPanel('Latency', 'ms') +
                           utils.latencyRecordingRulePanel(
                             'loki_request_duration_seconds',
                             dashboards['loki-reads.json'].clusterMatchers + dashboards['loki-reads.json'].matchers.ingester + [utils.selector.re('route', grpc_routes)],
                             sum_by=['route']
                           )
                         )
                         .addPanel(
                           p99LatencyByPod(
                             'loki_request_duration_seconds',
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
                           $.newQueryPanel('QPS') +
                           $.newQpsPanel('loki_request_duration_seconds_count{%s route=~"%s"}' % [dashboards['loki-reads.json'].ingesterZoneSelector, grpc_routes])
                         )
                         .addPanel(
                           $.newQueryPanel('Latency', 'ms') +
                           utils.latencyRecordingRulePanel(
                             'loki_request_duration_seconds',
                             dashboards['loki-reads.json'].clusterMatchers + dashboards['loki-reads.json'].matchers.ingesterZoneAware + [utils.selector.re('route', grpc_routes)],
                             sum_by=['route']
                           )
                         )
                         .addPanel(
                           p99LatencyByPod(
                             'loki_request_duration_seconds',
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
                           $.newQueryPanel('QPS') +
                           $.newQpsPanel('loki_index_request_duration_seconds_count{%s operation!="index_chunk"}' % dashboards['loki-reads.json'].querierSelector)
                         )
                         .addPanel(
                           $.newQueryPanel('Latency', 'ms') +
                           $.latencyPanel('loki_index_request_duration_seconds', '{%s operation!="index_chunk"}' % dashboards['loki-reads.json'].querierSelector)
                         )
                         .addPanel(
                           p99LatencyByPod(
                             'loki_index_request_duration_seconds',
                             '{%s operation!="index_chunk"}' % dashboards['loki-reads.json'].querierSelector
                           )
                         )
                       )
                       .addRowIf(
                         !$._config.ssd.enabled,
                         $.row('Bloom Gateway')
                         .addPanel(
                           $.newQueryPanel('QPS') +
                           $.newQpsPanel('loki_request_duration_seconds_count{%s route=~"%s"}' % [dashboards['loki-reads.json'].bloomGatewaySelector, grpc_routes])
                         )
                         .addPanel(
                           $.newQueryPanel('Latency', 'ms') +
                           utils.latencyRecordingRulePanel(
                             'loki_request_duration_seconds',
                             dashboards['loki-reads.json'].clusterMatchers + dashboards['loki-reads.json'].matchers.bloomGateway + [utils.selector.re('route', grpc_routes)],
                             sum_by=['route']
                           )
                         )
                         .addPanel(
                           p99LatencyByPod(
                             'loki_request_duration_seconds',
                             $.toPrometheusSelector(
                               dashboards['loki-reads.json'].clusterMatchers +
                               dashboards['loki-reads.json'].matchers.bloomGateway +
                               [utils.selector.re('route', grpc_routes)]
                             ),
                           )
                         )
                       )
                       .addRowIf(
                         showBigTable,
                         $.row('BigTable')
                         .addPanel(
                           $.newQueryPanel('QPS') +
                           $.newQpsPanel('loki_bigtable_request_duration_seconds_count{%s operation="/google.bigtable.v2.Bigtable/ReadRows"}' % dashboards['loki-reads.json'].querierSelector)
                         )
                         .addPanel(
                           $.newQueryPanel('Latency', 'ms') +
                           utils.latencyRecordingRulePanel(
                             'loki_bigtable_request_duration_seconds',
                             dashboards['loki-reads.json'].clusterMatchers + dashboards['loki-reads.json'].matchers.querier + [utils.selector.eq('operation', '/google.bigtable.v2.Bigtable/ReadRows')]
                           )
                         )
                       )
                       .addRow(
                         $.row('BoltDB Shipper')
                         .addPanel(
                           $.newQueryPanel('QPS') +
                           $.newQpsPanel('loki_boltdb_shipper_request_duration_seconds_count{%s operation="Shipper.Query"}' % dashboards['loki-reads.json'].querierOrIndexGatewaySelector)
                         )
                         .addPanel(
                           $.newQueryPanel('Latency', 'ms') +
                           $.latencyPanel('loki_boltdb_shipper_request_duration_seconds', '{%s operation="Shipper.Query"}' % dashboards['loki-reads.json'].querierOrIndexGatewaySelector)
                         )
                         .addPanel(
                           p99LatencyByPod(
                             'loki_boltdb_shipper_request_duration_seconds',
                             '{%s operation="Shipper.Query"}' % dashboards['loki-reads.json'].querierOrIndexGatewaySelector
                           )
                         )
                       ),
  },
}
