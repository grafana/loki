local utils = import 'mixin-utils/utils.libsonnet';
local selector = (import '../selectors.libsonnet').new;

local selectors = {
  cortexGw: selector().cortexGateway(),
  distributor: selector().distributor(),
  ingester: selector().job(['ingester', 'partition-ingester']),
  ingesterZone: selector().job(['partition-ingester', 'ingester-zone']),
};

local http_routes = [
  'api_prom_push',
  'loki_api_v1_push',
  'otlp_v1_logs',
];

local distributor_routes = http_routes + [
  '/httpgrpc.HTTP/Handle',
];

local ingester_routes = http_routes + [
  '/logproto.Pusher/Push',
];

(import 'dashboard-utils.libsonnet') {
  grafanaDashboards+: {
    local showBigTable = false,

    'loki-writes.json':
      $.dashboard('Loki / Writes', uid='writes')
      .addCluster()
      .addNamespace()
      .addTag()
      .addRowIf(
        $._config.internal_components,
        $.row('Frontend (cortex_gw)')
        .addPanel(
          $.newQueryPanel('QPS') +
          $.newQpsPanel('loki_request_duration_seconds_count{%s}' % selectors.cortexGw.route(http_routes).build())
        )
        .addPanel(
          $.newQueryPanel('Latency', 'ms') +
          utils.latencyRecordingRulePanel(
            'loki_request_duration_seconds',
            selectors.cortexGw.route(http_routes).list(),
          )
        )
        .addPanel(
          $.p99LatencyByPod(
            'loki_request_duration_seconds',
            selectors.cortexGw.route(http_routes).build(brackets=true),
          )
        )
      )
      .addRow(
        $.row(if $._config.ssd.enabled then 'Write Path' else 'Distributor')
        .addPanel(
          $.newQueryPanel('QPS') +
          $.newQpsPanel('loki_request_duration_seconds_count{%s}' % selectors.distributor.route(distributor_routes).build())
        )
        .addPanel(
          $.newQueryPanel('Latency', 'ms') +
          utils.latencyRecordingRulePanel(
            'loki_request_duration_seconds',
            selectors.distributor.route(distributor_routes).list(),
          )
        )
        .addPanel(
          $.p99LatencyByPod(
            'loki_request_duration_seconds',
            selectors.distributor.route(distributor_routes).build(brackets=true),
          )
        )
      )
      .addRowIf(
        $._config.tsdb,
        $.row((if $._config.ssd.enabled then 'Write Path' else 'Distributor') + ' - Structured Metadata')
        .addPanel(
          $.newQueryPanel('Per Total Received Bytes') +
          $.queryPanel(
            |||
              sum(
                rate(loki_distributor_structured_metadata_bytes_received_total{%(selector)s}[$__rate_interval])
              )
              /
              sum(
                rate(loki_distributor_bytes_received_total{%(selector)s}[$__rate_interval])
              )
            ||| % { selector: selectors.distributor.build() },
            'bytes'
          )
        )
        .addPanel(
          $.newQueryPanel('Per Tenant') +
          $.queryPanel(
            |||
              sum by (tenant) (
                rate(loki_distributor_structured_metadata_bytes_received_total{%(selector)s}[$__rate_interval])
              )
              / ignoring(tenant) group_left
              sum(
                rate(loki_distributor_structured_metadata_bytes_received_total{%(selector)s}[$__rate_interval])
              )
            ||| % { selector: selectors.distributor.build() },
            '{{tenant}}'
          ) + {
            stack: true,
            yaxes: [
              { format: 'short', label: null, logBase: 1, max: 1, min: 0, show: true },
              { format: 'short', label: null, logBase: 1, max: 1, min: null, show: false },
            ],
          },
        )
      )
      .addRowIf(
        !$._config.ssd.enabled && $._config.components['ingester-zone'].enabled,
        $.row('Ingester - Zone Aware')
        .addPanel(
          $.newQueryPanel('QPS') +
          $.newQpsPanel('loki_request_duration_seconds_count{%s}' % selectors.ingesterZone.route(ingester_routes).build())
        )
        .addPanel(
          $.newQueryPanel('Latency', 'ms') +
          utils.latencyRecordingRulePanel(
            'loki_request_duration_seconds',
            selectors.ingesterZone.route(ingester_routes).list(),
          )
        )
        .addPanel(
          $.p99LatencyByPod(
            'loki_request_duration_seconds',
            selectors.ingesterZone.route(ingester_routes).build(brackets=true),
          )
        )
      )
      .addRowIf(
        !$._config.ssd.enabled,
        $.row('Ingester')
        .addPanel(
          $.newQueryPanel('QPS') +
          $.newQpsPanel('loki_request_duration_seconds_count{%s}' % selectors.ingester.route(ingester_routes).build())
        )
        .addPanel(
          $.newQueryPanel('Latency', 'ms') +
          utils.latencyRecordingRulePanel(
            'loki_request_duration_seconds',
            selectors.ingester.route(ingester_routes).list(),
          )
        )
        .addPanel(
          $.p99LatencyByPod(
            'loki_request_duration_seconds',
            selectors.ingester.route(ingester_routes).build(brackets=true),
          )
        )
      )
      .addRowIf(
        !$._config.ssd.enabled,
        $.row('Index')
        .addPanel(
          $.newQueryPanel('QPS') +
          $.newQpsPanel('loki_index_request_duration_seconds_count{%s}' % selectors.ingester.label('operation').eq('index_chunk').build())
        )
        .addPanel(
          $.newQueryPanel('Latency', 'ms') +
          $.latencyPanel('loki_index_request_duration_seconds', '{%s}' % selectors.ingester.label('operation').eq('index_chunk').build())
        )
        .addPanel(
          $.p99LatencyByPod(
            'loki_index_request_duration_seconds',
            selectors.ingester.label('operation').eq('index_chunk').build(brackets=true),
          )
        )
      )
      .addRowIf(
        showBigTable,
        $.row('BigTable')
        .addPanel(
          $.newQueryPanel('QPS') +
          $.newQpsPanel('loki_bigtable_request_duration_seconds_count{%s}' % selectors.ingester.label('operation').eq('/google.bigtable.v2.Bigtable/MutateRows').build())
        )
        .addPanel(
          $.newQueryPanel('Latency', 'ms') +
          utils.latencyRecordingRulePanel(
            'loki_bigtable_request_duration_seconds',
            selectors.ingester.label('operation').eq('/google.bigtable.v2.Bigtable/MutateRows').list(),
          )
        )
        .addPanel(
          $.p99LatencyByPod(
            'loki_bigtable_request_duration_seconds',
            selectors.ingester.label('operation').eq('/google.bigtable.v2.Bigtable/MutateRows').build(brackets=true),
          )
        )
      )
      .addRowIf(
        !$._config.ssd.enabled && $._config.operational.boltDB,
        $.row('BoltDB Index')
        .addPanel(
          $.newQueryPanel('QPS') +
          $.newQpsPanel('loki_boltdb_shipper_request_duration_seconds_count{%s}' % selectors.ingester.label('operation').eq('WRITE').build())
        )
        .addPanel(
          $.newQueryPanel('Latency', 'ms') +
          $.latencyPanel('loki_boltdb_shipper_request_duration_seconds', '{%s}' % selectors.ingester.label('operation').eq('WRITE').build())
        )
        .addPanel(
          $.p99LatencyByPod(
            'loki_boltdb_shipper_request_duration_seconds',
            selectors.ingester.label('operation').eq('WRITE').build(brackets=true),
          )
        )
      ),
  },
}
