local utils = import 'mixin-utils/utils.libsonnet';

(import 'dashboard-utils.libsonnet') {
  local compactor_pod_matcher = if $._config.ssd.enabled then 'container="loki", pod=~"%s-read.*"' % $._config.ssd.pod_prefix_matcher else 'container="compactor"',
  local compactor_job_matcher = if $._config.ssd.enabled then '%s-read' % $._config.ssd.pod_prefix_matcher else 'compactor',
  grafanaDashboards+::
    {
      'loki-retention.json':
        ($.dashboard('Loki / Retention', uid='retention'))
        .addCluster()
        .addNamespace()
        .addTag()
        .addLog()
        .addRow(
          $.row('Resource Usage')
          .addPanel(
            $.CPUUsagePanel('CPU', compactor_pod_matcher),
          )
          .addPanel(
            $.memoryWorkingSetPanel('Memory (workingset)', compactor_pod_matcher),
          )
          .addPanel(
            $.goHeapInUsePanel('Memory (go heap inuse)', compactor_job_matcher),
          )

        )
        .addRow(
          $.row('Compaction')
          .addPanel(
            $.fromNowPanel('Last Compact Tables Operation Success', 'loki_boltdb_shipper_compact_tables_operation_last_successful_run_timestamp_seconds')
          )
          .addPanel(
            $.panel('Compact Tables Operations Duration') +
            $.queryPanel(['loki_boltdb_shipper_compact_tables_operation_duration_seconds{%s}' % $.namespaceMatcher()], ['duration']) +
            { yaxes: $.yaxes('s') },
          )
        )
        .addRow(
          $.row('')
          .addPanel(
            $.panel('Number of times Tables were skipped during Compaction') +
            $.queryPanel(['sum(increase(loki_compactor_skipped_compacting_locked_table_total{%s}[$__range]))' % $.namespaceMatcher()], ['{{table_name}}']),
          )
          .addPanel(
            $.panel('Compact Tables Operations Per Status') +
            $.queryPanel(['sum by (status)(rate(loki_boltdb_shipper_compact_tables_operation_total{%s}[$__rate_interval]))' % $.namespaceMatcher()], ['{{success}}']),
          )
        )
        .addRow(
          $.row('Retention')
          .addPanel(
            $.fromNowPanel('Last Mark Operation Success', 'loki_compactor_apply_retention_last_successful_run_timestamp_seconds')
          )
          .addPanel(
            $.panel('Mark Operations Duration') +
            $.queryPanel(['loki_compactor_apply_retention_operation_duration_seconds{%s}' % $.namespaceMatcher()], ['duration']) +
            { yaxes: $.yaxes('s') },
          )
          .addPanel(
            $.panel('Mark Operations Per Status') +
            $.queryPanel(['sum by (status)(rate(loki_compactor_apply_retention_operation_total{%s}[$__rate_interval]))' % $.namespaceMatcher()], ['{{success}}']),
          )
        )
        .addRow(
          $.row('Per Table Marker')
          .addPanel(
            $.panel('Processed Tables Per Action') +
            $.queryPanel(['count by(action)(loki_boltdb_shipper_retention_marker_table_processed_total{%s})' % $.namespaceMatcher()], ['{{action}}']) + $.stack,
          )
          .addPanel(
            $.panel('Modified Tables') +
            $.queryPanel(['count by(table,action)(loki_boltdb_shipper_retention_marker_table_processed_total{%s , action=~"modified|deleted"})' % $.namespaceMatcher()], ['{{table}}-{{action}}']) + $.stack,
          )
          .addPanel(
            $.panel('Marks Creation Rate Per Table') +
            $.queryPanel(['sum by (table)(rate(loki_boltdb_shipper_retention_marker_count_total{%s}[$__rate_interval])) >0' % $.namespaceMatcher()], ['{{table}}']) + $.stack,
          )
        )
        .addRow(
          $.row('')
          .addPanel(
            $.panel('Marked Chunks (24h)') +
            $.statPanel('sum (increase(loki_boltdb_shipper_retention_marker_count_total{%s}[24h]))' % $.namespaceMatcher(), 'short')
          )
          .addPanel(
            $.panel('Mark Table Latency') +
            $.latencyPanel('loki_boltdb_shipper_retention_marker_table_processed_duration_seconds', '{%s}' % $.namespaceMatcher())
          )
        )
        .addRow(
          $.row('Sweeper')
          .addPanel(
            $.panel('Delete Chunks (24h)') +
            $.statPanel('sum (increase(loki_boltdb_shipper_retention_sweeper_chunk_deleted_duration_seconds_count{%s}[24h]))' % $.namespaceMatcher(), 'short')
          )
          .addPanel(
            $.panel('Delete Latency') +
            $.latencyPanel('loki_boltdb_shipper_retention_sweeper_chunk_deleted_duration_seconds', '{%s}' % $.namespaceMatcher())
          )
        )
        .addRow(
          $.row('')
          .addPanel(
            $.panel('Sweeper Lag') +
            $.queryPanel(['time() - (loki_boltdb_shipper_retention_sweeper_marker_file_processing_current_time{%s} > 0)' % $.namespaceMatcher()], ['lag']) + {
              yaxes: $.yaxes({ format: 's', min: null }),
            },
          )
          .addPanel(
            $.panel('Marks Files to Process') +
            $.queryPanel(['sum(loki_boltdb_shipper_retention_sweeper_marker_files_current{%s})' % $.namespaceMatcher()], ['count']),
          )
          .addPanel(
            $.panel('Delete Rate Per Status') +
            $.queryPanel(['sum by (status)(rate(loki_boltdb_shipper_retention_sweeper_chunk_deleted_duration_seconds_count{%s}[$__rate_interval]))' % $.namespaceMatcher()], ['{{status}}']),
          )
        )
        .addRow(
          $.row('Logs')
          .addPanel(
            $.logPanel('Compactor Logs', '{%s}' % $.jobMatcher(compactor_job_matcher)),
          )
        ),
    },
}
