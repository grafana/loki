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

      matchers:: {
        cortexgateway: [utils.selector.re('job', '($namespace)/cortex-gw')],
        distributor: [utils.selector.re('job', '($namespace)/distributor')],
        ingester: [utils.selector.re('job', '($namespace)/ingester')],
        querier: [utils.selector.re('job', '($namespace)/querier')],
      },
    }
    + lokiOperational + {
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
        if ds == null || ds == '' then ds
        else if ds == '$datasource' then '$datasource'
        else '$logs',

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
              ] else [],
            }
            for sp in p.panels
          ] else [],
        }
        for p in super.panels
      ],
    } +
      $.dashboard('Loki / Operational')
      // TODO (callum) For this cluster the cluster template is not actually
      // added since the json defines the template array, we just need the tags
      // and links from this function until they're moved to a separate function.
      .addClusterSelectorTemplates(false)
  },
}
