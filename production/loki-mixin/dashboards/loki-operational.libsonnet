local lokiOperational = (import './dashboard-loki-operational.json');
local selector = (import '../selectors.libsonnet').new;

local jobSelectors = {
  cortexGateway: selector().cortexGateway().list(),
  distributor: selector().distributor().list(),
  ingester: selector().job(['ingester', 'partition-ingester']).list(),
  querier: selector().querier().list(),
  queryFrontend: selector().queryFrontend().list(),
  backend: selector().label('job').re('backend.*').list(),
};

local podSelectors = {
  cortexGateway: selector().pod('cortex-gw').list(),
  distributor: selector().pod('distributor').list(),
  ingester: selector().pod(['ingester', 'partition-ingester']).list(),
  querier: selector().pod('querier').list(),
  queryFrontend: selector().pod('query-frontend').list(),
  backend: selector().pod('backend').list(),
};

(import 'dashboard-utils.libsonnet') {
  grafanaDashboards+: {
    local dashboards = self,

    'loki-operational.json': {
                               showAnnotations:: true,
                               showLinks:: true,
                               showMultiCluster:: true,

                               hiddenRows:: [
                                 'Cassandra',
                                 if $._config.ssd.enabled then 'Ingester',
                                 if !$._config.ssd.enabled then 'Backend Path',
                                 if !$._config.operational.memcached then 'Memcached',
                                 if !$._config.operational.consul then 'Consul',
                                 if !$._config.operational.bigTable then 'Big Table',
                                 if !$._config.operational.dynamo then 'Dynamo',
                                 if !$._config.operational.gcs then 'GCS',
                                 if !$._config.operational.s3 then 'S3',
                                 if !$._config.operational.azureBlob then 'Azure Blob',
                                 if !$._config.operational.boltDB then 'BoltDB Shipper',
                               ],

                               hiddenPanels:: if $._config.promtail.enabled then [] else [
                                 'Bad Words',
                               ],
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
                                   if std.length(jobSelectors[matcherId]) > 0 then
                                     std.join(',', ['%(label)s%(op)s"%(value)s"' % matcher for matcher in jobSelectors[matcherId]]) + sep
                                   else error 'no job matchers'
                                 else if matcher == 'pod' then
                                   if std.length(podSelectors[matcherId]) > 0 then
                                     std.join(',', ['%(label)s%(op)s"%(value)s"' % matcher for matcher in podSelectors[matcherId]]) + sep
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
                                       $._config.labels.cluster + '=~"$cluster"'
                                     ),
                                     'cluster="$cluster"',
                                     $._config.labels.cluster + '="$cluster"'
                                   ),
                                   'cluster_job',
                                   $._config.labels.cluster + '_job'
                                 )
                                 else
                                   std.strReplace(
                                     std.strReplace(
                                       std.strReplace(
                                         expr,
                                         ', ' + $._config.labels.cluster + '="$cluster"',
                                         ''
                                       ),
                                       ', ' + $._config.labels.cluster + '=~"$cluster"',
                                       ''
                                     ),
                                     $._config.labels.cluster + '="$cluster",',
                                     ''
                                   ),

                               local replaceBackendMatchers(expr) =
                                 std.strReplace(
                                   std.strReplace(
                                     std.strReplace(
                                       expr,
                                       'pod=~"backend.*"',
                                       matcherStr('backend', matcher='pod', sep='')
                                     ),
                                     'job="$namespace/backend",',
                                     matcherStr('backend')
                                   ),
                                   'job="$namespace/backend"',
                                   std.rstripChars(matcherStr('backend'), ',')
                                 ),

                               local replaceQuerierMatchers(expr) =
                                 std.strReplace(
                                   std.strReplace(
                                     std.strReplace(
                                       expr,
                                       'pod=~"querier.*"',
                                       matcherStr('querier', matcher='pod', sep='')
                                     ),
                                     'job="$namespace/querier",',
                                     matcherStr('querier')
                                   ),
                                   'job="$namespace/querier"',
                                   std.rstripChars(matcherStr('querier'), ',')
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
                                                         'pod=~"ingester.*"',
                                                         matcherStr('ingester', matcher='pod', sep='')
                                                       ),
                                                       'pod=~"distributor.*"',
                                                       matcherStr('distributor', matcher='pod', sep='')
                                                     ),
                                                     'job="$namespace/cortex-gw",',
                                                     matcherStr('cortexGateway')
                                                   ),
                                                   'job="$namespace/cortex-gw"',
                                                   std.rstripChars(matcherStr('cortexGateway'), ',')
                                                 ),
                                                 'job=~"($namespace)/cortex-gw",',
                                                 matcherStr('cortexGateway')
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

                               local replaceAllMatchers(expr) =
                                 replaceBackendMatchers(
                                   replaceQuerierMatchers(
                                     replaceMatchers(expr)
                                   )
                                 ),

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
