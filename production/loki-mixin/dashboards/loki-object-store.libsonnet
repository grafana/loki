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
      dashboard.dashboard('Loki / Object Store Thanos', uid='object-store')
      .addCluster()
      .addNamespace()
      .addTag()
      .addRow(
        row.new('Operations')
        .addPanel(
          $.newQueryPanel('RPS / operation', 'reqps') +
          $.queryPanel(
            'sum by(operation) (rate(loki_objstore_bucket_operations_total{%s}[$__rate_interval]))' % cluster_namespace_matcher,
            '{{operation}}'
          )
        )
        .addPanel(
          $.newQueryPanel('Error rate / operation', 'reqps') +
          $.queryPanel(
            'sum by(operation) (rate(loki_objstore_bucket_operation_failures_total{%s}[$__rate_interval])) > 0' % cluster_namespace_matcher,
            '{{operation}}'
          )
        )
        .addPanel(
          $.newQueryPanel('Transport error rate / method and status code', 'reqps') +
          $.queryPanel(
            'sum by (method, status_code) (rate(loki_objstore_bucket_transport_requests_total{%s, status_code!~"2.."}[$__rate_interval])) > 0' % cluster_namespace_matcher,
            '{{method}} - {{status_code}}'
          )
        )
      )
      .addRow(
        row.new('')
        .addPanel(
          $.newQueryPanel('Op: Get', 'ms') +
          $.latencyPanel('loki_objstore_bucket_operation_duration_seconds', '{%s,operation="get"}' % cluster_namespace_matcher)
        )
        .addPanel(
          $.newQueryPanel('Op: GetRange', 'ms') +
          $.latencyPanel('loki_objstore_bucket_operation_duration_seconds', '{%s,operation="get_range"}' % cluster_namespace_matcher)
        )
        .addPanel(
          $.newQueryPanel('Op: Exists', 'ms') +
          $.latencyPanel('loki_objstore_bucket_operation_duration_seconds', '{%s,operation="exists"}' % cluster_namespace_matcher)
        )
      )
      .addRow(
        row.new('')
        .addPanel(
          $.newQueryPanel('Op: Attributes', 'ms') +
          $.latencyPanel('loki_objstore_bucket_operation_duration_seconds', '{%s,operation="attributes"}' % cluster_namespace_matcher)
        )
        .addPanel(
          $.newQueryPanel('Op: Upload', 'ms') +
          $.latencyPanel('loki_objstore_bucket_operation_duration_seconds', '{%s,operation="upload"}' % cluster_namespace_matcher)
        )
        .addPanel(
          $.newQueryPanel('Op: Delete', 'ms') +
          $.latencyPanel('loki_objstore_bucket_operation_duration_seconds', '{%s,operation="delete"}' % cluster_namespace_matcher)
        )
      ),
  },
}
