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
        panels: [
          p {
            targets: if std.objectHas(p, 'targets') then [
              e {
                expr: $.replaceMatchers(e.expr),
              }
              for e in p.targets
            ] else [],
            panels: if std.objectHas(p, 'panels') then [
              sp {
                targets: if std.objectHas(sp, 'targets') then [
                  spe {
                    expr: $.replaceMatchers(spe.expr),
                  }
                  for spe in sp.targets
                ] else [],
                panels: if std.objectHas(sp, 'panels') then [
                  ssp {
                    targets: if std.objectHas(ssp, 'targets') then [
                      sspe {
                        expr: $.replaceMatchers(sspe.expr),
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
