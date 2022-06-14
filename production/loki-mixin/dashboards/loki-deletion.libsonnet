local g = import 'grafana-builder/grafana.libsonnet';
local utils = import 'mixin-utils/utils.libsonnet';

(import 'dashboard-utils.libsonnet') {
  grafanaDashboards+::
    {
      'loki-deletion.json':
        ($.dashboard('Loki / Deletion', uid='deletion'))
        .addCluster()
        .addNamespace()
        .addTag()
        .addRow(
          ($.row('Headlines') +
           {
             height: '100px',
             showTitle: false,
           })
          .addPanel(
            $.panel('Number of Pending Requests') +
            $.statPanel('sum(loki_compactor_pending_delete_requests_count{%s})' % $.namespaceMatcher(), format='none')
          )
          .addPanel(
            $.panel('Oldest Pending Request Age') +
            $.statPanel('max(loki_compactor_oldest_pending_delete_request_age_seconds{%s})' % $.namespaceMatcher(), format='dtdurations')
          )
        )
        .addRow(
          g.row('Churn')
          .addPanel(
            g.panel('Delete Requests Received / Day') +
            g.queryPanel('sum(increase(loki_compactor_delete_requests_received_total{%s}[1d]))' % $.namespaceMatcher(), 'received'),
          )
          .addPanel(
            g.panel('Delete Requests Processed / Day') +
            g.queryPanel('sum(increase(loki_compactor_delete_requests_processed_total{%s}[1d]))' % $.namespaceMatcher(), 'processed'),
          )
        ).addRow(
          g.row('Failures')
          .addPanel(
            g.panel('Failures in Loading Delete Requests / Hour') +
            g.queryPanel('sum(increase(loki_compactor_load_pending_requests_attempts_total{status="fail", %s}[1h]))' % $.namespaceMatcher(), 'failures'),
          )
        ).addRow(
          g.row('Deleted lines')
          .addPanel(
            g.panel('Lines Deleted / Sec') +
            g.queryPanel('sum(rate(loki_compactor_deleted_lines{' + $._config.per_cluster_label + '=~"$cluster",job=~"$namespace/compactor"}[$__rate_interval])) by (user)', '{{user}}'),
          )
        ),
    },
}
