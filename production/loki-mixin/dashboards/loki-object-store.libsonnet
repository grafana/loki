local grafana = import 'grafonnet/grafana.libsonnet';
local row = grafana.row;

{
  grafanaDashboards+:: {
    local cluster_namespace_matcher = 'cluster="$cluster", namespace=~"$namespace"',
    local dashboard = (
      (import 'dashboard-utils.libsonnet') + {
        _config+:: $._config,
      }
    ),
    'loki_thanos_object_storage.json':
      dashboard.dashboard('Loki / Object Store Thanos')
      .addCluster()
      .addNamespace()
      .addTag()
      .addRow(
        row.new('Operations')
        .addPanel(
          grafana.graphPanel.new(
            'RPS / operation',
            format='reqps',
            span=4,
            datasource='$datasource',
          ).addTargets(
            [
              grafana.prometheus.target('sum by(operation) (rate(loki_objstore_bucket_operations_total{%s}[$__rate_interval]))' % cluster_namespace_matcher),
            ],
          )
        )
        .addPanel(
          grafana.graphPanel.new(
            'Error rate / operation',
            format='reqps',
            span=4,
            datasource='$datasource',
          ).addTargets(
            [
              grafana.prometheus.target('sum by(operation) (rate(loki_objstore_bucket_operation_failures_total{%s}[$__rate_interval])) > 0' % cluster_namespace_matcher),
            ],
          )
        )
        .addPanel(
          grafana.graphPanel.new(
            'Transport error rate / method and status code',
            format='reqps',
            span=4,
            datasource='$datasource',
          ).addTargets(
            [
              grafana.prometheus.target('sum  by (method, status_code) (rate(loki_objstore_bucket_transport_requests_total{%s, status_code!~"2.."}[$__rate_interval])) > 0' % cluster_namespace_matcher),
            ],
          )
        )
      )
      .addRow(
        row.new('')
        .addPanel(
          dashboard.timeseriesPanel('Op: Get') +
          dashboard.latencyPanel('loki_objstore_bucket_operation_duration_seconds', '{%s,operation="get"}' % cluster_namespace_matcher) +
          { fieldConfig: { defaults: { unit: 'ms' } } },
        )
        .addPanel(
          dashboard.timeseriesPanel('Op: GetRange') +
          dashboard.latencyPanel('loki_objstore_bucket_operation_duration_seconds', '{%s,operation="get_range"}' % cluster_namespace_matcher) +
          { fieldConfig: { defaults: { unit: 'ms' } } },
        )
        .addPanel(
          dashboard.timeseriesPanel('Op: Exists') +
          dashboard.latencyPanel('loki_objstore_bucket_operation_duration_seconds', '{%s,operation="exists"}' % cluster_namespace_matcher) +
          { fieldConfig: { defaults: { unit: 'ms' } } },
        )
      )
      .addRow(
        row.new('')
        .addPanel(
          dashboard.timeseriesPanel('Op: Attributes') +
          dashboard.latencyPanel('loki_objstore_bucket_operation_duration_seconds', '{%s,operation="attributes"}' % cluster_namespace_matcher) +
          { fieldConfig: { defaults: { unit: 'ms' } } },
        )
        .addPanel(
          dashboard.timeseriesPanel('Op: Upload') +
          dashboard.latencyPanel('loki_objstore_bucket_operation_duration_seconds', '{%s,operation="upload"}' % cluster_namespace_matcher) +
          { fieldConfig: { defaults: { unit: 'ms' } } },
        )
        .addPanel(
          dashboard.timeseriesPanel('Op: Delete') +
          dashboard.latencyPanel('loki_objstore_bucket_operation_duration_seconds', '{%s,operation="delete"}' % cluster_namespace_matcher) +
          { fieldConfig: { defaults: { unit: 'ms' } } },
        )
      ),
  },
}
