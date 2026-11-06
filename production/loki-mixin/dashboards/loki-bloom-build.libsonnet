local raw = (import './dashboard-bloom-build.json');
local template = import 'grafonnet/template.libsonnet';

// !--- HOW TO UPDATE THIS DASHBOARD ---!
// 1. Export the dashboard from Grafana as JSON
//    !NOTE: Make sure you collapse all rows but the (first) Overview row.
// 2. Copy the JSON into `dashboard-bloom-build.json`
// 3. Delete the `id` and `templating` fields from the JSON
(import 'dashboard-utils.libsonnet') {

  local tenantTemplate =
    template.new(
      'tenant',
      '$datasource',
      'label_values(loki_bloomplanner_tenant_tasks_planned{cluster="$cluster", ' + $._config.per_namespace_label + '="$namespace"}, tenant)',
      label='Tenant',
      sort=3,  // numerical ascending
      includeAll=true,
      allValues='.+',
    ),

  grafanaDashboards+:: if !$._config.blooms.enabled then {} else {
    'loki-bloom-build.json':
      raw
      {
        local replaceClusterMatchers(expr) =
          // Replace the recording rules cluster label with the per-cluster label
          std.strReplace(
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
          ),

        // Log queries select streams on namespace and per_component_label instead of job,
        // because log pipelines don't always index job.
        local replaceLogMatchers(expr) =
          std.foldl(
            function(e, component)
              std.strReplace(
                std.strReplace(
                  e,
                  'job="$namespace/%s"' % component,
                  '%s="$namespace", %s' % [$._config.per_namespace_label, $.componentMatcher(component)]
                ),
                'job=~"$namespace/%s"' % component,
                '%s=~"$namespace", %s' % [$._config.per_namespace_label, $.componentMatcher(component)]
              ),
            ['bloom-planner', 'bloom-builder'],
            expr
          ),

        local isLogTarget(target) =
          std.objectHas(target, 'datasource') && std.isObject(target.datasource) && target.datasource.type == 'loki',

        local replaceTargetMatchers(target) =
          replaceClusterMatchers(if isLogTarget(target) then replaceLogMatchers(target.expr) else target.expr),

        panels: [
          p {
            targets: if std.objectHas(p, 'targets') then [
              e {
                expr: replaceTargetMatchers(e),
              }
              for e in p.targets
            ] else [],
            panels: if std.objectHas(p, 'panels') then [
              sp {
                targets: if std.objectHas(sp, 'targets') then [
                  spe {
                    expr: replaceTargetMatchers(spe),
                  }
                  for spe in sp.targets
                ] else [],
                panels: if std.objectHas(sp, 'panels') then [
                  ssp {
                    targets: if std.objectHas(ssp, 'targets') then [
                      sspe {
                        expr: replaceTargetMatchers(sspe),
                      }
                      for sspe in ssp.targets
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
      }
      + $.dashboard('Loki / Bloom Build', uid='bloom-build')
        .addCluster()
        .addNamespace()
        .addLog()
        .addTag()
      + {
        templating+: { list+: [tenantTemplate] },
      },
  },
}
