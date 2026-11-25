// The grafana-databases-lib instance shared by Loki's dashboards, configured from $._config.
// It is merged into dashboard-utils.libsonnet, so every dashboard can use $._databases. Each area
// of the library (autoscaling, ...) adds its own config mixin below.
local dblib = import 'grafana-databases-lib/main.libsonnet';

{
  _databases::
    dblib.new({
      selector: $.namespaceMatcher(),
      clusterLabel: $._config.per_cluster_label,
      namespaceLabel: $._config.per_namespace_label,
      groupBy: $._config.cluster_labels,
    })
    // Autoscaling panels and rows, one set per component in $._config.autoscaling_hpa_names.
    + dblib.autoscaling.withConfigMixin({
      hpaPrefix: $._config.autoscaling_hpa_prefix,
      components: {
        [name]: { hpaName: $._config.autoscaling_hpa_prefix + $._config.autoscaling_hpa_names[name] }
        for name in std.objectFields($._config.autoscaling_hpa_names)
      },
    }),
}
