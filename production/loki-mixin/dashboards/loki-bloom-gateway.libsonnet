local raw = (import './dashboard-bloom-gateway.json');

// !--- HOW TO UPDATE THIS DASHBOARD ---!
// 1. Export the dashboard from Grafana as JSON
//    !NOTE: Make sure you collapse all rows but the (first) Overview row.
// 2. Copy the JSON into `dashboard-bloom-gateway.json`
// 3. Delete the `id` and `templating` fields from the JSON
(import 'dashboard-utils.libsonnet') {
  grafanaDashboards+:
    {
      'loki-bloom-gateway.json':
        raw +
        $.dashboard('Loki / Bloom Gateway', uid='bloom-gateway')
        .addCluster()
        .addNamespace()
        .addLog()
        .addTag(),
    },
}
