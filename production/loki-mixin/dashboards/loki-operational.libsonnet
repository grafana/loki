local lokiOperational = (import './dashboard-loki-operational.json');
local utils = import 'mixin-utils/utils.libsonnet';

{
  grafanaDashboards+: {
    local dashboards = self,

    'loki-operational.json': {
      local cfg = self,

      showAnnotations:: true,
      showLinks:: true,
      showMultiCluster:: true,
      clusterLabel:: 'cluster',

      namespaceType:: 'query',
      namespaceQuery::
        if cfg.showMultiCluster then
          'kube_pod_container_info{cluster="$cluster"}'
        else
          'kube_pod_container_info',

      assert (cfg.namespaceType == 'custom' || cfg.namespaceType == 'query') : "Only types 'query' and 'custom' are allowed for dashboard variable 'namespace'",

      matchers:: {
        cortexgateway: [utils.selector.re('job', '($namespace)/cortex-gw')],
        distributor: [utils.selector.re('job', '($namespace)/distributor')],
        ingester: [utils.selector.re('job', '($namespace)/ingester')],
        querier: [utils.selector.re('job', '($namespace)/querier')],
      },

      templateLabels:: [
        {
          name:: 'logs',
          type:: 'datasource',
          query:: 'loki',
        },
        {
          name:: 'metrics',
          type:: 'datasource',
          query:: 'prometheus',
        },
      ]+(
        if cfg.showMultiCluster then [
          {
            variable:: 'cluster',
            label:: cfg.clusterLabel,
            query:: 'kube_pod_container_info',
            datasource:: '$metrics',
            type:: 'query',
          },
        ] else []
      ) + [
        {
          variable:: 'namespace',
          label:: 'namespace',
          query:: cfg.namespaceQuery,
          datasource:: '$metrics',
          type:: cfg.namespaceType,
        },
      ],
    } + lokiOperational + {
      annotations:
        if dashboards['loki-operational.json'].showAnnotations
        then super.annotations
        else {},

      links:
        if dashboards['loki-operational.json'].showLinks then
          super.links
        else [],

      local matcherStr(matcherId) =
        if std.length(dashboards['loki-operational.json'].matchers[matcherId]) > 0 then
          std.join(',', ['%(label)s%(op)s"%(value)s"' % matcher for matcher in dashboards['loki-operational.json'].matchers[matcherId]]) + ','
        else '',

      local replaceClusterMatchers(expr) =
        if dashboards['loki-operational.json'].showMultiCluster
        then expr
        else
          std.strReplace(
            std.strReplace(
              std.strReplace(
                expr,
                ', cluster="$cluster"',
                ''
              ),
              ', cluster=~"$cluster"',
              ''
            ),
            'cluster="$cluster",',
            ''
          ),

      local replaceMatchers(expr) =
        std.strReplace(
          std.strReplace(
            std.strReplace(
              std.strReplace(
                std.strReplace(
                  std.strReplace(
                    std.strReplace(
                      std.strReplace(
                        std.strReplace(
                          std.strReplace(
                            std.strReplace(
                              std.strReplace(
                                expr,
                                'job="$namespace/cortex-gw",',
                                matcherStr('cortexgateway')
                              ),
                              'job="$namespace/cortex-gw"',
                              std.rstripChars(matcherStr('cortexgateway'), ',')
                            ),
                            'job=~"($namespace)/cortex-gw",',
                            matcherStr('cortexgateway')
                          ),
                          'job="$namespace/distributor",',
                          matcherStr('distributor')
                        ),
                        'job="$namespace/distributor"',
                        std.rstripChars(matcherStr('distributor'), ',')
                      ),
                      'job=~"($namespace)/distributor",',
                      matcherStr('distributor')
                    ),
                    'job=~"($namespace)/distributor"',
                    std.rstripChars(matcherStr('distributor'), ',')
                  ),
                  'job="$namespace/ingester",',
                  matcherStr('ingester')
                ),
                'job="$namespace/ingester"',
                std.rstripChars(matcherStr('ingester'), ',')
              ),
              'job=~"($namespace)/ingester",',
              matcherStr('ingester'),
            ),
            'job="$namespace/querier",',
            matcherStr('querier')
          ),
          'job="$namespace/querier"',
          std.rstripChars(matcherStr('querier'), ',')
        ),

      local replaceAllMatchers(expr) =
        replaceMatchers(replaceClusterMatchers(expr)),

      local selectDatasource(ds) =
        if ds == null || ds == "" then ds
        else if ds == "$datasource" then "$metrics"
        else "$logs",

      panels: [
        p {
          datasource: selectDatasource(super.datasource),
          targets: if std.objectHas(p, 'targets') then [
            e {
              expr: replaceAllMatchers(e.expr),
            }
            for e in p.targets
          ] else [],
          panels: if std.objectHas(p, 'panels') then [
            sp {
              datasource: selectDatasource(super.datasource),
              targets: if std.objectHas(sp, 'targets') then [
                e {
                  expr: replaceAllMatchers(e.expr),
                }
                for e in sp.targets
              ] else [],
              panels: if std.objectHas(sp, 'panels') then [
                ssp {
                  datasource: selectDatasource(super.datasource),
                  targets: if std.objectHas(ssp, 'targets') then [
                    e {
                      expr: replaceAllMatchers(e.expr),
                    }
                    for e in ssp.targets
                  ] else [],
                }
                for ssp in sp.panels
              ] else []
            }
            for sp in p.panels
          ] else []
        }
        for p in super.panels
      ],
      templating: {
        list+:[
          {
            hide: 0,
            includeAll: false,
            label: null,
            multi: false,
            name: l.name,
            options: [],
            query: l.query,
            refresh: 1,
            regex: '',
            skipUrlSync: false,
            type: l.type,
          },
          for l in dashboards['loki-operational.json'].templateLabels
          if l.type == 'datasource'
        ] + [
          {
            allValue: null,
            current:
              if l.type == 'custom' then {
                text: l.query,
                value: l.query,
              } else {},
            datasource: l.datasource,
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
          for l in dashboards['loki-operational.json'].templateLabels
          if l.type == 'query' || l.type == 'custom'
        ],
      },
    },
  },
}
