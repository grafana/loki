local utils = import 'mixin-utils/utils.libsonnet';
local selector = (import '../selectors.libsonnet').new;

local selectors = {
  cortexGateway: selector().cortexGateway(),
  queryFrontend: selector().queryFrontend(),
  querier: selector().querier(),
  ingester: selector().ingester(),
  ingesterZone: selector().ingesterZone(),
  querierOrIndexGateway: selector().querier().indexGateway(),
  indexGateway: selector().indexGateway(),
  bloomGateway: selector().bloomGateway(),
};

// Available HTTP routes can be collected with the following instant query:
// count by (route) (loki_request_duration_seconds_count{route!~"/.*"})
local http_routes = [
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
];

// Available GRPC routes can be collected with the following instant query:
// count by (route) (loki_request_duration_seconds_count{route=~"/.*"})
local grpc_routes = [
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
];

(import 'dashboard-utils.libsonnet') {
  grafanaDashboards+: {
    local showBigTable = false,
    'loki-reads.json':
      $.dashboard('Loki / Reads', uid='reads')
      .addCluster()
      .addNamespace()
      .addTag()
      .addRowIf(
        $._config.internal_components,
        $.row('Frontend (cortex_gw)')
        .addPanel(
          $.newQueryPanel('QPS') +
          $.newQpsPanel('loki_request_duration_seconds_count{%s}' % selectors.cortexGateway.route(http_routes).build())
        )
        .addPanel(
          $.newQueryPanel('Latency', 'ms') +
          utils.latencyRecordingRulePanel(
            'loki_request_duration_seconds',
            selectors.cortexGateway.route(http_routes).list(),
            sum_by=['route']
          )
        )
        .addPanel(
          $.p99LatencyByPod(
            'loki_request_duration_seconds',
            selectors.cortexGateway.route(http_routes).build(brackets=true),
          )
        )
      )
      .addRow(
        $.row(if $._config.ssd.enabled then 'Read Path' else 'Frontend (query-frontend)')
        .addPanel(
          $.newQueryPanel('QPS') +
          $.newQpsPanel('loki_request_duration_seconds_count{%s}' % selectors.queryFrontend.route(http_routes).build())
        )
        .addPanel(
          $.newQueryPanel('Latency', 'ms') +
          utils.latencyRecordingRulePanel(
            'loki_request_duration_seconds',
            selectors.queryFrontend.route(http_routes).list(),
            sum_by=['route']
          )
        )
        .addPanel(
          $.p99LatencyByPod(
            'loki_request_duration_seconds',
            selectors.queryFrontend.route(http_routes).build(brackets=true),
          )
        )
      )
      .addRowIf(
        !$._config.ssd.enabled,
        $.row('Querier')
        .addPanel(
          $.newQueryPanel('QPS') +
          $.newQpsPanel('loki_request_duration_seconds_count{%s}' % selectors.querier.route(http_routes).build())
        )
        .addPanel(
          $.newQueryPanel('Latency', 'ms') +
          utils.latencyRecordingRulePanel(
            'loki_request_duration_seconds',
            selectors.querier.route(http_routes).list(),
            sum_by=['route']
          )
        )
        .addPanel(
          $.p99LatencyByPod(
            'loki_request_duration_seconds',
            selectors.querier.route(http_routes).build(brackets=true),
          )
        )
      )
      .addRowIf(
        !$._config.ssd.enabled,
        $.row('Ingester')
        .addPanel(
          $.newQueryPanel('QPS') +
          $.newQpsPanel('loki_request_duration_seconds_count{%s}' % selectors.ingester.route(grpc_routes).build())
        )
        .addPanel(
          $.newQueryPanel('Latency', 'ms') +
          utils.latencyRecordingRulePanel(
            'loki_request_duration_seconds',
            selectors.ingester.route(grpc_routes).list(),
            sum_by=['route']
          )
        )
        .addPanel(
          $.p99LatencyByPod(
            'loki_request_duration_seconds',
            selectors.ingester.route(grpc_routes).build(brackets=true),
          )
        )
      )
      // todo: add row iff multi zone ingesters are enabled
      .addRowIf(
        !$._config.ssd.enabled && $._config.components['ingester-zone'].enabled,
        $.row('Ingester - Zone Aware')
        .addPanel(
          $.newQueryPanel('QPS') +
          $.newQpsPanel('loki_request_duration_seconds_count{%s}' % selectors.ingesterZone.route(grpc_routes).build())
        )
        .addPanel(
          $.newQueryPanel('Latency', 'ms') +
          utils.latencyRecordingRulePanel(
            'loki_request_duration_seconds',
            selectors.ingesterZone.route(grpc_routes).list(),
            sum_by=['route']
          )
        )
        .addPanel(
          $.p99LatencyByPod(
            'loki_request_duration_seconds',
            selectors.ingesterZone.route(grpc_routes).build(brackets=true),
          )
        )
      )
      .addRowIf(
        !$._config.ssd.enabled,
        $.row('Index Gateway')
        .addPanel(
          $.newQueryPanel('QPS') +
          $.newQpsPanel('loki_request_duration_seconds_count{%s}' % selectors.indexGateway.route(grpc_routes).build())
        )
        .addPanel(
          $.newQueryPanel('Latency', 'ms') +
          utils.latencyRecordingRulePanel(
            'loki_request_duration_seconds',
            selectors.indexGateway.route(grpc_routes).list(),
            sum_by=['route']
          )
        )
        .addPanel(
          $.p99LatencyByPod(
            'loki_request_duration_seconds',
            selectors.indexGateway.route(grpc_routes).build(brackets=true),
          )
        )
      )
      .addRowIf(
        !$._config.ssd.enabled,
        $.row('Bloom Gateway')
        .addPanel(
          $.newQueryPanel('QPS') +
          $.newQpsPanel('loki_request_duration_seconds_count{%s}' % selectors.bloomGateway.route(grpc_routes).build())
        )
        .addPanel(
          $.newQueryPanel('Latency', 'ms') +
          utils.latencyRecordingRulePanel(
            'loki_request_duration_seconds',
            selectors.bloomGateway.route(grpc_routes).list(),
            sum_by=['route']
          )
        )
        .addPanel(
          $.p99LatencyByPod(
            'loki_request_duration_seconds',
            selectors.bloomGateway.route(grpc_routes).build(brackets=true),
          )
        )
      )
      .addRowIf(
        showBigTable,
        $.row('BigTable')
        .addPanel(
          $.newQueryPanel('QPS') +
          $.newQpsPanel('loki_bigtable_request_duration_seconds_count{%s}' % selectors.querier.label('operation').eq('/google.bigtable.v2.Bigtable/ReadRows').build())
        )
        .addPanel(
          $.newQueryPanel('Latency', 'ms') +
          utils.latencyRecordingRulePanel(
            'loki_bigtable_request_duration_seconds',
            selectors.querier.label('operation').eq('/google.bigtable.v2.Bigtable/ReadRows').list()
          )
        )
      )
      .addRowIf(
        !$._config.ssd.enabled,
        $.row('TSBD Index')
        .addPanel(
          $.newQueryPanel('QPS') +
          $.newQpsPanel('loki_index_request_duration_seconds_count{%s}' % selectors.querier.label('operation').neq('index_chunk').build())
        )
        .addPanel(
          $.newQueryPanel('Latency', 'ms') +
          $.latencyPanel(
            'loki_index_request_duration_seconds',
            selectors.querier.label('operation').neq('index_chunk').build(brackets=true)
          )
        )
        .addPanel(
          $.p99LatencyByPod(
            'loki_index_request_duration_seconds',
            selectors.querier.label('operation').neq('index_chunk').build(brackets=true)
          )
        )
      )
      .addRowIf(
        !$._config.ssd.enabled && $._config.operational.boltDB,
        $.row('BoltDB Index')
        .addPanel(
          $.newQueryPanel('QPS') +
          $.newQpsPanel('loki_boltdb_shipper_request_duration_seconds_count{%s}' % selectors.querierOrIndexGateway.label('operation').eq('Shipper.Query').build())
        )
        .addPanel(
          $.newQueryPanel('Latency', 'ms') +
          $.latencyPanel(
            'loki_boltdb_shipper_request_duration_seconds',
            selectors.querierOrIndexGateway.label('operation').eq('Shipper.Query').build(brackets=true)
          )
        )
        .addPanel(
          $.p99LatencyByPod(
            'loki_boltdb_shipper_request_duration_seconds',
            selectors.querierOrIndexGateway.label('operation').eq('Shipper.Query').build(brackets=true)
          )
        )
      ),
  },
}
