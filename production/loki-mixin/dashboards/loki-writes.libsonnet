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

      namespaceType:: 'query',
      namespaceQuery::
        if cfg.showMultiCluster then
          'kube_pod_container_info{cluster="$cluster", image=~".*loki.*"}'
        else
          'kube_pod_container_info{image=~".*loki.*"}',

      assert (cfg.namespaceType == 'custom' || cfg.namespaceType == 'query') : "Only types 'query' and 'custom' are allowed for dashboard variable 'namespace'",

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

      templateLabels:: (
        if cfg.showMultiCluster then [
          {
            variable:: 'cluster',
            label:: cfg.clusterLabel,
            query:: 'kube_pod_container_info{image=~".*loki.*"}',
            type:: 'query',
          },
        ] else []
      ) + [
        {
          variable:: 'namespace',
          label:: 'namespace',
          query:: cfg.namespaceQuery,
          type:: cfg.namespaceType,
        },
      ],
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
    ){
      templating+: {
        list+: [
          {
            allValue: null,
            current:
              if l.type == 'custom' then {
                text: l.query,
                value: l.query,
              } else {},
            datasource: '$datasource',
            hide: 0,
            includeAll: false,
            label: l.variable,
            multi: false,
            name: l.variable,
            options: [],
            query:
              if l.type == 'query' then
                'label_values(%s, %s)' % [l.query, l.label]
              else
                l.query,
            refresh: 1,
            regex: '',
            sort: 2,
            tagValuesQuery: '',
            tags: [],
            tagsQuery: '',
            type: l.type,
            useTags: false,
          }
          for l in dashboards['loki-writes.json'].templateLabels
        ],
      },
    },
  },
}
