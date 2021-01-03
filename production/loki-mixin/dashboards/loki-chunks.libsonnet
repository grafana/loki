local g = import 'grafana-builder/grafana.libsonnet';
local utils = import 'mixin-utils/utils.libsonnet';

{
  grafanaDashboards+: {
    local dashboards = self,

    'loki-chunks.json':{
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
        ingester: [utils.selector.re('job', '($namespace)/ingester')],
      },

      local selector(matcherId) =
        std.join(',', ['%(label)s%(op)s"%(value)s"' % matcher for matcher in (cfg.clusterMatchers + cfg.matchers[matcherId])]),

      ingesterSelector:: selector('ingester'),
      ingesterSelectorOnly::
        std.join(',', ['%(label)s%(op)s"%(value)s"' % matcher for matcher in cfg.matchers.ingester]),

      templateLabels:: (
        if cfg.showMultiCluster then [
          {
            variable:: 'cluster',
            label:: cfg.clusterLabel,
            query:: 'kube_pod_container_info{image=~".*loki.*"}',
            type:: 'query'
          },
        ] else []
      ) + [
        {
          variable:: 'namespace',
          label:: 'namespace',
          query:: cfg.namespaceQuery,
          type:: cfg.namespaceType
        },
      ],
    } +
    g.dashboard('Loki / Chunks')
    .addRow(
      g.row('Active Series / Chunks')
      .addPanel(
        g.panel('Series') +
        g.queryPanel('sum(loki_ingester_memory_chunks{%s})' % dashboards['loki-chunks.json'].ingesterSelector, 'series'),
      )
      .addPanel(
        g.panel('Chunks per series') +
        g.queryPanel(
          'sum(loki_ingester_memory_chunks{%s}) / sum(loki_ingester_memory_streams{%s})' % [
            dashboards['loki-chunks.json'].ingesterSelector,
            dashboards['loki-chunks.json'].ingesterSelectorOnly,
          ],
          'chunks'
        ),
      )
    )
    .addRow(
      g.row('Flush Stats')
      .addPanel(
        g.panel('Utilization') +
        g.latencyPanel('loki_ingester_chunk_utilization', '{%s}' % dashboards['loki-chunks.json'].ingesterSelector, multiplier='1') +
        { yaxes: g.yaxes('percentunit') },
      )
      .addPanel(
        g.panel('Age') +
        g.latencyPanel('loki_ingester_chunk_age_seconds', '{%s}' % dashboards['loki-chunks.json'].ingesterSelector),
      ),
    )
    .addRow(
      g.row('Flush Stats')
      .addPanel(
        g.panel('Size') +
        g.latencyPanel('loki_ingester_chunk_entries', '{%s}' % dashboards['loki-chunks.json'].ingesterSelector, multiplier='1') +
        { yaxes: g.yaxes('short') },
      )
      .addPanel(
        g.panel('Entries') +
        g.queryPanel(
          'sum(rate(cortex_chunk_store_index_entries_per_chunk_sum{%s}[5m])) / sum(rate(cortex_chunk_store_index_entries_per_chunk_count{%s}[5m]))' % [
            dashboards['loki-chunks.json'].ingesterSelector,
            dashboards['loki-chunks.json'].ingesterSelector,
          ],
          'entries'
        ),
      ),
    )
    .addRow(
      g.row('Flush Stats')
      .addPanel(
        g.panel('Queue Length') +
        g.queryPanel('cortex_ingester_flush_queue_length{%s}' % dashboards['loki-chunks.json'].ingesterSelector, '{{pod}}'),
      )
      .addPanel(
        g.panel('Flush Rate') +
        g.qpsPanel('loki_ingester_chunk_age_seconds_count{%s}' % dashboards['loki-chunks.json'].ingesterSelector,),
      ),
    )
    .addRow(
      g.row('Duration')
      .addPanel(
        g.panel('Chunk Duration hours (end-start)') +
        g.queryPanel(
          [
            'histogram_quantile(0.5, sum(rate(loki_ingester_chunk_bounds_hours_bucket{%s}[5m])) by (le))' % dashboards['loki-chunks.json'].ingesterSelector,
            'histogram_quantile(0.99, sum(rate(loki_ingester_chunk_bounds_hours_bucket{%s}[5m])) by (le))' % dashboards['loki-chunks.json'].ingesterSelector,
            'sum(rate(loki_ingester_chunk_bounds_hours_sum{%s}[5m])) / sum(rate(loki_ingester_chunk_bounds_hours_count{%s}[5m]))' % [
              dashboards['loki-chunks.json'].ingesterSelector,
              dashboards['loki-chunks.json'].ingesterSelector,
            ],
          ],
          [
            'p50',
            'p99',
            'avg',
          ],
        ),
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
          for l in dashboards['loki-chunks.json'].templateLabels
        ],
      },
    },
  }
}
