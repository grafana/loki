local g = import 'grafana-builder/grafana.libsonnet';
local utils = import 'mixin-utils/utils.libsonnet';

(import 'dashboard-utils.libsonnet') {
  local compactor_matcher = 'pod=~"(compactor|%s-backend.*|loki-single-binary)"' % $._config.ssd.pod_prefix_matcher,
  grafanaDashboards+::
    {
      'loki-deletion.json':
        ($.dashboard('Loki / Deletion', uid='deletion'))
        .addCluster()
        .addNamespace()
        .addTag()
        .addLog()
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
            $.newQueryPanel('# of Delete Requests (received - processed) ') +
            g.queryPanel('(loki_compactor_delete_requests_received_total{%s} or on() vector(0)) - on () (loki_compactor_delete_requests_processed_total{%s} or on () vector(0))' % [$.namespaceMatcher(), $.namespaceMatcher()], 'in progress'),
          )
          .addPanel(
            $.newQueryPanel('Delete Requests Received / Day') +
            g.queryPanel('sum(increase(loki_compactor_delete_requests_received_total{%s}[1d]))' % $.namespaceMatcher(), 'received'),
          )
          .addPanel(
            $.newQueryPanel('Delete Requests Processed / Day') +
            g.queryPanel('sum(increase(loki_compactor_delete_requests_processed_total{%s}[1d]))' % $.namespaceMatcher(), 'processed'),
          )
        ).addRow(
          g.row('Compactor')
          .addPanel(
            $.newQueryPanel('Compactor CPU usage') +
            g.queryPanel('node_namespace_pod_container:container_cpu_usage_seconds_total:sum_irate{%s, %s}' % [$.namespaceMatcher(), compactor_matcher], '{{pod}}'),
          )
          .addPanel(
            $.newQueryPanel('Compactor memory usage (MiB)') +
            g.queryPanel('go_memstats_heap_inuse_bytes{%s, %s} / 1024 / 1024 ' % [$.namespaceMatcher(), compactor_matcher], ' {{pod}} '),
          )
          .addPanel(
            $.newQueryPanel('Compaction run duration (seconds)') +
            g.queryPanel('loki_boltdb_shipper_compact_tables_operation_duration_seconds{%s}' % $.namespaceMatcher(), '{{pod}}'),
          )
        ).addRow(
          g.row('Deletion metrics')
          .addPanel(
            $.newQueryPanel('Failures in Loading Delete Requests / Hour') +
            g.queryPanel('sum(increase(loki_compactor_load_pending_requests_attempts_total{status="fail", %s}[1h]))' % $.namespaceMatcher(), 'failures'),
          )
          .addPanel(
            $.newQueryPanel('Lines Deleted / Sec') +
            g.queryPanel('sum(rate(loki_compactor_deleted_lines{' + $.namespaceMatcher() + ', ' + compactor_matcher + '}[$__rate_interval])) by (user)', '{{user}}'),
          )
        ).addRow(
          g.row('List of deletion requests')
          .addPanel(
            $.logPanel('In progress/finished', '{%s, %s} |~ "Started processing delete request|delete request for user marked as processed" | logfmt | line_format "{{.ts}} user={{.user}} delete_request_id={{.delete_request_id}} msg={{.msg}}" ' % [$.namespaceMatcher(), compactor_matcher]),
          )
          .addPanel(
            $.logPanel('Requests', '{%s, %s} |~ "delete request for user added" | logfmt | line_format "{{.ts}} user={{.user}} query=\'{{.query}}\'"' % [$.namespaceMatcher(), compactor_matcher]),
          )
        ),
    },
}
