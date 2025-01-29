local g = import 'grafana-builder/grafana.libsonnet';
local selector = (import '../selectors.libsonnet').new;

local selectors = {
  base: selector(false).cluster().namespace().build(),
  compactor: selector().compactor().build(),
};

(import 'dashboard-utils.libsonnet') {
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
            $.statPanel(
              |||
                sum(
                  loki_compactor_pending_delete_requests_count{%s}
                )
              ||| % selectors.base,
              format='none',
            )
          )
          .addPanel(
            $.panel('Oldest Pending Request Age') +
            $.statPanel(
              |||
                max(
                  loki_compactor_oldest_pending_delete_request_age_seconds{%s}
                )
              ||| % selectors.base,
              format='dtdurations',
            )
          )
        )
        .addRow(
          g.row('Churn')
          .addPanel(
            $.newQueryPanel('# of Delete Requests (received - processed) ') +
            g.queryPanel(
              |||
                (
                  loki_compactor_delete_requests_received_total{%(matcher)s}
                  or
                  on() vector(0)
                )
                - on ()
                (
                  loki_compactor_delete_requests_processed_total{%(matcher)s}
                  or
                  on () vector(0)
                )
              ||| % { matcher: selectors.base },
              'in progress',
            )
          )
          .addPanel(
            $.newQueryPanel('Delete Requests Received / Day') +
            g.queryPanel(
              |||
                sum(
                  increase(
                    loki_compactor_delete_requests_received_total{%s}[1d]
                  )
                )
              ||| % selectors.base,
              'received',
            )
          )
          .addPanel(
            $.newQueryPanel('Delete Requests Processed / Day') +
            g.queryPanel(
              |||
                sum(
                  increase(
                    loki_compactor_delete_requests_processed_total{%s}[1d]
                  )
                )
              ||| % selectors.base,
              'processed',
            )
          )
        )
        .addRow(
          g.row('Compactor')
          .addPanel(
            $.newQueryPanel('Compactor CPU usage') +
            g.queryPanel(
              'node_namespace_pod_container:container_cpu_usage_seconds_total:sum_irate{%s}' % selectors.compactor,
              '{{pod}}',
            )
          )
          .addPanel(
            $.newQueryPanel('Compactor memory usage (MiB)') +
            g.queryPanel(
              'go_memstats_heap_inuse_bytes{%s} / 1024 / 1024' % selectors.compactor,
              '{{pod}}',
            )
          )
          .addPanel(
            $.newQueryPanel('Compaction run duration (seconds)') +
            g.queryPanel(
              'loki_boltdb_shipper_compact_tables_operation_duration_seconds{%s}' % selectors.compactor,
              '{{pod}}',
            )
          )
        )
        .addRow(
          g.row('Deletion metrics')
          .addPanel(
            $.newQueryPanel('Failures in Loading Delete Requests / Hour') +
            g.queryPanel(
              |||
                sum(
                  increase(
                    loki_compactor_load_pending_requests_attempts_total{status="fail", %s}[1h]
                  )
                )
              ||| % selectors.base,
              'failures',
            )
          )
          .addPanel(
            $.newQueryPanel('Lines Deleted / Sec') +
            g.queryPanel(
              |||
                sum(
                  rate(
                    loki_compactor_deleted_lines{%s}[$__rate_interval]
                  )
                )
              ||| % selectors.compactor,
              '{{user}}',
            )
          )
        )
        .addRow(
          g.row('List of deletion requests')
          .addPanel(
            $.logPanel(
              'In progress/finished',
              |||
                {%s}
                |~ `Started processing delete request|delete request for user marked as processed`
                | logfmt ts, user, delete_request_id, msg
                | line_format `{{.ts}} user={{.user}} delete_request_id={{.delete_request_id}} msg={{.msg}}`
              ||| % selectors.compactor
            ),
          )
          .addPanel(
            $.logPanel(
              'Requests',
              |||
                {%s}
                |~ `delete request for user added`
                | logfmt ts, user, query
                | line_format `{{.ts}} user={{.user}} query="{{.query}}"`
              ||| % selectors.compactor,
            )
          )
        ),
    },
}
