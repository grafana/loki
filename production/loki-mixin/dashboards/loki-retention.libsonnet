(import 'dashboard-utils.libsonnet') {
  local compactor_pod_matcher = if $._config.meta_monitoring.enabled
  then $._config.per_instance_label + '=~"(.*compactor.*|loki-single-binary)"'
  else 'container="compactor"',
  local compactor_job_matcher = if $._config.meta_monitoring.enabled
  then '(.*compactor|loki-single-binary)'
  else 'compactor',
  // The compactor/retention histogram metrics were renamed from loki_boltdb_shipper_* to loki_tsdb_shipper_*.
  // Combine both names with `or` within each bucket/sum/count query so the panel works in mixed
  // environments where some namespaces still run the old metric name and some run the new one.
  local dualMetricLatencyPanel(metricNameNew, metricNameOld, selector, multiplier='1e3') = {
    nullPointMode: 'null as zero',
    targets: [
      {
        expr: 'histogram_quantile(0.99, sum(rate(%s_bucket%s[$__rate_interval]) or rate(%s_bucket%s[$__rate_interval])) by (le)) * %s' % [metricNameNew, selector, metricNameOld, selector, multiplier],
        format: 'time_series',
        legendFormat: '99th Percentile',
        refId: 'A',
      },
      {
        expr: 'histogram_quantile(0.50, sum(rate(%s_bucket%s[$__rate_interval]) or rate(%s_bucket%s[$__rate_interval])) by (le)) * %s' % [metricNameNew, selector, metricNameOld, selector, multiplier],
        format: 'time_series',
        legendFormat: '50th Percentile',
        refId: 'B',
      },
      {
        expr: 'sum(rate(%s_sum%s[$__rate_interval]) or rate(%s_sum%s[$__rate_interval])) * %s / sum(rate(%s_count%s[$__rate_interval]) or rate(%s_count%s[$__rate_interval]))' % [metricNameNew, selector, metricNameOld, selector, multiplier, metricNameNew, selector, metricNameOld, selector],
        format: 'time_series',
        legendFormat: 'Average',
        refId: 'C',
      },
    ],
    yaxes: $.yaxes('ms'),
  },

  // Dashed, lightly shaded line used for the resource reference lines (requests, limits and the Go
  // runtime's own limits) so they stand out from the per-pod usage series.
  local referenceLine(name, color) =
    $.colorOverride(name, color) + {
      properties+: [
        { id: 'custom.lineWidth', value: 2 },
        { id: 'custom.lineStyle', value: { dash: [10, 10], fill: 'dash' } },
        { id: 'custom.fillOpacity', value: 10 },
      ],
    },

  local resourcePanel(title, unit, queries, legends, overrides, description=null) =
    $.newQueryPanel(title, unit) +
    $.queryPanel(queries, legends) +
    {
      tooltip: { sort: 2 },  // Sort descending.
      fieldConfig+: { overrides+: overrides },
    } + (if description == null then {} else { description: description }),

  // CPU usage with the Kubernetes request and limit, plus GOMAXPROCS to show how many threads the
  // Go runtime schedules on (it should not exceed the CPU limit).
  local cpuPanel(title, matcher, goMatcher) =
    resourcePanel(
      title,
      'short',
      [
        'sum by(pod) (rate(container_cpu_usage_seconds_total{%s, %s}[$__rate_interval]))' % [$.namespaceMatcher(), matcher],
        'min(kube_pod_container_resource_requests{%s, %s, resource="cpu"} > 0)' % [$.namespaceMatcher(), matcher],
        'min(container_spec_cpu_quota{%s, %s} / container_spec_cpu_period{%s, %s} > 0)' % [$.namespaceMatcher(), matcher, $.namespaceMatcher(), matcher],
        'min(go_sched_gomaxprocs_threads{%s} > 0)' % goMatcher,
      ],
      ['{{pod}}', 'Requests', 'Limits', 'GOMAXPROCS'],
      [
        referenceLine('Requests', 'blue'),
        referenceLine('Limits', 'red'),
        referenceLine('GOMAXPROCS', 'orange'),
      ],
    ),

  // Container memory footprint (the figure the OOM killer acts on) with the Kubernetes request and limit.
  local workingSetPanel(title, matcher) =
    resourcePanel(
      title,
      'bytes',
      [
        // "max" rather than "sum": during a rolling update the old pod's stale series would otherwise be added to the new pod's.
        'max by(pod) (container_memory_working_set_bytes{%s, %s})' % [$.namespaceMatcher(), matcher],
        'min(kube_pod_container_resource_requests{%s, %s, resource="memory"} > 0)' % [$.namespaceMatcher(), matcher],
        'min(container_spec_memory_limit_bytes{%s, %s} > 0)' % [$.namespaceMatcher(), matcher],
      ],
      ['{{pod}}', 'Requests', 'Limits'],
      [
        referenceLine('Requests', 'blue'),
        referenceLine('Limits', 'red'),
      ],
      "The container's memory footprint, used to trigger Out-Of-Memory (OOM) kills.",
    ),

  // Live heap objects with the Kubernetes request and limit, plus GOMEMLIMIT, the soft limit at which
  // the Go garbage collector becomes more aggressive.
  local liveObjectsPanel(title, matcher, goMatcher) =
    resourcePanel(
      title,
      'bytes',
      [
        'sum by(%s) (go_memstats_heap_alloc_bytes{%s})' % [$._config.per_instance_label, goMatcher],
        'min(kube_pod_container_resource_requests{%s, %s, resource="memory"} > 0)' % [$.namespaceMatcher(), matcher],
        'min(container_spec_memory_limit_bytes{%s, %s} > 0)' % [$.namespaceMatcher(), matcher],
        // go_gc_gomemlimit_bytes is greater than 9e18 (math.MaxInt64) when GOMEMLIMIT is not set.
        'min(go_gc_gomemlimit_bytes{%s} < 9e18)' % goMatcher,
      ],
      ['{{%s}}' % $._config.per_instance_label, 'Requests', 'Limits', 'GOMEMLIMIT'],
      [
        referenceLine('Requests', 'blue'),
        referenceLine('Limits', 'red'),
        referenceLine('GOMEMLIMIT', 'purple'),
      ],
      'The total amount of heap memory used by live objects.',
    ),

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
            cpuPanel('CPU', compactor_pod_matcher, $.jobMatcher(compactor_job_matcher)),
          )
          .addPanel(
            liveObjectsPanel('Memory (live objects)', compactor_pod_matcher, $.jobMatcher(compactor_job_matcher)),
          )
          .addPanel(
            workingSetPanel('Memory (workingset)', compactor_pod_matcher),
          )
        )
        .addRow(
          $.row('Object Store')
          .addPanel(
            $.newQueryPanel('Operations / sec', 'reqps') +
            $.queryPanel(
              'sum by(operation) (rate(loki_objstore_bucket_operations_total{%s}[$__rate_interval]))' % $.jobMatcher(compactor_job_matcher),
              '{{operation}}'
            ) +
            $.withStacking,
          )
          .addPanel(
            $.newQueryPanel('Errors / sec', 'reqps') +
            $.queryPanel(
              'sum by(operation) (rate(loki_objstore_bucket_operation_failures_total{%s}[$__rate_interval])) > 0' % $.jobMatcher(compactor_job_matcher),
              '{{operation}}'
            ),
          )
        )
        .addRow(
          $.row('Compaction')
          .addPanel(
            $.fromNowPanel(
              'Last Compact Tables Operation Success',
              [
                'loki_tsdb_shipper_compact_tables_operation_last_successful_run_timestamp_seconds',
                'loki_boltdb_shipper_compact_tables_operation_last_successful_run_timestamp_seconds',
              ],
            )
          )
          .addPanel(
            $.newQueryPanel('Compact Tables Operations Duration', 's') +
            $.queryPanel(
              [
                'loki_tsdb_shipper_compact_tables_operation_duration_seconds{%s}' % $.namespaceMatcher(),
                'loki_boltdb_shipper_compact_tables_operation_duration_seconds{%s}' % $.namespaceMatcher(),
              ],
              [
                'duration',
                'duration',
              ]
            ),
          )
        )
        .addRow(
          $.row('')
          .addPanel(
            $.newQueryPanel('Number of times Tables were skipped during Compaction') +
            $.queryPanel(
              ['sum(loki_compactor_locked_table_successive_compaction_skips{%s})' % $.namespaceMatcher()],
              ['{{table_name}}']
            ),
          )
          .addPanel(
            $.newQueryPanel('Compact Tables Operations Per Status') +
            $.queryPanel(
              [
                'sum by (status) (rate(loki_tsdb_shipper_compact_tables_operation_total{%s}[$__rate_interval]))' % $.namespaceMatcher(),
                'sum by (status) (rate(loki_boltdb_shipper_compact_tables_operation_total{%s}[$__rate_interval]))' % $.namespaceMatcher(),
              ],
              [
                '{{status}}',
                '{{status}}',
              ]
            ),
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
            $.queryPanel(
              [
                'count by (action) (loki_tsdb_shipper_retention_marker_table_processed_total{%s})' % $.namespaceMatcher(),
                'count by (action) (loki_boltdb_shipper_retention_marker_table_processed_total{%s})' % $.namespaceMatcher(),
              ],
              [
                '{{action}}',
                '{{action}}',
              ]
            ) +
            $.withStacking,
          )
          .addPanel(
            $.newQueryPanel('Modified Tables') +
            $.queryPanel(
              [
                'count by (table, action) (loki_tsdb_shipper_retention_marker_table_processed_total{%s , action=~"modified|deleted"})' % $.namespaceMatcher(),
                'count by (table, action) (loki_boltdb_shipper_retention_marker_table_processed_total{%s , action=~"modified|deleted"})' % $.namespaceMatcher(),
              ],
              [
                '{{table}}-{{action}}',
                '{{table}}-{{action}}',
              ]
            ) +
            $.withStacking,
          )
          .addPanel(
            $.newQueryPanel('Marks Creation Rate Per Table') +
            $.queryPanel(
              [
                'sum by (table) (rate(loki_tsdb_shipper_retention_marker_count_total{%s}[$__rate_interval])) >0' % $.namespaceMatcher(),
                'sum by (table) (rate(loki_boltdb_shipper_retention_marker_count_total{%s}[$__rate_interval])) >0' % $.namespaceMatcher(),
              ],
              [
                '{{table}}',
                '{{table}}',
              ]
            ) +
            $.withStacking,
          )
        )
        .addRow(
          $.row('')
          .addPanel(
            $.newQueryPanel('Marked Chunks (24h)') +
            $.statPanel(|||
              sum (
                increase(loki_tsdb_shipper_retention_marker_count_total{%(matcher)s}[24h])
                or
                increase(loki_boltdb_shipper_retention_marker_count_total{%(matcher)s}[24h])
              )
            ||| % { matcher: $.namespaceMatcher() }, 'short')
          )
          .addPanel(
            $.newQueryPanel('Mark Table Latency') +
            dualMetricLatencyPanel(
              'loki_tsdb_shipper_retention_marker_table_processed_duration_seconds',
              'loki_boltdb_shipper_retention_marker_table_processed_duration_seconds',
              '{%s}' % $.namespaceMatcher(),
            )
          )
        )
        .addRow(
          $.row('Sweeper')
          .addPanel(
            $.newQueryPanel('Delete Chunks (24h)') +
            $.statPanel(|||
              sum (
                increase(loki_tsdb_shipper_retention_sweeper_chunk_deleted_duration_seconds_count{%(matcher)s}[24h])
                or
                increase(loki_boltdb_shipper_retention_sweeper_chunk_deleted_duration_seconds_count{%(matcher)s}[24h])
              )
            ||| % { matcher: $.namespaceMatcher() }, 'short')
          )
          .addPanel(
            $.newQueryPanel('Delete Latency') +
            dualMetricLatencyPanel(
              'loki_tsdb_shipper_retention_sweeper_chunk_deleted_duration_seconds',
              'loki_boltdb_shipper_retention_sweeper_chunk_deleted_duration_seconds',
              '{%s}' % $.namespaceMatcher(),
            )
          )
        )
        .addRow(
          $.row('')
          .addPanel(
            $.newQueryPanel('Sweeper Lag', 's') +
            $.queryPanel(
              [
                'time() - (loki_tsdb_shipper_retention_sweeper_marker_file_processing_current_time{%s} > 0)' % $.namespaceMatcher(),
                'time() - (loki_boltdb_shipper_retention_sweeper_marker_file_processing_current_time{%s} > 0)' % $.namespaceMatcher(),
              ],
              [
                'lag',
                'lag',
              ]
            ),
          )
          .addPanel(
            $.newQueryPanel('Marks Files to Process') +
            $.queryPanel(
              [
                'sum(loki_tsdb_shipper_retention_sweeper_marker_files_current{%s})' % $.namespaceMatcher(),
                'sum(loki_boltdb_shipper_retention_sweeper_marker_files_current{%s})' % $.namespaceMatcher(),
              ],
              [
                'count',
                'count',
              ]
            ),
          )
          .addPanel(
            $.newQueryPanel('Delete Rate Per Status') +
            $.queryPanel(
              [
                'sum by (status) (rate(loki_tsdb_shipper_retention_sweeper_chunk_deleted_duration_seconds_count{%s}[$__rate_interval]))' % $.namespaceMatcher(),
                'sum by (status) (rate(loki_boltdb_shipper_retention_sweeper_chunk_deleted_duration_seconds_count{%s}[$__rate_interval]))' % $.namespaceMatcher(),
              ],
              [
                '{{status}}',
                '{{status}}',
              ]
            ),
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
