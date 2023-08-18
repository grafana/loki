local utils = import 'mixin-utils/utils.libsonnet';

(import 'dashboard-utils.libsonnet') {
  grafanaDashboards+: {
    local dashboards = self,
    local showBigTable = false,

    'loki-writes.json': {
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
                            distributor: [utils.selector.re('job', '($namespace)/%s' % (if $._config.ssd.enabled then '%s-write' % $._config.ssd.pod_prefix_matcher else 'distributor'))],
                            ingester: [utils.selector.re('job', '($namespace)/%s' % (if $._config.ssd.enabled then '%s-write' % $._config.ssd.pod_prefix_matcher else 'ingester'))],
                            ingester_zone: [utils.selector.re('job', '($namespace)/%s' % (if $._config.ssd.enabled then '%s-write' % $._config.ssd.pod_prefix_matcher else 'ingester-zone.*'))],
                            any_ingester: [utils.selector.re('job', '($namespace)/%s' % (if $._config.ssd.enabled then '%s-write' % $._config.ssd.pod_prefix_matcher else 'ingester.*'))],
                          },

                          local selector(matcherId) =
                            local ms = cfg.clusterMatchers + cfg.matchers[matcherId];
                            if std.length(ms) > 0 then
                              std.join(',', ['%(label)s%(op)s"%(value)s"' % matcher for matcher in ms]) + ','
                            else '',

                          cortexGwSelector:: selector('cortexgateway'),
                          distributorSelector:: selector('distributor'),
                          ingesterSelector:: selector('ingester'),
                          ingesterZoneSelector:: selector('ingester_zone'),
                          anyIngester:: selector('any_ingester'),
                        } +
                        $.dashboard('Loki / Writes', uid='writes')
                        .addCluster()
                        .addNamespace()
                        .addTag()
                        .addRowIf(
                          $._config.internal_components,
                          $.row('Frontend (cortex_gw)')
                          .addPanel(
                            $.panel('QPS') +
                            $.qpsPanel('loki_request_duration_seconds_count{%s route=~"api_prom_push|loki_api_v1_push"}' % dashboards['loki-writes.json'].cortexGwSelector)
                          )
                          .addPanel(
                            $.panel('Latency') +
                            utils.latencyRecordingRulePanel(
                              'loki_request_duration_seconds',
                              dashboards['loki-writes.json'].clusterMatchers + dashboards['loki-writes.json'].matchers.cortexgateway + [utils.selector.re('route', 'api_prom_push|loki_api_v1_push')],
                            )
                          )
                        )
                        .addRow(
                          $.row(if $._config.ssd.enabled then 'Write Path' else 'Distributor')
                          .addPanel(
                            $.panel('QPS') +
                            $.qpsPanel('loki_request_duration_seconds_count{%s, route=~"api_prom_push|loki_api_v1_push|/httpgrpc.HTTP/Handle"}' % std.rstripChars(dashboards['loki-writes.json'].distributorSelector, ','))
                          )
                          .addPanel(
                            $.panel('Latency') +
                            utils.latencyRecordingRulePanel(
                              'loki_request_duration_seconds',
                              dashboards['loki-writes.json'].clusterMatchers + dashboards['loki-writes.json'].matchers.distributor,
                            )
                          )
                        )
                        .addRowIf(
                          !$._config.ssd.enabled,
                          $.row('Ingester - Zone Aware')
                          .addPanel(
                            $.panel('QPS') +
                            $.qpsPanel('loki_request_duration_seconds_count{%s route="/logproto.Pusher/Push"}' % dashboards['loki-writes.json'].ingesterZoneSelector)
                          )
                          .addPanel(
                            $.panel('Latency') +
                            utils.latencyRecordingRulePanel(
                              'loki_request_duration_seconds',
                              dashboards['loki-writes.json'].clusterMatchers + dashboards['loki-writes.json'].matchers.ingester_zone + [utils.selector.eq('route', '/logproto.Pusher/Push')],
                            )
                          )
                        )
                        .addRowIf(
                          !$._config.ssd.enabled,
                          $.row('Ingester')
                          .addPanel(
                            $.panel('QPS') +
                            $.qpsPanel('loki_request_duration_seconds_count{%s route="/logproto.Pusher/Push"}' % dashboards['loki-writes.json'].ingesterSelector) +
                            $.qpsPanel('loki_request_duration_seconds_count{%s route="/logproto.Pusher/Push"}' % dashboards['loki-writes.json'].ingesterSelector)
                          )
                          .addPanel(
                            $.panel('Latency') +
                            utils.latencyRecordingRulePanel(
                              'loki_request_duration_seconds',
                              dashboards['loki-writes.json'].clusterMatchers + dashboards['loki-writes.json'].matchers.ingester + [utils.selector.eq('route', '/logproto.Pusher/Push')],
                            )
                          )
                        )
                        .addRowIf(
                          !$._config.ssd.enabled,
                          $.row('Index')
                          .addPanel(
                            $.panel('QPS') +
                            $.qpsPanel('loki_index_request_duration_seconds_count{%s operation="index_chunk"}' % dashboards['loki-writes.json'].anyIngester)
                          )
                          .addPanel(
                            $.panel('Latency') +
                            $.latencyPanel('loki_index_request_duration_seconds', '{%s operation="index_chunk"}' % dashboards['loki-writes.json'].anyIngester)
                          )
                        )
                        .addRowIf(
                          showBigTable,
                          $.row('BigTable')
                          .addPanel(
                            $.panel('QPS') +
                            $.qpsPanel('loki_bigtable_request_duration_seconds_count{%s operation="/google.bigtable.v2.Bigtable/MutateRows"}' % dashboards['loki-writes.json'].ingesterSelector)
                          )
                          .addPanel(
                            $.panel('Latency') +
                            utils.latencyRecordingRulePanel(
                              'loki_bigtable_request_duration_seconds',
                              dashboards['loki-writes.json'].clusterMatchers + dashboards['loki-writes.json'].clusterMatchers + dashboards['loki-writes.json'].matchers.ingester + [utils.selector.eq('operation', '/google.bigtable.v2.Bigtable/MutateRows')]
                            )
                          )
                        )
                        .addRow(
                          $.row('BoltDB Shipper')
                          .addPanel(
                            $.panel('QPS') +
                            $.qpsPanel('loki_boltdb_shipper_request_duration_seconds_count{%s operation="WRITE"}' % dashboards['loki-writes.json'].ingesterSelector)
                          )
                          .addPanel(
                            $.panel('Latency') +
                            $.latencyPanel('loki_boltdb_shipper_request_duration_seconds', '{%s operation="WRITE"}' % dashboards['loki-writes.json'].ingesterSelector)
                          )
                        ),
  },
}
