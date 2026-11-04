local utils = import 'mixin-utils/utils.libsonnet';

(import 'dashboard-utils.libsonnet') {
  grafanaDashboards+: {
    local dashboards = self,

    // Available HTTP routes can be collected with the following instant query:
    // count by (route) (loki_request_duration_seconds_count{route!~"/.*"})
    local http_routes = '(%s)' % std.join(
      '|', [
        'api_prom_rules',
        'api_prom_rules_namespace_groupname',
        'api_v1_rules',
        'loki_api_v1_delete',
        'loki_api_v1_detected_labels',
        'loki_api_v1_index_stats',
        'loki_api_v1_index_volume',
        'loki_api_v1_index_volume_range',
        'loki_api_v1_label_name_values',
        'loki_api_v1_label_values',
        'loki_api_v1_labels',
        'loki_api_v1_patterns',
        'loki_api_v1_query',
        'loki_api_v1_query_range',
        'loki_api_v1_series',
        'prometheus_api_v1_rules',
      ]
    ),

    // Available GRPC routes can be collected with the following instant query:
    // count by (route) (loki_request_duration_seconds_count{route=~"/.*"})
    local grpc_routes = '(%s)' % std.join(
      '|', [
        '/base.Ruler/Rules',
        '/indexgatewaypb.IndexGateway/GetChunkRef',
        '/indexgatewaypb.IndexGateway/GetSeries',
        '/indexgatewaypb.IndexGateway/GetShards',
        '/indexgatewaypb.IndexGateway/GetStats',
        '/indexgatewaypb.IndexGateway/GetVolume',
        '/indexgatewaypb.IndexGateway/LabelNamesForMetricName',
        '/indexgatewaypb.IndexGateway/LabelValuesForMetricName',
        '/indexgatewaypb.IndexGateway/QueryIndex',
        '/logproto.BloomGateway/FilterChunkRefs',
        '/logproto.Pattern/Query',
        '/logproto.Querier/GetChunkIDs',
        '/logproto.Querier/GetDetectedLabels',
        '/logproto.Querier/GetStats',
        '/logproto.Querier/GetVolume',
        '/logproto.Querier/Label',
        '/logproto.Querier/Query',
        '/logproto.Querier/QuerySample',
        '/logproto.Querier/Series',
        '/logproto.StreamData/GetStreamRates',
      ]
    ),

    local querierSel = dashboards['loki-reads.json'].querierSelector,

    local regexColorOverride(regex, color) = $.colorOverride(regex, color) + {
      matcher: { id: 'byRegexp', options: regex },
    },

    local withDescription(text) = { description: text },

    local withTooltipSortDesc = {
      options+: {
        tooltip+: { mode: 'multi', sort: 'desc' },
      },
    },

    // Hides series whose values are never above zero in the selected time range.
    local withZeroSeriesHidden = {
      fieldConfig+: {
        overrides+: [
          {
            matcher: { id: 'byValue', options: { reducer: 'max', op: 'lte', value: 0 } },
            properties: [
              { id: 'custom.hideFrom', value: { legend: true, tooltip: true, viz: true } },
            ],
          },
        ],
      },
    },

    // p99 of a histogram. "> 0" drops the NaN points of series that had no
    // observations, so idle series don't show up at all.
    local p99Query(metric, by) =
      'histogram_quantile(0.99, sum by (%s) (rate(%s_bucket{%s}[$__rate_interval]))) > 0' % [std.join(', ', ['le'] + by), metric, querierSel],

    local congestionControlNoValue = 'Congestion control disabled',

    // Extra panels for the Querier row, three per line: object storage
    // traffic, then HTTP errors and congestion control.
    local querierObjectStoragePanels = [
      $.newQueryPanel('Object storage operations', 'reqps') +
      $.queryPanel(
        'sum by (operation) (rate(loki_objstore_bucket_operations_total{%s}[$__rate_interval]))' % querierSel,
        '{{operation}}'
      ) +
      withZeroSeriesHidden +
      withTooltipSortDesc +
      withDescription('Object storage operations issued by queriers, by operation. Operations with no requests in the selected time range are hidden. Only available with the Thanos object storage client (use_thanos_objstore: true).'),

      $.newQueryPanel('Object storage latency (p99)', 's') +
      $.queryPanel(p99Query('loki_objstore_bucket_operation_duration_seconds', ['operation']), '{{operation}}') +
      withTooltipSortDesc +
      withDescription('p99 latency of a single object storage operation as seen by the client, by operation, including any retries the client does internally.'),

      $.newQueryPanel('Chunk download latency (per query)', 's') +
      $.queryPanel(
        [
          p99Query('loki_logql_querystats_chunk_download_latency_seconds', []),
          'histogram_quantile(0.50, sum by (le) (rate(loki_logql_querystats_chunk_download_latency_seconds_bucket{%s}[$__rate_interval]))) > 0' % querierSel,
        ],
        ['p99', 'p50']
      ) +
      withTooltipSortDesc +
      withDescription('Total time a (sub)query spent downloading chunks, across all of its chunk fetches. This is not the latency of a single GET: see "Object storage latency (p99)" for that.'),

      $.newQueryPanel('Object storage HTTP errors', 'reqps') +
      $.queryPanel(
        'sum by (status_code) (rate(loki_objstore_bucket_transport_requests_total{%s status_code!~"2.."}[$__rate_interval]))' % querierSel,
        '{{status_code}}'
      ) +
      withZeroSeriesHidden +
      withTooltipSortDesc +
      withDescription('HTTP requests to object storage that got a non-2xx response, by status code. Status codes with no requests in the selected time range are hidden. Every HTTP attempt is counted, including retries done inside the client. 429 and 503 (orange) are throttling, for example S3 SlowDown; other 5xx (red) are server errors; 404 (grey) is object not found. Requests that failed without an HTTP response are not counted here.') +
      {
        fieldConfig+: {
          overrides+: [
            regexColorOverride('^5\\d\\d$', '#E24D42'),
            regexColorOverride('^(429|503)$', '#EF843C'),
            regexColorOverride('^404$', '#A9A9A9'),
          ],
        },
      },

      $.newQueryPanel('Retries', 'reqps') +
      $.queryPanel(
        [
          'sum(rate(loki_store_congestion_control_retries_total{%s}[$__rate_interval]))' % querierSel,
          'sum(rate(loki_store_congestion_control_retries_exceeded_total{%s}[$__rate_interval]))' % querierSel,
          'sum(rate(loki_store_congestion_control_non_retryable_errors_total{%s}[$__rate_interval]))' % querierSel,
        ],
        ['retries', 'GETs failed after retries', 'non-retryable errors']
      ) +
      $.withNoValue(congestionControlNoValue) +
      withTooltipSortDesc +
      withDescription('Retries done by congestion control. These cover chunk GETs only. "GETs failed after retries" counts GETs that still failed after the last allowed retry; the error is returned to the query. Without congestion control the object storage client retries internally, and those retries are not counted by any metric.'),

      $.newQueryPanel('Rate-limit wait per attempt', 's') +
      $.queryPanel(
        |||
          sum(rate(loki_store_congestion_control_backoff_seconds_total{%s}[$__rate_interval]))
          /
          sum(rate(loki_store_congestion_control_requests_total{%s}[$__rate_interval]))
        ||| % [querierSel, querierSel],
        'average'
      ) +
      $.withNoValue(congestionControlNoValue) +
      withTooltipSortDesc +
      withDescription('Average time a chunk GET attempt waited for the AIMD rate limiter before it was sent, across all queriers. This is not a delay between retries: retries are sent as soon as the rate limiter allows.'),
    ],

    'loki-reads.json': {
                         local cfg = self,

                         showMultiCluster:: true,
                         clusterMatchers::
                           if cfg.showMultiCluster then
                             [utils.selector.re($._config.per_cluster_label, '$cluster')]
                           else
                             [],

                         matchers:: {
                           cortexgateway: [utils.selector.re('job', '($namespace)/cortex-gw(-internal)?')],
                           queryFrontend: if $._config.meta_monitoring.enabled
                           then [utils.selector.re('job', '($namespace)/(query-frontend|loki-single-binary)')]
                           else [utils.selector.re('job', '($namespace)/query-frontend')],
                           querier: if $._config.meta_monitoring.enabled
                           then [utils.selector.re('job', '($namespace)/(querier|loki-single-binary)')]
                           else [utils.selector.re('job', '($namespace)/querier')],
                           ingester: if $._config.meta_monitoring.enabled
                           then [utils.selector.re('job', '($namespace)/(partition-ingester.*|ingester.*|loki-single-binary)')]
                           else [utils.selector.re('job', '($namespace)/(ingester.*|partition-ingester.*)')],
                           ingesterZoneAware: if $._config.meta_monitoring.enabled
                           then [utils.selector.re('job', '($namespace)/(partition-ingester-.*|ingester-zone-.*|loki-single-binary)')]
                           else [utils.selector.re('job', '($namespace)/(partition-ingester-.*|ingester-zone.*)')],
                           querierOrIndexGateway: if $._config.meta_monitoring.enabled
                           then [utils.selector.re('job', '($namespace)/(querier|index-gateway|loki-single-binary)')]
                           else [utils.selector.re('job', '($namespace)/(querier|index-gateway)')],
                           indexGateway: if $._config.meta_monitoring.enabled
                           then [utils.selector.re('job', '($namespace)/(index-gateway|loki-single-binary)')]
                           else [utils.selector.re('job', '($namespace)/index-gateway')],
                           bloomGateway: if $._config.meta_monitoring.enabled
                           then [utils.selector.re('job', '($namespace)/(bloom-gateway|loki-single-binary)')]
                           else [utils.selector.re('job', '($namespace)/bloom-gateway')],
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
                         indexGatewaySelector:: selector('indexGateway'),
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
                           $.p99LatencyByPod(
                             'loki_request_duration_seconds',
                             $.toPrometheusSelector(
                               dashboards['loki-reads.json'].clusterMatchers + dashboards['loki-reads.json'].matchers.cortexgateway + [utils.selector.re('route', http_routes)]
                             ),
                           )
                         )
                       )
                       .addRow(
                         $.row('Query Frontend')
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
                           $.p99LatencyByPod(
                             'loki_request_duration_seconds',
                             $.toPrometheusSelector(
                               dashboards['loki-reads.json'].clusterMatchers +
                               dashboards['loki-reads.json'].matchers.queryFrontend +
                               [utils.selector.re('route', http_routes)]
                             ),
                           )
                         )
                       )
                       .addRow(
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
                           $.p99LatencyByPod(
                             'loki_request_duration_seconds',
                             $.toPrometheusSelector(
                               dashboards['loki-reads.json'].clusterMatchers +
                               dashboards['loki-reads.json'].matchers.querier +
                               [utils.selector.re('route', http_routes)]
                             ),
                           )
                         ) + {
                           // row() spreads its panels across one line; these wrap onto further lines.
                           panels+: [p { span: 4 } for p in querierObjectStoragePanels],
                         }
                       )
                       .addRow(
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
                           $.p99LatencyByPod(
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
                       .addRow(
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
                           $.p99LatencyByPod(
                             'loki_request_duration_seconds',
                             $.toPrometheusSelector(
                               dashboards['loki-reads.json'].clusterMatchers +
                               dashboards['loki-reads.json'].matchers.ingesterZoneAware +
                               [utils.selector.re('route', grpc_routes)]
                             ),
                           )
                         )
                       )
                       .addRow(
                         $.row('Index Gateway')
                         .addPanel(
                           $.newQueryPanel('QPS') +
                           $.newQpsPanel('loki_request_duration_seconds_count{%s route=~"%s"}' % [dashboards['loki-reads.json'].indexGatewaySelector, grpc_routes])
                         )
                         .addPanel(
                           $.newQueryPanel('Latency', 'ms') +
                           utils.latencyRecordingRulePanel(
                             'loki_request_duration_seconds',
                             dashboards['loki-reads.json'].clusterMatchers + dashboards['loki-reads.json'].matchers.indexGateway + [utils.selector.re('route', grpc_routes)],
                             sum_by=['route']
                           )
                         )
                         .addPanel(
                           $.p99LatencyByPod(
                             'loki_request_duration_seconds',
                             $.toPrometheusSelector(
                               dashboards['loki-reads.json'].clusterMatchers +
                               dashboards['loki-reads.json'].matchers.indexGateway +
                               [utils.selector.re('route', grpc_routes)]
                             ),
                           )
                         )
                       )
                       .addRow(
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
                           $.p99LatencyByPod(
                             'loki_request_duration_seconds',
                             $.toPrometheusSelector(
                               dashboards['loki-reads.json'].clusterMatchers +
                               dashboards['loki-reads.json'].matchers.bloomGateway +
                               [utils.selector.re('route', grpc_routes)]
                             ),
                           )
                         )
                       )
                       .addRow(
                         $.row('TSDB Index')
                         .addPanel(
                           $.newQueryPanel('QPS') +
                           $.newQpsPanel('loki_index_request_duration_seconds_count{%s operation!="index_chunk"}' % dashboards['loki-reads.json'].querierSelector)
                         )
                         .addPanel(
                           $.newQueryPanel('Latency', 'ms') +
                           $.latencyPanel('loki_index_request_duration_seconds', '{%s operation!="index_chunk"}' % dashboards['loki-reads.json'].querierSelector)
                         )
                         .addPanel(
                           $.p99LatencyByPod(
                             'loki_index_request_duration_seconds',
                             '{%s operation!="index_chunk"}' % dashboards['loki-reads.json'].querierSelector
                           )
                         )
                       ),
  },
}
