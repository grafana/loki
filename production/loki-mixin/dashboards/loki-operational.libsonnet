local lokiOperational = (import './dashboard-loki-operational.json');
local utils = import 'mixin-utils/utils.libsonnet';

(import 'dashboard-utils.libsonnet') {
  grafanaDashboards+: {
    local dashboards = self,

    'loki-operational.json': {
                               local cfg = self,

                               showAnnotations:: true,
                               showLinks:: true,
                               showMultiCluster:: true,

                               hiddenRows:: [
                                 'Cassandra',
                               ] + if !$._config.ssd.enabled then [] else [
                                 'Ingester',
                               ],

                               hiddenPanels:: if $._config.promtail.enabled then [] else [
                                 'Bad Words',
                               ],

                               jobMatchers:: {
                                 cortexgateway: [utils.selector.re('job', '($namespace)/cortex-gw(-internal)?')],
                                 distributor: if $._config.meta_monitoring.enabled
                                 then [utils.selector.re('job', '($namespace)/(distributor|%s-write|loki-single-binary)' % $._config.ssd.pod_prefix_matcher)]
                                 else [utils.selector.re('job', '($namespace)/%s' % (if $._config.ssd.enabled then '%s-write' % $._config.ssd.pod_prefix_matcher else 'distributor'))],
                                 ingester: if $._config.meta_monitoring.enabled
                                 then [utils.selector.re('job', '($namespace)/(ingester|%s-write|loki-single-binary)' % $._config.ssd.pod_prefix_matcher)]
                                 else [utils.selector.re('job', '($namespace)/%s' % (if $._config.ssd.enabled then '%s-write' % $._config.ssd.pod_prefix_matcher else 'ingester.*'))],
                                 querier: if $._config.meta_monitoring.enabled
                                 then [utils.selector.re('job', '($namespace)/(querier|%s-read|loki-single-binary)' % $._config.ssd.pod_prefix_matcher)]
                                 else [utils.selector.re('job', '($namespace)/%s' % (if $._config.ssd.enabled then '%s-read' % $._config.ssd.pod_prefix_matcher else 'querier'))],
                                 queryFrontend: if $._config.meta_monitoring.enabled
                                 then [utils.selector.re('job', '($namespace)/(query-frontend|%s-read|loki-single-binary)' % $._config.ssd.pod_prefix_matcher)]
                                 else [utils.selector.re('job', '($namespace)/%s' % (if $._config.ssd.enabled then '%s-read' % $._config.ssd.pod_prefix_matcher else 'query-frontend'))],
                               },

                               podMatchers:: {
                                 cortexgateway: [utils.selector.re('pod', 'cortex-gw')],
                                 distributor: if $._config.meta_monitoring.enabled
                                 then [utils.selector.re('pod', '(distributor|%s-write|loki-single-binary)' % $._config.ssd.pod_prefix_matcher)]
                                 else [utils.selector.re('pod', '%s' % (if $._config.ssd.enabled then '%s-write.*' % $._config.ssd.pod_prefix_matcher else 'distributor.*'))],
                                 ingester: if $._config.meta_monitoring.enabled
                                 then [utils.selector.re('pod', '(ingester|%s-write|loki-single-binary)' % $._config.ssd.pod_prefix_matcher)]
                                 else [utils.selector.re('pod', '%s' % (if $._config.ssd.enabled then '%s-write.*' % $._config.ssd.pod_prefix_matcher else 'ingester.*'))],
                                 querier: if $._config.meta_monitoring.enabled
                                 then [utils.selector.re('pod', '(querier|%s-read|loki-single-binary)' % $._config.ssd.pod_prefix_matcher)]
                                 else [utils.selector.re('pod', '%s' % (if $._config.ssd.enabled then '%s-read.*' % $._config.ssd.pod_prefix_matcher else 'querier.*'))],
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

                               local matcherStr(matcherId, matcher='job', sep=',') =
                                 if matcher == 'job' then
                                   if std.length(dashboards['loki-operational.json'].jobMatchers[matcherId]) > 0 then
                                     std.join(',', ['%(label)s%(op)s"%(value)s"' % matcher for matcher in dashboards['loki-operational.json'].jobMatchers[matcherId]]) + sep
                                   else error 'no job matchers'
                                 else if matcher == 'pod' then
                                   if std.length(dashboards['loki-operational.json'].podMatchers[matcherId]) > 0 then
                                     std.join(',', ['%(label)s%(op)s"%(value)s"' % matcher for matcher in dashboards['loki-operational.json'].podMatchers[matcherId]]) + sep
                                   else error 'no pod matchers'
                                 else error 'matcher must be either job or container',

                               local replaceClusterMatchers(expr) =
                                 if dashboards['loki-operational.json'].showMultiCluster
                                 // Replace the recording rules cluster label with the per-cluster label
                                 then std.strReplace(
                                   // Replace the cluster label for equality matchers with the per-cluster label
                                   std.strReplace(
                                     // Replace the cluster label for regex matchers with the per-cluster label
                                     std.strReplace(
                                       expr,
                                       'cluster=~"$cluster"',
                                       $._config.per_cluster_label + '=~"$cluster"'
                                     ),
                                     'cluster="$cluster"',
                                     $._config.per_cluster_label + '="$cluster"'
                                   ),
                                   'cluster_job',
                                   $._config.per_cluster_label + '_job'
                                 )
                                 else
                                   std.strReplace(
                                     std.strReplace(
                                       std.strReplace(
                                         expr,
                                         ', ' + $._config.per_cluster_label + '="$cluster"',
                                         ''
                                       ),
                                       ', ' + $._config.per_cluster_label + '=~"$cluster"',
                                       ''
                                     ),
                                     $._config.per_cluster_label + '="$cluster",',
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
                                                         std.strReplace(
                                                           std.strReplace(
                                                             std.strReplace(
                                                               expr,
                                                               'pod=~"querier.*"',
                                                               matcherStr('querier', matcher='pod', sep='')
                                                             ),
                                                             'pod=~"ingester.*"',
                                                             matcherStr('ingester', matcher='pod', sep='')
                                                           ),
                                                           'pod=~"distributor.*"',
                                                           matcherStr('distributor', matcher='pod', sep='')
                                                         ),
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
                                 replaceMatchers(expr),

                               local selectDatasource(ds) =
                                 if ds == null || ds == '' then ds
                                 else if ds == '$datasource' then '$datasource'
                                 else '$loki_datasource',

                               local isRowHidden(row) =
                                 std.member(dashboards['loki-operational.json'].hiddenRows, row),

                               local isPanelHidden(panelTitle) =
                                 std.member(dashboards['loki-operational.json'].hiddenPanels, panelTitle),

                               local replaceCortexGateway(expr, replacement) = if $._config.internal_components then
                                 expr
                               else
                                 std.strReplace(
                                   expr,
                                   'job=~"$namespace/cortex-gw(-internal)?"',
                                   matcherStr(replacement, matcher='job', sep='')
                                 ),

                               local removeInternalComponents(title, expr) = if (title == 'Queries/Second') then
                                 replaceCortexGateway(expr, 'queryFrontend')
                               else if (title == 'Pushes/Second') then
                                 replaceCortexGateway(expr, 'distributor')
                               else if (title == 'Push Latency') then
                                 replaceCortexGateway(expr, 'distributor')
                               else
                                 replaceAllMatchers(expr),

                               panels: [
                                 p {
                                   datasource: selectDatasource(super.datasource),
                                   targets: if std.objectHas(p, 'targets') then [
                                     e {
                                       expr: removeInternalComponents(p.title, replaceClusterMatchers(e.expr)),
                                     }
                                     for e in p.targets
                                   ] else [],
                                   panels: if std.objectHas(p, 'panels') then [
                                     sp {
                                       datasource: selectDatasource(super.datasource),
                                       targets: if std.objectHas(sp, 'targets') then [
                                         e {
                                           expr: removeInternalComponents(p.title, replaceClusterMatchers(e.expr)),
                                         }
                                         for e in sp.targets
                                       ] else [],
                                       panels: if std.objectHas(sp, 'panels') then [
                                         ssp {
                                           datasource: selectDatasource(super.datasource),
                                           targets: if std.objectHas(ssp, 'targets') then [
                                             e {
                                               expr: removeInternalComponents(p.title, replaceClusterMatchers(e.expr)),
                                             }
                                             for e in ssp.targets
                                           ] else [],
                                         }
                                         for ssp in sp.panels
                                         if !(isPanelHidden(ssp.title))
                                       ] else [],
                                     }
                                     for sp in p.panels
                                     if !(isPanelHidden(sp.title))
                                   ] else [],
                                   title: if !($._config.ssd.enabled && p.type == 'row') then p.title else
                                     if p.title == 'Distributor' then 'Write Path'
                                     else if p.title == 'Querier' then 'Read Path'
                                     else p.title,
                                 }
                                 for p in super.panels
                                 if !(p.type == 'row' && isRowHidden(p.title)) && !(isPanelHidden(p.title))
                               ],
                             } +
                             $.dashboard('Loki / Operational', uid='operational')
                             // The queries in this dashboard don't make use of the cluster template label selector
                             // but we keep it here to allow selecting a namespace specific to a certain cluster, the
                             // namespace template variable selectors query uses the cluster value.
                             .addLog()
                             .addCluster()
                             .addNamespace()
                             .addTag(),
  },
}
