local utils = import 'mixin-utils/utils.libsonnet';

(import 'dashboard-utils.libsonnet') {
  grafanaDashboards+: {
    local dashboards = self,

    'loki-writes.json': {
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
        distributor: [utils.selector.re('job', '($namespace)/distributor')],
        ingester: [utils.selector.re('job', '($namespace)/ingester')],
      },

      local selector(matcherId) =
        local ms = cfg.clusterMatchers + cfg.matchers[matcherId];
        if std.length(ms) > 0 then
          std.join(',', ['%(label)s%(op)s"%(value)s"' % matcher for matcher in ms]) + ','
        else '',

      cortexGwSelector:: selector('cortexgateway'),
      distributorSelector:: selector('distributor'),
      ingesterSelector:: selector('ingester'),
    } +
    $.dashboard('Loki / Writes')
    .addClusterSelectorTemplates(false)
    .addRow(
      $.row('Frontend (cortex_gw)')
      .addPanel(
        $.panel('QPS') +
        $.qpsPanel('loki_request_duration_seconds_count{%s route=~"api_prom_push|loki_api_v1_push"}' % dashboards['loki-writes.json'].cortexGwSelector)
      )
      .addPanel(
        $.panel('Latency') +
        utils.latencyRecordingRulePanel(
          'loki_request_duration_seconds',
          dashboards['loki-writes.json'].matchers.cortexgateway + [utils.selector.re('route', 'api_prom_push|loki_api_v1_push')],
          extra_selectors=dashboards['loki-writes.json'].clusterMatchers
        )
      )
    )
    .addRow(
      $.row('Distributor')
      .addPanel(
        $.panel('QPS') +
        $.qpsPanel('loki_request_duration_seconds_count{%s}' % std.rstripChars(dashboards['loki-writes.json'].distributorSelector, ','))
      )
      .addPanel(
        $.panel('Latency') +
        utils.latencyRecordingRulePanel(
          'loki_request_duration_seconds',
          dashboards['loki-writes.json'].matchers.distributor,
          extra_selectors=dashboards['loki-writes.json'].clusterMatchers
        )
      )
    )
    .addRow(
      $.row('Ingester')
      .addPanel(
        $.panel('QPS') +
        $.qpsPanel('loki_request_duration_seconds_count{%s route="/logproto.Pusher/Push"}' % dashboards['loki-writes.json'].ingesterSelector)
      )
      .addPanel(
        $.panel('Latency') +
        utils.latencyRecordingRulePanel(
          'loki_request_duration_seconds',
          dashboards['loki-writes.json'].matchers.ingester + [utils.selector.eq('route', '/logproto.Pusher/Push')],
          extra_selectors=dashboards['loki-writes.json'].clusterMatchers
        )
      )
    )
    .addRow(
      $.row('BigTable')
      .addPanel(
        $.panel('QPS') +
        $.qpsPanel('cortex_bigtable_request_duration_seconds_count{%s operation="/google.bigtable.v2.Bigtable/MutateRows"}' % dashboards['loki-writes.json'].ingesterSelector)
      )
      .addPanel(
        $.panel('Latency') +
        utils.latencyRecordingRulePanel(
          'cortex_bigtable_request_duration_seconds',
          dashboards['loki-writes.json'].clusterMatchers + dashboards['loki-writes.json'].matchers.ingester + [utils.selector.eq('operation', '/google.bigtable.v2.Bigtable/MutateRows')]
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
    )
  },
}
