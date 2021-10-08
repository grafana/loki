local utils = import 'mixin-utils/utils.libsonnet';

(import 'dashboard-utils.libsonnet') {
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
    $.dashboard('Loki / Chunks')
    .addClusterSelectorTemplates(false)
    .addRow(
      $.row('Active Series / Chunks')
      .addPanel(
        $.panel('Series') +
        $.queryPanel('sum(loki_ingester_memory_chunks{%s})' % dashboards['loki-chunks.json'].ingesterSelector, 'series'),
      )
      .addPanel(
        $.panel('Chunks per series') +
        $.queryPanel(
          'sum(loki_ingester_memory_chunks{%s}) / sum(loki_ingester_memory_streams{%s})' % [
            dashboards['loki-chunks.json'].ingesterSelector,
            dashboards['loki-chunks.json'].ingesterSelectorOnly,
          ],
          'chunks'
        ),
      )
    )
    .addRow(
      $.row('Flush Stats')
      .addPanel(
        $.panel('Utilization') +
        $.latencyPanel('loki_ingester_chunk_utilization', '{%s}' % dashboards['loki-chunks.json'].ingesterSelector, multiplier='1') +
        { yaxes: $.yaxes('percentunit') },
      )
      .addPanel(
        $.panel('Age') +
        $.latencyPanel('loki_ingester_chunk_age_seconds', '{%s}' % dashboards['loki-chunks.json'].ingesterSelector),
      ),
    )
    .addRow(
      $.row('Flush Stats')
      .addPanel(
        $.panel('Size') +
        $.latencyPanel('loki_ingester_chunk_entries', '{%s}' % dashboards['loki-chunks.json'].ingesterSelector, multiplier='1') +
        { yaxes: $.yaxes('short') },
      )
      .addPanel(
        $.panel('Entries') +
        $.queryPanel(
          'sum(rate(loki_chunk_store_index_entries_per_chunk_sum{%s}[5m])) / sum(rate(loki_chunk_store_index_entries_per_chunk_count{%s}[5m]))' % [
            dashboards['loki-chunks.json'].ingesterSelector,
            dashboards['loki-chunks.json'].ingesterSelector,
          ],
          'entries'
        ),
      ),
    )
    .addRow(
      $.row('Flush Stats')
      .addPanel(
        $.panel('Queue Length') +
        $.queryPanel('cortex_ingester_flush_queue_length{%s}' % dashboards['loki-chunks.json'].ingesterSelector, '{{pod}}'),
      )
      .addPanel(
        $.panel('Flush Rate') +
        $.qpsPanel('loki_ingester_chunk_age_seconds_count{%s}' % dashboards['loki-chunks.json'].ingesterSelector,),
      ),
    )
    .addRow(
      $.row('Duration')
      .addPanel(
        $.panel('Chunk Duration hours (end-start)') +
        $.queryPanel(
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
