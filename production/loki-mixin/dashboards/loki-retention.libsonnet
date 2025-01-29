local selector = (import '../selectors.libsonnet').new;

local podSelectors = {
  base: selector(false).cluster().namespace().build(),
  compactor: selector().pod('compactor').build(),
};
local jobSelectors = {
  base: selector(false).cluster().namespace().build(),
  compactor: selector().job('compactor').build(),
};

(import 'dashboard-utils.libsonnet') {
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
            $.CPUUsagePanel('CPU', podSelectors.compactor),
          )
          .addPanel(
            $.memoryWorkingSetPanel('Memory (workingset)', podSelectors.compactor),
          )
          .addPanel(
            $.goHeapInUsePanel('Memory (go heap inuse)', jobSelectors.compactor),
          )
        )
        .addRow(
          $.row('Compaction')
          .addPanel(
            $.fromNowPanel(
              'Last Compact Tables Operation Success',
              'loki_boltdb_shipper_compact_tables_operation_last_successful_run_timestamp_seconds{%s}' % jobSelectors.compactor,
            ),
          )
          .addPanel(
            $.newQueryPanel('Compact Tables Operations Duration', 's') +
            $.queryPanel(['loki_boltdb_shipper_compact_tables_operation_duration_seconds{%s}' % jobSelectors.compactor], ['duration']),
          )
        )
        .addRow(
          $.row('')
          .addPanel(
            $.newQueryPanel('Number of times Tables were skipped during Compaction') +
            $.queryPanel(['sum(loki_compactor_locked_table_successive_compaction_skips{%s})' % jobSelectors.compactor], ['{{table_name}}']),
          )
          .addPanel(
            $.newQueryPanel('Compact Tables Operations Per Status') +
            $.queryPanel(
              [
                |||
                  sum by (status) (
                    rate(
                      loki_boltdb_shipper_compact_tables_operation_total{%s}
                      [$__rate_interval]
                    )
                  )
                ||| % jobSelectors.compactor,
              ],
              ['{{success}}'],
            ),
          )
        )
        .addRow(
          $.row('Retention')
          .addPanel(
            $.fromNowPanel(
              'Last Mark Operation Success',
              'loki_compactor_apply_retention_last_successful_run_timestamp_seconds{%s}' % jobSelectors.compactor,
            ),
          )
          .addPanel(
            $.newQueryPanel('Mark Operations Duration', 's') +
            $.queryPanel(['loki_compactor_apply_retention_operation_duration_seconds{%s}' % jobSelectors.compactor], ['duration']),
          )
          .addPanel(
            $.newQueryPanel('Mark Operations Per Status') +
            $.queryPanel(
              [
                |||
                  sum by (status) (
                    rate(
                      loki_compactor_apply_retention_operation_total{%s}
                      [$__rate_interval]
                    )
                  )
                ||| % jobSelectors.compactor,
              ],
              ['{{success}}'],
            ),
          )
        )
        .addRow(
          $.row('Per Table Marker')
          .addPanel(
            $.newQueryPanel('Processed Tables Per Action') +
            $.queryPanel(
              [
                |||
                  count by (action) (
                    loki_boltdb_shipper_retention_marker_table_processed_total{%s}
                  )
                ||| % jobSelectors.compactor,
              ],
              ['{{action}}'],
            ) +
            $.withStacking,
          )
          .addPanel(
            $.newQueryPanel('Modified Tables') +
            $.queryPanel(
              [
                |||
                  count by (table, action) (
                    loki_boltdb_shipper_retention_marker_table_processed_total{%s, action=~"modified|deleted"}
                  )
                ||| % jobSelectors.compactor,
              ],
              ['{{table}}-{{action}}'],
            ) +
            $.withStacking,
          )
          .addPanel(
            $.newQueryPanel('Marks Creation Rate Per Table') +
            $.queryPanel(
              [
                |||
                  sum by (table) (
                    rate(
                      loki_boltdb_shipper_retention_marker_count_total{%s}
                      [$__rate_interval]
                    )
                  )
                ||| % jobSelectors.compactor,
              ],
              ['{{table}}'],
            ) +
            $.withStacking,
          )
        )
        .addRow(
          $.row('')
          .addPanel(
            $.newQueryPanel('Marked Chunks (24h)') +
            $.statPanel(
              |||
                sum (
                  increase(
                    loki_boltdb_shipper_retention_marker_count_total{%s}
                    [24h]
                  )
                )
              ||| % jobSelectors.compactor,
              'short',
            )
          )
          .addPanel(
            $.newQueryPanel('Mark Table Latency') +
            $.latencyPanel(
              'loki_boltdb_shipper_retention_marker_table_processed_duration_seconds',
              '{%s}' % jobSelectors.compactor,
            )
          )
        )
        .addRow(
          $.row('Sweeper')
          .addPanel(
            $.newQueryPanel('Delete Chunks (24h)') +
            $.statPanel(
              |||
                sum (
                  increase(
                    loki_boltdb_shipper_retention_sweeper_chunk_deleted_duration_seconds_count{%s}
                    [24h]
                  )
                )
              ||| % jobSelectors.compactor,
              'short',
            )
          )
          .addPanel(
            $.newQueryPanel('Delete Latency') +
            $.latencyPanel(
              'loki_boltdb_shipper_retention_sweeper_chunk_deleted_duration_seconds',
              '{%s}' % jobSelectors.compactor,
            )
          )
        )
        .addRow(
          $.row('')
          .addPanel(
            $.newQueryPanel('Sweeper Lag', 's') +
            $.queryPanel(
              [
                |||
                  time() - (
                    loki_boltdb_shipper_retention_sweeper_marker_file_processing_current_time{%s}
                    > 0
                  )
                ||| % jobSelectors.compactor,
              ],
              ['lag'],
            )
          )
          .addPanel(
            $.newQueryPanel('Marks Files to Process') +
            $.queryPanel(
              [
                |||
                  sum(
                    loki_boltdb_shipper_retention_sweeper_marker_files_current{%s}
                  )
                ||| % jobSelectors.compactor,
              ],
              ['count'],
            )
          )
          .addPanel(
            $.newQueryPanel('Delete Rate Per Status') +
            $.queryPanel(
              [
                |||
                  sum by (status) (
                    rate(
                      loki_boltdb_shipper_retention_sweeper_chunk_deleted_duration_seconds_count{%s}
                      [$__rate_interval]
                    )
                  )
                ||| % jobSelectors.compactor,
              ],
              ['{{status}}'],
            )
          )
        )
        .addRow(
          $.row('Logs')
          .addPanel(
            $.logPanel('Compactor Logs', '{%s}' % jobSelectors.compactor),
          )
        ),
    },
}
