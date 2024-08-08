local utils = import 'mixin-utils/utils.libsonnet';

(import 'dashboard-utils.libsonnet') {
  local compactor_pod_matcher = if $._config.meta_monitoring.enabled
  then 'pod=~"(compactor.*|%s-backend.*|loki-single-binary)"' % $._config.ssd.pod_prefix_matcher
  else if $._config.ssd.enabled then 'container="loki", pod=~"%s-read.*"' % $._config.ssd.pod_prefix_matcher else 'container="compactor"',
  local compactor_job_matcher = if $._config.meta_monitoring.enabled
  then '"(compactor|%s-backend.*|loki-single-binary)"' % $._config.ssd.pod_prefix_matcher
  else if $._config.ssd.enabled then '%s-backend' % $._config.ssd.pod_prefix_matcher else 'compactor',
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
            $.newQueryPanel('Compact Tables Operations Duration', 's') +
            $.queryPanel(['loki_boltdb_shipper_compact_tables_operation_duration_seconds{%s}' % $.namespaceMatcher()], ['duration']),
          )
        )
        .addRow(
          $.row('')
          .addPanel(
            $.newQueryPanel('Number of times Tables were skipped during Compaction') +
            $.queryPanel(['sum(increase(loki_compactor_skipped_compacting_locked_table_total{%s}[$__range]))' % $.namespaceMatcher()], ['{{table_name}}']),
          )
          .addPanel(
            $.newQueryPanel('Compact Tables Operations Per Status') +
            $.queryPanel(['sum by (status)(rate(loki_boltdb_shipper_compact_tables_operation_total{%s}[$__rate_interval]))' % $.namespaceMatcher()], ['{{success}}']),
          )
        )
        .addRow(
          $.row('Retention')
          .addPanel(
            $.fromNowPanel('Last Mark Operation Success', 'loki_compactor_apply_retention_last_successful_run_timestamp_seconds')
          )
          .addPanel(
            $.newQueryPanel('Mark Operations Duration', 's') +
            $.queryPanel(['loki_compactor_apply_retention_operation_duration_seconds{%s}' % $.namespaceMatcher()], ['duration']),
          )
          .addPanel(
            $.newQueryPanel('Mark Operations Per Status') +
            $.queryPanel(['sum by (status)(rate(loki_compactor_apply_retention_operation_total{%s}[$__rate_interval]))' % $.namespaceMatcher()], ['{{success}}']),
          )
        )
        .addRow(
          $.row('Per Table Marker')
          .addPanel(
            $.newQueryPanel('Processed Tables Per Action') +
            $.queryPanel(['count by(action)(loki_boltdb_shipper_retention_marker_table_processed_total{%s})' % $.namespaceMatcher()], ['{{action}}']) +
            $.withStacking,
          )
          .addPanel(
            $.newQueryPanel('Modified Tables') +
            $.queryPanel(['count by(table,action)(loki_boltdb_shipper_retention_marker_table_processed_total{%s , action=~"modified|deleted"})' % $.namespaceMatcher()], ['{{table}}-{{action}}']) +
            $.withStacking,
          )
          .addPanel(
            $.newQueryPanel('Marks Creation Rate Per Table') +
            $.queryPanel(['sum by (table)(rate(loki_boltdb_shipper_retention_marker_count_total{%s}[$__rate_interval])) >0' % $.namespaceMatcher()], ['{{table}}']) +
            $.withStacking,
          )
        )
        .addRow(
          $.row('')
          .addPanel(
            $.newQueryPanel('Marked Chunks (24h)') +
            $.statPanel('sum (increase(loki_boltdb_shipper_retention_marker_count_total{%s}[24h]))' % $.namespaceMatcher(), 'short')
          )
          .addPanel(
            $.newQueryPanel('Mark Table Latency') +
            $.latencyPanel('loki_boltdb_shipper_retention_marker_table_processed_duration_seconds', '{%s}' % $.namespaceMatcher())
          )
        )
        .addRow(
          $.row('Sweeper')
          .addPanel(
            $.newQueryPanel('Delete Chunks (24h)') +
            $.statPanel('sum (increase(loki_boltdb_shipper_retention_sweeper_chunk_deleted_duration_seconds_count{%s}[24h]))' % $.namespaceMatcher(), 'short')
          )
          .addPanel(
            $.newQueryPanel('Delete Latency') +
            $.latencyPanel('loki_boltdb_shipper_retention_sweeper_chunk_deleted_duration_seconds', '{%s}' % $.namespaceMatcher())
          )
        )
        .addRow(
          $.row('')
          .addPanel(
            $.newQueryPanel('Sweeper Lag', 's') +
            $.queryPanel(['time() - (loki_boltdb_shipper_retention_sweeper_marker_file_processing_current_time{%s} > 0)' % $.namespaceMatcher()], ['lag']),
          )
          .addPanel(
            $.newQueryPanel('Marks Files to Process') +
            $.queryPanel(['sum(loki_boltdb_shipper_retention_sweeper_marker_files_current{%s})' % $.namespaceMatcher()], ['count']),
          )
          .addPanel(
            $.newQueryPanel('Delete Rate Per Status') +
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
