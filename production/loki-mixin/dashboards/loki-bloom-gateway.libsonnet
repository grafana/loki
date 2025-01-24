local raw = (import './dashboard-bloom-gateway.json');

// !--- HOW TO UPDATE THIS DASHBOARD ---!
// 1. Export the dashboard from Grafana as JSON
//    !NOTE: Make sure you collapse all rows but the (first) Overview row.
// 2. Copy the JSON into `dashboard-bloom-gateway.json`
// 3. Delete the `id` and `templating` fields from the JSON
(import 'dashboard-utils.libsonnet') {
  grafanaDashboards+:: if !$._config.blooms.enabled then {} else {
    'loki-bloom-gateway.json':
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

        panels: [
          p {
            targets: if std.objectHas(p, 'targets') then [
              e {
                expr: replaceClusterMatchers(e.expr),
              }
              for e in p.targets
            ] else [],
            panels: if std.objectHas(p, 'panels') then [
              sp {
                targets: if std.objectHas(sp, 'targets') then [
                  spe {
                    expr: replaceClusterMatchers(spe.expr),
                  }
                  for spe in sp.targets
                ] else [],
                panels: if std.objectHas(sp, 'panels') then [
                  ssp {
                    targets: if std.objectHas(ssp, 'targets') then [
                      sspe {
                        expr: replaceClusterMatchers(sspe.expr),
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
      + $.dashboard('Loki / Bloom Gateway', uid='bloom-gateway')
        .addCluster()
        .addNamespace()
        .addLog()
        .addTag(),
  },
}
