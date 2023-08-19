local g = import 'grafana-builder/grafana.libsonnet';
local utils = import 'mixin-utils/utils.libsonnet';

(import 'dashboard-utils.libsonnet') {
  local compactor_matcher = if $._config.ssd.enabled then '%s-read' % $._config.ssd.pod_prefix_matcher else 'compactor',
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
            g.panel('# of Delete Requests (received - processed) ') +
            g.queryPanel('(loki_compactor_delete_requests_received_total{%s} or on() vector(0)) - on () (loki_compactor_delete_requests_processed_total{%s} or on () vector(0))' % [$.namespaceMatcher(), $.namespaceMatcher()], 'in progress'),
          )
          .addPanel(
            g.panel('Delete Requests Received / Day') +
            g.queryPanel('sum(increase(loki_compactor_delete_requests_received_total{%s}[1d]))' % $.namespaceMatcher(), 'received'),
          )
          .addPanel(
            g.panel('Delete Requests Processed / Day') +
            g.queryPanel('sum(increase(loki_compactor_delete_requests_processed_total{%s}[1d]))' % $.namespaceMatcher(), 'processed'),
          )
        ).addRow(
          g.row('Compactor')
          .addPanel(
            g.panel('Compactor CPU usage') +
            g.queryPanel('node_namespace_pod_container:container_cpu_usage_seconds_total:sum_irate{%s, container="compactor"}' % $.namespaceMatcher(), '{{pod}}'),
          )
          .addPanel(
            g.panel('Compactor memory usage (MiB)') +
            g.queryPanel('go_memstats_heap_inuse_bytes{%s, container="compactor"} / 1024 / 1024 ' % $.namespaceMatcher(), ' {{pod}} '),
          )
          .addPanel(
            g.panel('Compaction run duration (seconds)') +
            g.queryPanel('loki_boltdb_shipper_compact_tables_operation_duration_seconds{%s}' % $.namespaceMatcher(), '{{pod}}'),
          )
        ).addRow(
          g.row('Deletion metrics')
          .addPanel(
            g.panel('Failures in Loading Delete Requests / Hour') +
            g.queryPanel('sum(increase(loki_compactor_load_pending_requests_attempts_total{status="fail", %s}[1h]))' % $.namespaceMatcher(), 'failures'),
          )
          .addPanel(
            g.panel('Lines Deleted / Sec') +
            g.queryPanel('sum(rate(loki_compactor_deleted_lines{' + $._config.per_cluster_label + '=~"$cluster",job=~"$namespace/%s"}[$__rate_interval])) by (user)' % compactor_matcher, '{{user}}'),
          )
        ).addRow(
          g.row('List of deletion requests')
          .addPanel(
            $.logPanel('In progress/finished', '{%s, container="compactor"} |~ "Started processing delete request|delete request for user marked as processed" | logfmt | line_format "{{.ts}} user={{.user}} delete_request_id={{.delete_request_id}} msg={{.msg}}" ' % $.namespaceMatcher()),
          )
          .addPanel(
            $.logPanel('Requests', '{%s, container="compactor"} |~ "delete request for user added" | logfmt | line_format "{{.ts}} user={{.user}} query=\'{{.query}}\'"' % $.namespaceMatcher()),
          )
        ),
    },
}
